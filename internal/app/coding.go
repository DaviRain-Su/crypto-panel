package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/davirain-su/lintel/internal/agent"
	"github.com/davirain-su/lintel/internal/agent/authoring"
	"github.com/davirain-su/lintel/internal/plugin"
)

type codingState struct {
	initialized                                       bool
	service                                           *authoring.Service
	profileID, executable, args                       string
	open, busy, checking                              bool
	prompt, slug, targetID, expectedHash, existingDir string
	notice, err, progress                             string
	installationID                                    string
	initialization                                    acp.Initialization
	session                                           acp.Session
	values                                            map[string]string
	cancel                                            context.CancelFunc
	generation                                        int
}

func codingProfiles() []authoring.Profile {
	return []authoring.Profile{
		{ID: "claude", Name: "Claude Code (ACP)", Executable: "claude-code-acp"},
		{ID: "codex", Name: "Codex (ACP)", Executable: "codex-acp"},
		{ID: "custom", Name: "自定义 ACP Agent"},
	}
}

func (a *App) initAuthoring() {
	if a.coding.initialized {
		return
	}
	a.coding.initialized = true
	if a.pluginManager != nil {
		a.coding.service = authoring.New(a.pluginManager)
	}
	id := a.settings.ActiveCodingAgent
	if id == "" {
		id = "claude"
	}
	a.selectCodingProfile(id)
}

func (a *App) selectCodingProfile(id string) {
	var profile authoring.Profile
	for _, preset := range codingProfiles() {
		if preset.ID == id {
			profile = preset
			break
		}
	}
	for _, saved := range a.settings.CodingAgents {
		if saved.ID == id {
			profile = saved
			break
		}
	}
	if profile.ID == "" {
		profile = codingProfiles()[0]
	}
	a.coding.profileID = profile.ID
	a.coding.executable = profile.Executable
	data, _ := json.Marshal(profile.Args)
	if profile.Args == nil {
		data = []byte("[]")
	}
	a.coding.args = string(data)
	a.coding.session = acp.Session{}
	a.coding.initialization = acp.Initialization{}
	a.coding.values = map[string]string{}
	for key, value := range a.settings.CodingConfig[profile.ID] {
		a.coding.values[key] = value
	}
}

func (a *App) codingProfile() (authoring.Profile, error) {
	p := authoring.Profile{ID: a.coding.profileID, Executable: strings.TrimSpace(a.coding.executable)}
	for _, preset := range codingProfiles() {
		if preset.ID == p.ID {
			p.Name = preset.Name
		}
	}
	if p.Executable == "" || len(p.Executable) > 4096 || strings.ContainsAny(p.Executable, "\x00\r\n") {
		return p, errors.New("请填写已安装的 ACP 可执行文件")
	}
	if err := json.Unmarshal([]byte(a.coding.args), &p.Args); err != nil || p.Args == nil || len(p.Args) > 32 {
		return p, errors.New("参数必须是 JSON 字符串数组，例如 [\"--flag\"]")
	}
	for _, arg := range p.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return p, errors.New("参数过长或包含无效字符")
		}
	}
	return p, nil
}

func (a *App) saveCodingProfile() bool {
	p, err := a.codingProfile()
	if err != nil {
		a.coding.err = err.Error()
		a.invalidate()
		return false
	}
	found := false
	for i := range a.settings.CodingAgents {
		if a.settings.CodingAgents[i].ID == p.ID {
			a.settings.CodingAgents[i] = p
			found = true
			break
		}
	}
	if !found {
		a.settings.CodingAgents = append(a.settings.CodingAgents, p)
	}
	a.settings.ActiveCodingAgent = p.ID
	if a.settings.CodingConfig == nil {
		a.settings.CodingConfig = map[string]map[string]string{}
	}
	values := map[string]string{}
	for key, value := range a.coding.values {
		values[key] = value
	}
	a.settings.CodingConfig[p.ID] = values
	a.coding.err = ""
	a.save()
	a.invalidate()
	return true
}

func (a *App) codingSettings(c *ui.Context) {
	a.initAuthoring()
	p := a.pal
	ui.Column(c).Gap(10).Padding(14, 16).Children(func() {
		ui.Text(c, "用本机编码 Agent 创建、修改个人应用").FontSize(12.5).FontWeight(600)
		ui.Text(c, "先安装 ACP 适配器并完成它自己的登录；测试连接只做协议握手，不发送生成请求。").FontSize(11).TextColor(p.muted).MaxLines(3)
		names := []string{}
		selected := ""
		for _, profile := range codingProfiles() {
			names = append(names, profile.Name)
			if profile.ID == a.coding.profileID {
				selected = profile.Name
			}
		}
		if ui.Select(c.Key("coding-profile"), &selected, names).Changed() {
			for _, profile := range codingProfiles() {
				if profile.Name == selected {
					a.selectCodingProfile(profile.ID)
					break
				}
			}
		}
		ui.TextInput(c.Key("coding-executable"), &a.coding.executable).Label("ACP 可执行文件")
		ui.TextInput(c.Key("coding-args"), &a.coding.args).Label("参数（JSON 数组）")
		ui.Row(c).Gap(8).Children(func() {
			a.primaryButton(c, "保存 Agent", nil, a.coding.busy || a.coding.checking, func() { a.saveCodingProfile() })
			a.primaryButton(c, "测试连接", nil, a.coding.busy || a.coding.checking, a.checkCodingAgent)
		})
		if info := a.coding.initialization.AgentInfo; info != nil {
			ui.Text(c, "已连接 "+safeAgentText(info.Name, 120)+" "+safeAgentText(info.Version, 80)).FontSize(11).TextColor(p.success)
		}
		for _, option := range a.coding.session.ConfigOptions {
			if option.Type != "select" {
				continue
			}
			choices := append([]acp.ConfigValue{}, option.Options...)
			for _, group := range option.Groups {
				choices = append(choices, group.Options...)
			}
			labels := []string{}
			value, ok := a.coding.values[option.ID]
			if !ok {
				value = option.CurrentValue
			}
			selection := ""
			for _, choice := range choices {
				label := safeAgentText(choice.Name, 100) + " (" + safeAgentText(choice.Value, 100) + ")"
				labels = append(labels, label)
				if value == choice.Value {
					selection = label
				}
			}
			ui.Text(c, safeAgentText(option.Name, 100)).FontSize(11).TextColor(p.muted)
			if len(labels) > 0 && ui.Select(c.Key("coding-config-"+option.ID), &selection, labels).Changed() {
				for i, label := range labels {
					if label == selection {
						a.coding.values[option.ID] = choices[i].Value
						a.saveCodingProfile()
						break
					}
				}
			}
		}
		if a.coding.checking {
			ui.Text(c, "正在检查 ACP 握手与会话能力…").FontSize(11).TextColor(p.muted)
		}
		if a.coding.err != "" {
			ui.Text(c, a.coding.err).FontSize(11).TextColor(p.danger).MaxLines(4)
		}
	})
}

func (a *App) workspaceRoot() string {
	return filepath.Join(filepath.Dir(a.settingsPath), "agent-workspaces")
}

func (a *App) checkCodingAgent() {
	a.initAuthoring()
	if a.coding.busy || a.coding.checking || a.coding.service == nil || !a.saveCodingProfile() {
		return
	}
	profile, _ := a.codingProfile()
	a.coding.checking = true
	a.coding.err = ""
	service := a.coding.service
	a.goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), acp.HandshakeTimeout)
		defer cancel()
		root := a.workspaceRoot()
		var initialization acp.Initialization
		var session acp.Session
		err := os.MkdirAll(root, 0700)
		if err == nil {
			initialization, session, err = service.Discover(ctx, profile, root)
		}
		a.runOnMain(func() {
			a.coding.checking = false
			if a.coding.profileID != profile.ID {
				a.invalidate()
				return
			}
			if err != nil {
				a.coding.err = "连接失败：" + safeAgentText(err.Error(), 600)
			} else {
				a.coding.initialization = initialization
				a.coding.session = session
			}
			a.invalidate()
		})
	})
}

func (a *App) openCreator(id string) {
	a.initAuthoring()
	if a.coding.busy {
		a.coding.open = true
		a.route = routePlugins
		a.invalidate()
		return
	}
	a.coding.open = false
	a.coding.targetID, a.coding.expectedHash, a.coding.existingDir = "", "", ""
	a.coding.err, a.coding.notice, a.coding.progress = "", "", ""
	a.coding.installationID = ""
	if id != "" {
		inst, err := a.plugins.Get(id)
		if err != nil || inst.Official || inst.Signed || !strings.HasPrefix(id, "local.") {
			a.pluginNotice = "只能修改 local.* 命名空间的 Unsigned 个人应用"
			a.invalidate()
			return
		}
		hash, err := a.plugins.PackageHash(id)
		if err != nil {
			a.pluginNotice = "无法校验当前应用"
			a.invalidate()
			return
		}
		a.coding.targetID, a.coding.expectedHash, a.coding.existingDir = id, hash, inst.Dir
	}
	a.coding.open = true
	a.coding.prompt = ""
	a.coding.slug = ""
	a.pluginTab = "installed"
	a.pluginDetail = ""
	a.route = routePlugins
	a.invalidate()
}

func (a *App) creatorView(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(12).Padding(16).Radius(14).Background(p.card).Border(1, p.hairline).Children(func() {
		title := "创建个人应用"
		if a.coding.targetID != "" {
			title = "修改个人应用 · " + a.coding.targetID
		}
		ui.Text(c, title).FontSize(17).FontWeight(600).MaxLines(2)
		ui.Text(c, "描述你要使用的功能和数据来源。生成完成后，先审阅权限，再决定安装。").FontSize(12).TextColor(p.muted).MaxLines(3)
		ui.Text(c, "不要在需求中填写私钥或 API key；应用凭据在安装后的设置中填写。").FontSize(11).TextColor(p.muted).MaxLines(2)
		if a.coding.targetID == "" {
			ui.TextInput(c.Key("coding-slug"), &a.coding.slug).Label("应用短名，例如 my-pools")
		}
		ui.TextArea(c.Key("coding-prompt"), &a.coding.prompt).Label("需求").Height(130)
		ui.Row(c).Gap(8).Wrap().Children(func() {
			a.primaryButton(c, "开始生成", nil, a.coding.busy || a.coding.checking || strings.TrimSpace(a.coding.prompt) == "", a.startGeneration)
			if a.coding.busy {
				a.softButton(c, "取消生成", nil, p.danger, a.cancelGeneration)
			}
			a.softButton(c, "Agent 设置", nil, p.muted, func() { a.goTo(routeSettings) })
			a.softButton(c, "返回应用", nil, p.muted, func() { a.coding.open = false; a.invalidate() })
		})
		if a.coding.busy {
			ui.Text(c, "正在生成…").FontSize(12).TextColor(p.accent)
		}
		if a.coding.notice != "" {
			ui.Text(c, a.coding.notice).FontSize(12).TextColor(p.success).MaxLines(4)
		}
		if a.coding.err != "" {
			ui.Text(c, a.coding.err).FontSize(12).TextColor(p.danger).MaxLines(5)
		}
		if a.coding.progress != "" {
			ui.Text(c, a.coding.progress).Font("monospace").FontSize(10.5).TextColor(p.muted).MaxLines(24)
		}
	})
}

func (a *App) startGeneration() {
	a.initAuthoring()
	if a.coding.service == nil || a.coding.busy || a.coding.checking || !a.saveCodingProfile() {
		return
	}
	prompt := strings.TrimSpace(a.coding.prompt)
	if prompt == "" || len(prompt) > 32<<10 {
		a.coding.err = "需求不能为空，且不能超过 32 KiB"
		a.invalidate()
		return
	}
	target := a.coding.targetID
	if target == "" {
		slug := strings.TrimSpace(a.coding.slug)
		if slug == "" {
			a.coding.err = "请填写应用短名"
			a.invalidate()
			return
		}
		target = "local.personal." + slug
		if len(slug) > 64 || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(slug) {
			a.coding.err = "短名只能使用小写字母、数字和连字符，最多 64 字符"
			a.invalidate()
			return
		}
	}
	profile, _ := a.codingProfile()
	catalog, _ := json.Marshal(agent.Catalog(true))
	var examples []plugin.Package
	for _, id := range []string{"com.example.gas", "com.example.feargreed"} {
		if pkg, _, err := plugin.OfficialPackage(id); err == nil {
			examples = append(examples, pkg)
		}
	}
	values := map[string]string{}
	for key, value := range a.coding.values {
		values[key] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	a.coding.cancel = cancel
	a.coding.busy = true
	a.coding.generation++
	generation := a.coding.generation
	a.coding.err, a.coding.notice, a.coding.progress = "", "", ""
	a.coding.installationID = ""
	request := authoring.Request{Profile: profile, Prompt: prompt, WorkspaceRoot: a.workspaceRoot(), TargetID: target, ExpectedHash: a.coding.expectedHash, ExistingDir: a.coding.existingDir, Catalog: catalog, Examples: examples, ConfigValues: values,
		RequestPermission: a.requestCodingPermission,
		OnUpdate: func(update acp.Update) {
			a.runOnMain(func() {
				if a.coding.generation != generation || !a.coding.busy {
					return
				}
				text := safeAgentText(update.Text, 4000)
				if text == "" {
					text = safeAgentText(update.Type, 120) + "\n"
				}
				a.coding.progress += text
				if len(a.coding.progress) > 24<<10 {
					a.coding.progress = string([]rune(a.coding.progress)[max(0, len([]rune(a.coding.progress))-12000):])
				}
				a.invalidate()
			})
		},
	}
	service := a.coding.service
	a.goAsync(func() {
		result, err := service.Run(ctx, request)
		cancelled := ctx.Err() != nil
		cancel()
		if cancelled && result.Installation.RequestID != "" {
			_, _ = a.pluginManager.Cancel(result.Installation.RequestID)
		}
		a.runOnMain(func() {
			if generation != a.coding.generation {
				return
			}
			a.coding.busy = false
			a.coding.cancel = nil
			if cancelled {
				a.coding.notice = "已取消生成"
			} else if err != nil {
				a.coding.err = "生成未完成：" + safeAgentText(err.Error(), 800)
			} else {
				a.coding.installationID = result.Installation.RequestID
				a.coding.notice = fmt.Sprintf("%s v%s 已通过校验，请在待确认区域审阅权限", result.Installation.Manifest.Name, result.Installation.Version)
				a.coding.initialization = result.Initialization
				a.coding.session = result.Session
			}
			a.invalidate()
		})
	})
	a.invalidate()
}

func (a *App) cancelGeneration() {
	if a.coding.cancel != nil {
		a.coding.cancel()
	}
	if service := a.coding.service; service != nil {
		a.goAsync(service.Cancel)
	}
	a.coding.notice = "正在取消…"
	a.invalidate()
}

func safeAgentText(text string, limit int) string {
	out := make([]rune, 0, min(len(text), limit))
	for _, r := range text {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return string(out)
}
