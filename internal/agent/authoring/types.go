// Package authoring 编排本机 ACP 文件生成，只提议待确认应用，不拥有安装或签名批准权。
package authoring

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/davirain-su/lintel/internal/plugin"
)

const (
	RunTimeout          = 15 * time.Minute
	MaxRepairRounds     = 3
	maxPromptBytes      = 64 << 10
	maxCatalogBytes     = 4 << 20
	maxExamples         = 8
	maxManifestBytes    = 64 << 10
	maxMainBytes        = 1 << 20
	maxContextBytes     = 12 << 20
	maxConfigValues     = 32
	maxConfigValueBytes = 4096
	maxCopyFiles        = 200
	maxCopyFileBytes    = 2 << 20
	maxCopyTotalBytes   = 8 << 20
	maxCopyEntries      = 1024
)

var (
	ErrBusy             = errors.New("authoring: another task is active")
	ErrInvalidRequest   = errors.New("authoring: invalid request")
	ErrExecutable       = errors.New("authoring: local executable is unavailable")
	ErrUnsafeExecutable = errors.New("authoring: installers, downloaders and shell launchers are not allowed")
	ErrWorkspace        = errors.New("authoring: workspace is unavailable or unsafe")
	ErrSource           = errors.New("authoring: existing code tree is unreadable or unsafe")
	ErrSourceLimit      = errors.New("authoring: existing code tree exceeds limits")
	ErrIncomplete       = errors.New("authoring: prompt did not complete with end_turn")
	ErrValidation       = errors.New("authoring: generated app failed static validation")
	ErrArtifactChanged  = errors.New("authoring: validated artifact changed before handoff")
	ErrManager          = errors.New("authoring: application manager is unavailable")
)

// Profile 只描述用户已安装的本机程序；参数不经过 shell，也不附加自动授权参数。
type Profile struct {
	ID, Name, Executable string
	Args                 []string
}

// Request 的目录与上下文由原生调用方显式提供，不含 Local API token 或秘密设置值。
// 回调应及时返回；工具许可窗口必须响应上下文取消。
type Request struct {
	Profile                                                    Profile
	Prompt, WorkspaceRoot, TargetID, ExpectedHash, ExistingDir string
	Catalog                                                    json.RawMessage
	Examples                                                   []plugin.Package
	ConfigValues                                               map[string]string
	OnUpdate                                                   func(acp.Update)
	RequestPermission                                          func(context.Context, acp.PermissionRequest) (string, error)
}

// Result 即使失败也保留已创建的工作目录，供调用方展示和后续清理。
type Result struct {
	Installation   plugin.Installation
	Workspace      string
	Session        acp.Session
	Initialization acp.Initialization
}

// ValidationFailure 保留共享校验器给出的安全字段错误，不包含源码或设置值。
type ValidationFailure struct{ Errors []plugin.ValidationError }

func (*ValidationFailure) Error() string { return ErrValidation.Error() }
func (*ValidationFailure) Unwrap() error { return ErrValidation }

// StopError 的原因只用于显示状态，不构成安装批准。
type StopError struct{ StopReason string }

func (*StopError) Error() string { return ErrIncomplete.Error() }
func (*StopError) Unwrap() error { return ErrIncomplete }

// 生命周期窄接口刻意不包含 Decide，避免编排层拥有批准能力。
type lifecycle interface {
	Validate(plugin.Source) (plugin.ValidationResult, error)
	Propose(plugin.Source, string, string, string, string) (plugin.Installation, error)
	Cancel(string) (plugin.Installation, error)
}

type peer interface {
	Initialize(context.Context) (acp.Initialization, error)
	NewSession(context.Context, string) (acp.Session, error)
	SetConfigOption(context.Context, string, string, string) (acp.Session, error)
	Prompt(context.Context, string, string) (acp.PromptResult, error)
	Close() error
}
type peerFactory func(context.Context, acp.Config) (peer, error)

type task struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// Service 同时最多处理一个 Run、Check 或 Discover；原生宿主应共享一个实例。
type Service struct {
	manager                  lifecycle
	start                    peerFactory
	runTimeout, checkTimeout time.Duration
	mu                       sync.Mutex
	active                   *task
}

// New 复用宿主现有 Manager；生产路径默认启动真正的 ACP stdio client。
func New(manager *plugin.Manager) *Service {
	s := &Service{runTimeout: RunTimeout, checkTimeout: acp.HandshakeTimeout}
	if manager != nil {
		s.manager = manager
	}
	s.start = func(ctx context.Context, cfg acp.Config) (peer, error) { return acp.Start(ctx, cfg) }
	return s
}
