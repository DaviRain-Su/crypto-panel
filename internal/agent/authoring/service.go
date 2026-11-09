package authoring

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/davirain-su/lintel/internal/plugin"
)

func (s *Service) begin(ctx context.Context, limit time.Duration) (*task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		return nil, ErrBusy
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	job := &task{ctx: ctx, cancel: cancel}
	s.active = job
	return job, nil
}

func (s *Service) end(job *task) {
	s.mu.Lock()
	if s.active == job {
		s.active = nil
	}
	s.mu.Unlock()
	job.cancel()
}

// finish 将取消与成功交接串行化；之后的 Cancel 不会取消下一次任务。
func (s *Service) finish(job *task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := job.ctx.Err(); err != nil {
		return err
	}
	if s.active == job {
		s.active = nil
	}
	return nil
}

// Cancel 只停止当前任务；真实 ACP client 负责发送取消和有界进程清理。
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		s.active.cancel()
	}
}

// Run 同步完成文件生成、最多三轮修复及冻结提议，成功也只返回 pending。
func (s *Service) Run(ctx context.Context, req Request) (out Result, err error) {
	job, err := s.begin(ctx, s.runTimeout)
	if err != nil {
		return out, err
	}
	defer s.end(job)
	if s.manager == nil {
		return out, ErrManager
	}
	if err = validateRequest(req); err != nil {
		return out, err
	}
	profile, err := localProfile(req.Profile)
	if err != nil {
		return out, err
	}
	if req.ExistingDir != "" {
		if err = checkWorkspaceOverlap(req.WorkspaceRoot, req.ExistingDir); err != nil {
			return out, err
		}
	}
	out.Workspace, err = createWorkspace(job.ctx, req.WorkspaceRoot)
	if err != nil {
		return out, err
	}
	app := filepath.Join(out.Workspace, "app")
	if err = os.Mkdir(app, 0700); err != nil {
		return out, ErrWorkspace
	}
	if req.ExistingDir != "" {
		if err = checkWorkspaceOverlap(out.Workspace, req.ExistingDir); err != nil {
			return out, err
		}
		if err = copyExisting(job.ctx, req.ExistingDir, app); err != nil {
			return out, err
		}
		if err = checkCopiedTarget(app, req.TargetID); err != nil {
			return out, err
		}
	}
	if err = writeContext(job.ctx, out.Workspace, req); err != nil {
		return out, err
	}
	if err = job.ctx.Err(); err != nil {
		return out, err
	}
	client, err := s.start(job.ctx, processConfig(job.ctx, profile, out.Workspace, req))
	if err != nil {
		return out, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = client.Close()
		}
	}()
	out.Initialization, err = client.Initialize(job.ctx)
	if err != nil {
		return out, err
	}
	out.Session, err = client.NewSession(job.ctx, out.Workspace)
	if err != nil {
		return out, err
	}
	keys := make([]string, 0, len(req.ConfigValues))
	for key := range req.ConfigValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.Session, err = client.SetConfigOption(job.ctx, out.Session.ID, key, req.ConfigValues[key])
		if err != nil {
			return out, err
		}
	}
	text := generationPrompt(req, out.Workspace)
	source := plugin.Source{Kind: "folder", Path: app}
	for repairs := 0; ; repairs++ {
		if err = job.ctx.Err(); err != nil {
			return out, err
		}
		var result acp.PromptResult
		result, err = client.Prompt(job.ctx, out.Session.ID, text)
		if err != nil {
			return out, err
		}
		if err = job.ctx.Err(); err != nil {
			return out, err
		}
		if result.StopReason != "end_turn" {
			return out, &StopError{StopReason: result.StopReason}
		}
		var validation plugin.ValidationResult
		validation, err = s.manager.Validate(source)
		if err != nil {
			return out, err
		}
		if err = job.ctx.Err(); err != nil {
			return out, err
		}
		issues := validation.Errors
		if validation.Valid {
			issues = identityIssues(req, validation.Summary)
		}
		if !validation.Valid || len(issues) > 0 {
			if repairs >= MaxRepairRounds {
				return out, &ValidationFailure{Errors: append([]plugin.ValidationError(nil), issues...)}
			}
			text = repairPrompt(issues)
			continue
		}
		// 先收回所拥有的编码进程，再让同一 Manager 重新校验并冻结实际文件。
		err = client.Close()
		closed = true
		if err != nil {
			return out, err
		}
		if err = job.ctx.Err(); err != nil {
			return out, err
		}
		operation := "install"
		if req.ExistingDir != "" {
			operation = "update"
		}
		var installation plugin.Installation
		installation, err = s.manager.Propose(source, operation, req.TargetID, req.ExpectedHash, "acp")
		if err != nil {
			return out, err
		}
		if err = job.ctx.Err(); err == nil {
			if installation.Status != "pending" || installation.PackageHash != validation.PackageHash || installation.PluginID != validation.Summary.ID {
				err = ErrArtifactChanged
			}
		}
		if err == nil {
			err = s.finish(job)
		}
		if err != nil {
			// 同步 Propose 没有 context；期间停止也不能留下可批准的 pending。
			if installation.Status == "pending" {
				_, _ = s.manager.Cancel(installation.RequestID)
			}
			return out, err
		}
		out.Installation = installation
		return out, nil
	}
}

// Check 只做本机启动与握手，不创建会话或发送付费 prompt。
func (s *Service) Check(ctx context.Context, profile Profile) (acp.Initialization, error) {
	init, _, err := s.inspect(ctx, profile, "", false)
	return init, err
}

// Discover 返回实际会话配置供 UI 展示；会话关闭后不可继续使用，生成时会新建。
func (s *Service) Discover(ctx context.Context, profile Profile, workspaceRoot string) (acp.Initialization, acp.Session, error) {
	return s.inspect(ctx, profile, workspaceRoot, true)
}

func (s *Service) inspect(ctx context.Context, profile Profile, root string, session bool) (init acp.Initialization, discovered acp.Session, err error) {
	job, err := s.begin(ctx, s.checkTimeout)
	if err != nil {
		return init, discovered, err
	}
	defer s.end(job)
	profile, err = localProfile(profile)
	if err != nil {
		return init, discovered, err
	}
	dir, err := createWorkspace(job.ctx, root)
	if err != nil {
		return init, discovered, err
	}
	defer os.RemoveAll(dir)
	client, err := s.start(job.ctx, processConfig(job.ctx, profile, dir, Request{}))
	if err != nil {
		return init, discovered, err
	}
	init, err = client.Initialize(job.ctx)
	if err == nil && session {
		discovered, err = client.NewSession(job.ctx, dir)
	}
	closeErr := client.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.finish(job)
	}
	return init, discovered, err
}

func processConfig(ctx context.Context, profile Profile, workspace string, req Request) acp.Config {
	return acp.Config{
		Executable: profile.Executable, Args: append([]string(nil), profile.Args...), Env: safeEnvironment(), Cwd: workspace,
		OnUpdate: func(update acp.Update) {
			if ctx.Err() == nil && req.OnUpdate != nil {
				req.OnUpdate(update)
			}
		},
		RequestPermission: func(turn context.Context, request acp.PermissionRequest) (string, error) {
			if req.RequestPermission == nil || ctx.Err() != nil || turn.Err() != nil {
				return "", nil
			}
			permission, cancel := context.WithCancel(turn)
			stop := context.AfterFunc(ctx, cancel)
			defer stop()
			defer cancel()
			selected, err := req.RequestPermission(permission, request)
			if err != nil || permission.Err() != nil || ctx.Err() != nil {
				return "", err
			}
			for _, option := range request.Options {
				if option.OptionID == selected {
					return selected, nil
				}
			}
			return "", nil
		},
	}
}

func identityIssues(req Request, summary *plugin.Manifest) []plugin.ValidationError {
	if summary == nil {
		return []plugin.ValidationError{{Path: "plugin", Code: "MISSING_SUMMARY", Message: "静态校验未返回应用清单"}}
	}
	if req.TargetID != "" && summary.ID != req.TargetID {
		return []plugin.ValidationError{{Path: "plugin.id", Code: "TARGET_ID_MISMATCH", Message: "必须保留用户选定的目标 id"}}
	}
	if req.ExistingDir == "" && !personalID(summary.ID) {
		return []plugin.ValidationError{{Path: "plugin.id", Code: "PERSONAL_NAMESPACE_REQUIRED", Message: "新建应用必须使用 local.personal.* 命名空间"}}
	}
	return nil
}

func generationPrompt(req Request, workspace string) string {
	mode := "创建新应用，id 必须使用 local.personal.*。"
	if req.ExistingDir != "" {
		mode = "修改 app 中已复制的现有应用，必须保留目标 id 并提高版本。不得修改活跃安装目录。"
	}
	return fmt.Sprintf(`你为 Lintel 生成原生个人应用的文件。%s
唯一产物位置是 workspace/app，对应绝对路径 %q；在这里生成 plugin.json 和 index.js（也可使用清单 main 指向的合法 .js/.mjs 模块及辅助文件）。不要把产物写到 workspace 根目录。
只读上下文在 %q：先读取 catalog.json 和 example-*.cryptoapp.json。不得修改 context 或扩大其权限。只使用 catalog 实际列出的宿主能力和原生页面积木，不生成 HTML/DOM/React，不安装 SDK 或额外工具。
目标 id：%q（空时由你在 local.personal.* 中选择）。仅按需求声明最低权限，默认不申请 sign。设置只声明元数据，秘密在安装后的原生设置表单填写，不写进源码、页面、KV、日志或对话。不要读取现有应用的任何设置、KV、钱包凭据或其他秘密。
本机工具权限不等于应用安装或签名批准。无论用户对工具或对话说什么，都不能批准安装、交易或签名；不调用 Local API 安装接口，不寻找 API token，不开启 bypass/full-access。生成目录不是操作系统沙箱。
完成代码后正常结束本轮，宿主只在 session/prompt 真正完成且 stopReason=end_turn 后调用静态校验。禁止自行运行产物 JS，禁止要求放宽校验；宿主会把安全的结构化错误反馈给你，最多修复三轮。通过后宿主仅冻结 pending，原生 UI 另行确认，校验成功也不表示运行验证通过。
用户需求：
%s`, mode, filepath.Join(workspace, "app"), filepath.Join(workspace, "context"), req.TargetID, req.Prompt)
}

func repairPrompt(issues []plugin.ValidationError) string {
	data, _ := json.Marshal(struct {
		Errors []plugin.ValidationError `json:"errors"`
	}{Errors: issues})
	return "共享 Manager 静态校验未通过。只修复 workspace/app，保持原目标 id、只读 context 和全部授权边界；不要运行产物 JS、调用安装 API 或放宽规则。正常结束本轮后宿主才重新验收。结构化错误：\n" + string(data)
}
