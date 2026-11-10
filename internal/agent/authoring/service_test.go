package authoring

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/davirain-su/lintel/internal/plugin"
)

// 假 peer 只控制编排时序，不启动真实模型或代替共享静态校验。
type fakePeer struct {
	cfg                       acp.Config
	prompt                    func(context.Context, string, string) (acp.PromptResult, error)
	initialize                func(context.Context) (acp.Initialization, error)
	setConfig                 func(context.Context, string, string, string) (acp.Session, error)
	prompts, sessions, closes atomic.Int32
}

func (p *fakePeer) Initialize(ctx context.Context) (acp.Initialization, error) {
	if p.initialize != nil {
		return p.initialize(ctx)
	}
	return acp.Initialization{ProtocolVersion: acp.ProtocolVersion}, nil
}
func (p *fakePeer) NewSession(context.Context, string) (acp.Session, error) {
	p.sessions.Add(1)
	return acp.Session{ID: "test-session"}, nil
}
func (p *fakePeer) Prompt(ctx context.Context, session, text string) (acp.PromptResult, error) {
	p.prompts.Add(1)
	if p.prompt != nil {
		return p.prompt(ctx, session, text)
	}
	return acp.PromptResult{StopReason: "end_turn"}, nil
}
func (p *fakePeer) Close() error { p.closes.Add(1); return nil }

// 测试包装器故意没有 Decide，编排层无法借此越过原生批准。
type recordingManager struct {
	manager                         *plugin.Manager
	validations, proposals, cancels atomic.Int32
	afterValidate                   func(plugin.ValidationResult)
	beforePropose                   func(plugin.Source)
	afterPropose                    func()
}

func (m *recordingManager) Validate(source plugin.Source) (plugin.ValidationResult, error) {
	m.validations.Add(1)
	v, err := m.manager.Validate(source)
	if m.afterValidate != nil {
		m.afterValidate(v)
	}
	return v, err
}
func (m *recordingManager) Propose(source plugin.Source, op, target, hash, via string) (plugin.Installation, error) {
	m.proposals.Add(1)
	if m.beforePropose != nil {
		m.beforePropose(source)
	}
	v, err := m.manager.Propose(source, op, target, hash, via)
	if m.afterPropose != nil {
		m.afterPropose()
	}
	return v, err
}
func (m *recordingManager) Cancel(id string) (plugin.Installation, error) {
	m.cancels.Add(1)
	return m.manager.Cancel(id)
}

func testPackage(id, version, main string) plugin.Package {
	manifest, _ := json.Marshal(plugin.Manifest{
		ID: id, Name: "测试个人应用", Version: version, Main: "index.js",
		Navigation: []plugin.NavItem{{Title: "概览", Page: "overview"}},
	})
	return plugin.Package{Plugin: manifest, Main: main}
}

const testCode = `export function page() { return { title: "测试" }; }`

func writeApp(dir, id, version, code string) error {
	p := testPackage(id, version, code)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), p.Plugin, 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "index.js"), []byte(p.Main), 0600)
}

func testProfile(t *testing.T) Profile {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Profile{ID: "test", Name: "本地测试 peer", Executable: exe}
}
func testRequest(t *testing.T) Request {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(root, func(name string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(name, 0700)
			}
			return err
		})
	})
	return Request{Profile: testProfile(t), Prompt: "创建只读工具", WorkspaceRoot: root,
		Catalog:  json.RawMessage(`{"catalogVersion":1}`),
		Examples: []plugin.Package{testPackage("com.example.gas", "1.0.0", testCode)},
	}
}
func harness(t *testing.T) (*Service, *fakePeer, *recordingManager, *plugin.Store) {
	t.Helper()
	return harnessSecrets(t, nil)
}
func harnessSecrets(t *testing.T, secrets plugin.Secrets) (*Service, *fakePeer, *recordingManager, *plugin.Store) {
	t.Helper()
	store := plugin.NewStore(filepath.Join(t.TempDir(), "plugins"), secrets)
	manager, err := plugin.NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	s := New(manager)
	wrapped := &recordingManager{manager: manager}
	s.manager = wrapped
	p := &fakePeer{}
	s.start = func(_ context.Context, cfg acp.Config) (peer, error) { p.cfg = cfg; return p, nil }
	return s, p, wrapped, store
}
func requireNoProposal(t *testing.T, m *recordingManager) {
	t.Helper()
	if m.proposals.Load() != 0 || len(m.manager.Installations()) != 0 {
		t.Fatal("失败任务产生了安装提议")
	}
}
func receiveError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("任务未及时停止")
		return nil
	}
}

func TestRunReturnsOnlyFrozenPending(t *testing.T) {
	s, p, m, store := harness(t)
	req := testRequest(t)
	// 若校验错误地执行 JS，这个顶层死循环会暴露问题。
	p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
		err := writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.created", "1.0.0", `while (true) {} `+testCode)
		return acp.PromptResult{StopReason: "end_turn"}, err
	}
	out, err := s.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if out.Installation.Status != "pending" || out.Installation.Via != "acp" || out.Installation.Operation != "install" {
		t.Fatalf("错误任务: %+v", out.Installation)
	}
	if out.Workspace == req.WorkspaceRoot || filepath.Dir(out.Workspace) != req.WorkspaceRoot || out.Session.ID != "test-session" {
		t.Fatalf("错误结果: %+v", out)
	}
	if _, err := os.Stat(out.Workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("完成后的生成目录未删除")
	}
	if out.Initialization.ProtocolVersion != acp.ProtocolVersion || p.closes.Load() != 1 || m.validations.Load() != 1 || m.proposals.Load() != 1 {
		t.Fatal("未完成握手 / 校验 / 清理")
	}
	installed, err := store.List()
	if err != nil || len(installed) != 0 {
		t.Fatal("编排批准或运行了 pending 应用")
	}
	frozen, err := m.manager.GetInstallation(out.Installation.RequestID)
	if err != nil || frozen.PackageHash != out.Installation.PackageHash || frozen.Status != "pending" {
		t.Fatal("外部文件改变影响了冻结提议")
	}
}

func TestCancelRemovesAuthoringWorkspace(t *testing.T) {
	s, p, _, _ := harness(t)
	entered := make(chan string, 1)
	p.prompt = func(ctx context.Context, _, _ string) (acp.PromptResult, error) {
		entered <- p.cfg.Cwd
		<-ctx.Done()
		return acp.PromptResult{}, ctx.Err()
	}
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), testRequest(t)); done <- err }()
	var workspace string
	select {
	case workspace = <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("生成没有开始")
	}
	if _, err := os.Stat(filepath.Join(workspace, "context")); err != nil {
		t.Fatal("运行中的生成目录不存在")
	}
	s.Cancel()
	if err := receiveError(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消: %v", err)
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("取消后生成目录还在")
	}
}

func TestInstalledHandoffDoesNotKeepAuthoringWorkspace(t *testing.T) {
	s, p, m, store := harness(t)
	p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
		return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.installed", "1.0.0", testCode)
	}
	out, err := s.Run(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out.Workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("交接后生成目录还在")
	}
	if _, err := m.manager.Decide(out.Installation.RequestID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("local.personal.installed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out.Workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("安装后又出现了生成目录")
	}
}

func TestBusyCancelAndUnfinishedPrompt(t *testing.T) {
	s, p, m, _ := harness(t)
	entered := make(chan struct{})
	p.prompt = func(ctx context.Context, _, _ string) (acp.PromptResult, error) {
		if err := writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.unfinished", "1.0.0", testCode); err != nil {
			return acp.PromptResult{}, err
		}
		if p.cfg.OnUpdate != nil {
			p.cfg.OnUpdate(acp.Update{SessionID: "test-session", Type: "agent_message_chunk", Text: "已经生成"})
		}
		close(entered)
		<-ctx.Done()
		return acp.PromptResult{StopReason: "end_turn"}, ctx.Err()
	}
	req := testRequest(t)
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), req); done <- err }()
	<-entered
	if m.validations.Load() != 0 {
		t.Fatal("prompt 完成前读取了候选")
	}
	if _, err := s.Run(context.Background(), req); !errors.Is(err, ErrBusy) {
		t.Fatalf("重叠 Run: %v", err)
	}
	if _, err := s.Check(context.Background(), req.Profile); !errors.Is(err, ErrBusy) {
		t.Fatalf("重叠 Check: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("静默或文字被当成完成: %v", err)
	default:
	}
	s.Cancel()
	s.Cancel()
	if err := receiveError(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消: %v", err)
	}
	requireNoProposal(t, m)
	if p.closes.Load() != 1 {
		t.Fatal("取消未关闭 peer")
	}
	p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
		return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.next", "1.0.0", testCode)
	}
	if _, err := s.Run(context.Background(), req); err != nil {
		t.Fatalf("取消未释放占用位: %v", err)
	}
}

func TestNonEndTurnAndPromptErrorsNeverPropose(t *testing.T) {
	for _, reason := range []string{"cancelled", "max_tokens", "max_turn_requests", "refusal", "", "tool_done"} {
		t.Run(reason, func(t *testing.T) {
			s, p, m, _ := harness(t)
			p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
				return acp.PromptResult{StopReason: reason}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.stopped", "1.0.0", testCode)
			}
			_, err := s.Run(context.Background(), testRequest(t))
			if !errors.Is(err, ErrIncomplete) {
				t.Fatalf("停止原因 %q 被接受: %v", reason, err)
			}
			if m.validations.Load() != 0 {
				t.Fatal("非 end_turn 仍被校验")
			}
			requireNoProposal(t, m)
		})
	}
	for _, promptErr := range []error{io.EOF, errors.New("fake failure"), context.Canceled} {
		t.Run(promptErr.Error(), func(t *testing.T) {
			s, p, m, _ := harness(t)
			p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
				return acp.PromptResult{StopReason: "end_turn"}, promptErr
			}
			if _, err := s.Run(context.Background(), testRequest(t)); err == nil {
				t.Fatal("错误被当成完成")
			}
			requireNoProposal(t, m)
		})
	}
}

func TestMissingArtifactAndRepairLimit(t *testing.T) {
	s, p, m, _ := harness(t)
	out, err := s.Run(context.Background(), testRequest(t))
	var invalid *ValidationFailure
	if !errors.Is(err, ErrValidation) || !errors.As(err, &invalid) || len(invalid.Errors) == 0 {
		t.Fatalf("缺少结构化失败: %v", err)
	}
	if p.prompts.Load() != 4 || m.validations.Load() != 4 {
		t.Fatalf("修复未封顶三轮: prompt=%d validate=%d", p.prompts.Load(), m.validations.Load())
	}
	if _, err := os.Stat(out.Workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("失败后的生成目录未删除")
	}
	requireNoProposal(t, m)
}

func TestRepairKeepsSessionAndStructuredErrors(t *testing.T) {
	s, p, _, _ := harness(t)
	var firstDeadline time.Time
	p.prompt = func(ctx context.Context, session, text string) (acp.PromptResult, error) {
		if session != "test-session" {
			t.Fatal("修复换了会话")
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("prompt 没有总期限")
		}
		code := `export function page() {`
		if p.prompts.Load() == 1 {
			firstDeadline = deadline
		} else {
			if !deadline.Equal(firstDeadline) {
				t.Fatal("修复重置了总期限")
			}
			if !strings.Contains(text, "INVALID_SYNTAX") {
				t.Fatal("修复未提供结构化错误")
			}
			code = testCode
		}
		return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.repaired", "1.0.0", code)
	}
	if _, err := s.Run(context.Background(), testRequest(t)); err != nil {
		t.Fatal(err)
	}
	if p.prompts.Load() != 2 || p.sessions.Load() != 1 {
		t.Fatal("修复次数或会话数错误")
	}
}

func TestTotalDeadlineAndCancellationAfterValidation(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		s, p, m, _ := harness(t)
		s.runTimeout = 30 * time.Millisecond
		p.prompt = func(ctx context.Context, _, _ string) (acp.PromptResult, error) {
			<-ctx.Done()
			return acp.PromptResult{}, ctx.Err()
		}
		_, err := s.Run(context.Background(), testRequest(t))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("总期限未约束 prompt: %v", err)
		}
		requireNoProposal(t, m)
	})
	t.Run("parent deadline", func(t *testing.T) {
		s, p, m, _ := harness(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		wantDeadline, _ := ctx.Deadline()
		p.prompt = func(ctx context.Context, _, _ string) (acp.PromptResult, error) {
			got, ok := ctx.Deadline()
			if !ok || !got.Equal(wantDeadline) {
				t.Fatal("覆盖了调用方更短期限")
			}
			<-ctx.Done()
			return acp.PromptResult{}, ctx.Err()
		}
		if _, err := s.Run(ctx, testRequest(t)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("调用方期限: %v", err)
		}
		requireNoProposal(t, m)
	})
	t.Run("validation cancelled", func(t *testing.T) {
		s, p, m, _ := harness(t)
		p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
			return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.cancelled", "1.0.0", testCode)
		}
		m.afterValidate = func(v plugin.ValidationResult) {
			if v.Valid {
				s.Cancel()
			}
		}
		if _, err := s.Run(context.Background(), testRequest(t)); !errors.Is(err, context.Canceled) {
			t.Fatalf("校验后取消: %v", err)
		}
		requireNoProposal(t, m)
	})
}

func TestProposalMutationAndCancellationAreRevoked(t *testing.T) {
	for _, scenario := range []string{"changed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			s, p, m, _ := harness(t)
			p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
				return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.race", "1.0.0", testCode)
			}
			if scenario == "changed" {
				m.beforePropose = func(source plugin.Source) {
					if err := os.WriteFile(filepath.Join(source.Path, "index.js"), []byte(testCode+" // mutated"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				m.afterPropose = s.Cancel
			}
			_, err := s.Run(context.Background(), testRequest(t))
			want := ErrArtifactChanged
			if scenario == "cancelled" {
				want = context.Canceled
			}
			if !errors.Is(err, want) {
				t.Fatalf("交接未拒绝 %s: %v", scenario, err)
			}
			if len(m.manager.Installations()) != 0 || m.cancels.Load() != 1 {
				t.Fatal("失效提议仍可批准")
			}
		})
	}
}

func TestContextEnvironmentAndPermissionBoundary(t *testing.T) {
	s, p, _, _ := harness(t)
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	secretText := hex.EncodeToString(secret)
	t.Setenv("LINTEL_API_TOKEN", secretText)
	t.Setenv("ARBITRARY_SECRET", secretText)
	originalHome := os.Getenv("HOME")
	req := testRequest(t)
	var updates atomic.Int32
	req.OnUpdate = func(acp.Update) { updates.Add(1) }
	p.prompt = func(ctx context.Context, _, text string) (acp.PromptResult, error) {
		for _, expected := range []string{"workspace/app", "context", "安装", "签名", "只读", "local.personal."} {
			if !strings.Contains(text, expected) {
				t.Fatalf("提示词缺少约定 %s", expected)
			}
		}
		allowed := map[string]bool{"HOME": true, "PATH": true, "USER": true, "TMPDIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true}
		values := map[string]string{}
		for _, item := range p.cfg.Env {
			k, v, ok := strings.Cut(item, "=")
			if !ok || !allowed[k] || strings.Contains(item, secretText) {
				t.Fatal("继承了未授权环境")
			}
			values[k] = v
		}
		if values["HOME"] != originalHome {
			t.Fatal("agent 未使用原 HOME")
		}
		if _, ok := values["PATH"]; !ok {
			t.Fatal("未传 PATH")
		}
		if strings.Contains(text, secretText) {
			t.Fatal("秘密进入提示词")
		}
		ctxDir := filepath.Join(p.cfg.Cwd, "context")
		for _, path := range []string{ctxDir, filepath.Join(ctxDir, "catalog.json"), filepath.Join(ctxDir, "example-1.cryptoapp.json")} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm()&0222 != 0 {
				t.Fatalf("上下文不是只读: %s", path)
			}
		}
		catalog, err := os.ReadFile(filepath.Join(ctxDir, "catalog.json"))
		if err != nil || !reflect.DeepEqual(json.RawMessage(catalog), req.Catalog) {
			t.Fatal("catalog 内容改变")
		}
		id, err := p.cfg.RequestPermission(ctx, acp.PermissionRequest{Options: []acp.PermissionOption{{OptionID: "opaque:allow", Kind: "allow_once"}}})
		if err != nil || id != "" {
			t.Fatal("默认工具许可未拒绝")
		}
		p.cfg.OnUpdate(acp.Update{SessionID: "test-session", Text: "progress"})
		return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.context", "1.0.0", testCode)
	}
	if _, err := s.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if updates.Load() != 1 {
		t.Fatal("更新未转发")
	}
}

func TestPermissionKeepsOriginalOptionID(t *testing.T) {
	for _, selection := range []string{"opaque / 允许 once", "unknown", "callback-error"} {
		t.Run(selection, func(t *testing.T) {
			s, p, _, _ := harness(t)
			req := testRequest(t)
			req.RequestPermission = func(ctx context.Context, r acp.PermissionRequest) (string, error) {
				if len(r.Options) != 2 || r.Options[0].OptionID != "opaque / 允许 once" {
					t.Fatal("optionId 被改写")
				}
				if selection == "callback-error" {
					return "opaque / 允许 once", errors.New("closed")
				}
				return selection, nil
			}
			p.prompt = func(ctx context.Context, _, _ string) (acp.PromptResult, error) {
				id, _ := p.cfg.RequestPermission(ctx, acp.PermissionRequest{Options: []acp.PermissionOption{{OptionID: "opaque / 允许 once", Kind: "allow_once"}, {OptionID: "opaque-deny", Kind: "reject_once"}}})
				want := selection
				if selection != "opaque / 允许 once" {
					want = ""
				}
				if id != want {
					t.Fatalf("许可结果=%q 期望=%q", id, want)
				}
				return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.permission", "1.0.0", testCode)
			}
			if _, err := s.Run(context.Background(), req); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCancelClosesPermissionContextAndRejectsLateAllow(t *testing.T) {
	s, p, m, _ := harness(t)
	req := testRequest(t)
	entered := make(chan struct{})
	req.RequestPermission = func(ctx context.Context, _ acp.PermissionRequest) (string, error) {
		close(entered)
		<-ctx.Done()
		return "opaque-allow", nil
	}
	p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
		id, err := p.cfg.RequestPermission(context.Background(), acp.PermissionRequest{Options: []acp.PermissionOption{{OptionID: "opaque-allow", Kind: "allow_once"}}})
		if id != "" || err != nil {
			return acp.PromptResult{}, errors.New("late permission was allowed")
		}
		return acp.PromptResult{StopReason: "end_turn"}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := s.Run(context.Background(), req); done <- err }()
	<-entered
	s.Cancel()
	if err := receiveError(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("许可窗口取消未绑定生成任务: %v", err)
	}
	requireNoProposal(t, m)
}

func TestExecutableValidationNeverInstallsTools(t *testing.T) {
	for _, executable := range []string{"npx", "/usr/local/bin/uvx", "curl", "npm", "sh", "env", "/missing/lintel-acp-agent"} {
		t.Run(executable, func(t *testing.T) {
			s, _, m, _ := harness(t)
			var starts atomic.Int32
			s.start = func(context.Context, acp.Config) (peer, error) { starts.Add(1); return nil, errors.New("must not run") }
			req := testRequest(t)
			req.Profile.Executable = executable
			if _, err := s.Run(context.Background(), req); err == nil {
				t.Fatal("接受了安装工具 / 缺失程序")
			}
			if _, err := s.Check(context.Background(), req.Profile); err == nil {
				t.Fatal("连接测试接受了安装工具 / 缺失程序")
			}
			if starts.Load() != 0 {
				t.Fatal("尝试运行自动安装命令")
			}
			requireNoProposal(t, m)
		})
	}
}

func TestCheckOnlyHandshakesAndSharesBusyGate(t *testing.T) {
	s, p, m, _ := harness(t)
	init, err := s.Check(context.Background(), testProfile(t))
	if err != nil || init.ProtocolVersion != acp.ProtocolVersion {
		t.Fatalf("Check: %+v %v", init, err)
	}
	if p.sessions.Load() != 0 || p.prompts.Load() != 0 || p.closes.Load() != 1 {
		t.Fatal("Check 发起了付费生成或未关闭")
	}
	if _, err := os.Stat(p.cfg.Cwd); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Check 目录未清理")
	}
	requireNoProposal(t, m)
	entered := make(chan struct{})
	done := make(chan error, 1)
	p.initialize = func(ctx context.Context) (acp.Initialization, error) {
		close(entered)
		<-ctx.Done()
		return acp.Initialization{}, ctx.Err()
	}
	go func() { _, err := s.Check(context.Background(), testProfile(t)); done <- err }()
	<-entered
	if _, err := s.Run(context.Background(), testRequest(t)); !errors.Is(err, ErrBusy) {
		t.Fatalf("Check 未占用: %v", err)
	}
	s.Cancel()
	if err := receiveError(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("Check 取消: %v", err)
	}
}

func TestRequestIdentityAndContextValidation(t *testing.T) {
	for _, kind := range []string{"empty prompt", "bad catalog", "empty examples", "create hash", "foreign create id", "update no hash", "relative workspace"} {
		t.Run(kind, func(t *testing.T) {
			s, _, m, _ := harness(t)
			req := testRequest(t)
			switch kind {
			case "empty prompt":
				req.Prompt = " "
			case "bad catalog":
				req.Catalog = []byte(`null`)
			case "empty examples":
				req.Examples = nil
			case "create hash":
				req.ExpectedHash = "sha256:" + strings.Repeat("0", 64)
			case "foreign create id":
				req.TargetID = "com.example.gas"
			case "update no hash":
				req.ExistingDir = t.TempDir()
				req.TargetID = "local.personal.old"
			case "relative workspace":
				req.WorkspaceRoot = "relative"
			}
			if _, err := s.Run(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("无效请求: %v", err)
			}
			requireNoProposal(t, m)
		})
	}
}

func TestGeneratedIdentityCannotChangeTarget(t *testing.T) {
	for _, target := range []string{"", "local.personal.fixed"} {
		t.Run(target, func(t *testing.T) {
			s, p, m, _ := harness(t)
			req := testRequest(t)
			req.TargetID = target
			p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
				return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.other.replacement", "1.0.0", testCode)
			}
			if _, err := s.Run(context.Background(), req); !errors.Is(err, ErrValidation) {
				t.Fatalf("接受了换目标: %v", err)
			}
			requireNoProposal(t, m)
		})
	}
}
