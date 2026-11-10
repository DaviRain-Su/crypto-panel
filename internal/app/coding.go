package app

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/pages"
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
	preview                                           candidatePreview
}

type candidatePreview struct {
	requestID  string
	manifest   plugin.Manifest
	page       string
	result     pages.Page
	updated    time.Time
	loading    bool
	err        string
	generation int
}

func (a *App) codingProfiles() []authoring.Profile {
	return []authoring.Profile{
		{ID: "claude", Name: "Claude Code (ACP)", Executable: "claude-agent-acp"},
		{ID: "codex", Name: "Codex (ACP)", Executable: "codex-acp"},
		{ID: "custom", Name: a.t(i18n.CreatorCustomAgent)},
	}
}

func (a *App) initAuthoring() {
	if a.coding.initialized {
		return
	}
	a.coding.initialized = true
	_ = authoring.SweepStaleWorkspaces(a.workspaceRoot())
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
	for _, preset := range a.codingProfiles() {
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
		profile = a.codingProfiles()[0]
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
	for _, preset := range a.codingProfiles() {
		if preset.ID == p.ID {
			p.Name = preset.Name
		}
	}
	if p.Executable == "" || len(p.Executable) > 4096 || strings.ContainsAny(p.Executable, "\x00\r\n") {
		return p, errors.New(a.t(i18n.CodingNeedExecutable))
	}
	if err := json.Unmarshal([]byte(a.coding.args), &p.Args); err != nil || p.Args == nil || len(p.Args) > 32 {
		return p, errors.New(a.t(i18n.CodingArgsJSON))
	}
	for _, arg := range p.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return p, errors.New(a.t(i18n.CodingArgsInvalid))
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

func codingSelectChoices(option acp.ConfigOption) []acp.ConfigValue {
	choices := append([]acp.ConfigValue{}, option.Options...)
	for _, group := range option.Groups {
		choices = append(choices, group.Options...)
	}
	if len(choices) > acp.MaxConfigValues {
		choices = choices[:acp.MaxConfigValues]
	}
	return choices
}

func (a *App) codingSettings(c *ui.Context) {
	a.initAuthoring()
	p := a.pal
	ui.Column(c).Gap(10).Padding(14, 16).Children(func() {
		ui.Text(c, a.t(i18n.CodingIntro)).FontSize(12.5).FontWeight(600)
		ui.Text(c, a.t(i18n.CodingIntroBody)).FontSize(12).TextColor(p.muted).MaxLines(3)
		names := []string{}
		selected := ""
		for _, profile := range a.codingProfiles() {
			names = append(names, profile.Name)
			if profile.ID == a.coding.profileID {
				selected = profile.Name
			}
		}
		if ui.Select(c.Key("coding-profile"), &selected, names).Changed() {
			for _, profile := range a.codingProfiles() {
				if profile.Name == selected {
					a.selectCodingProfile(profile.ID)
					break
				}
			}
		}
		ui.TextInput(c.Key("coding-executable"), &a.coding.executable).Label(a.t(i18n.CodingExecutable))
		ui.TextInput(c.Key("coding-args"), &a.coding.args).Label(a.t(i18n.CodingArgs))
		ui.Row(c).Gap(8).Children(func() {
			a.primaryButton(c, a.t(i18n.CodingSave), nil, a.coding.busy || a.coding.checking, func() { a.saveCodingProfile() })
			a.softButton(c, a.t(i18n.CodingTest), nil, p.muted, func() {
				if a.coding.busy || a.coding.checking {
					return
				}
				a.checkCodingAgent()
			})
		})
		if info := a.coding.initialization.AgentInfo; info != nil {
			ui.Text(c, a.t(i18n.CodingConnected, safeAgentText(info.Name, 120), safeAgentText(info.Version, 80))).FontSize(12).TextColor(p.success)
		}
		for _, option := range a.coding.session.ConfigOptions {
			if option.Type != "select" {
				continue
			}
			choices := codingSelectChoices(option)
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
			ui.Text(c, a.t(i18n.CodingChecking)).FontSize(12).TextColor(p.muted)
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
				a.coding.err = a.t(i18n.CodingConnectFailed, safeAgentText(err.Error(), 600))
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
			a.pluginNotice = a.t(i18n.CodingPersonalOnly)
			a.invalidate()
			return
		}
		hash, err := a.plugins.PackageHash(id)
		if err != nil {
			a.pluginNotice = a.t(i18n.CodingCannotVerify)
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
		title := a.t(i18n.CreateAppTitle)
		if a.coding.targetID != "" {
			title = a.t(i18n.EditAppTitle, a.coding.targetID)
		}
		ui.Text(c, title).FontSize(17).FontWeight(600).MaxLines(2)
		ui.Text(c, a.t(i18n.CreateAppBody)).FontSize(12).TextColor(p.muted).MaxLines(3)
		ui.Text(c, a.t(i18n.CreateAppSecret)).FontSize(12).TextColor(p.muted).MaxLines(2)
		if a.coding.targetID == "" {
			ui.TextInput(c.Key("coding-slug"), &a.coding.slug).Label(a.t(i18n.CreateAppSlug))
		}
		ui.TextArea(c.Key("coding-prompt"), &a.coding.prompt).Label(a.t(i18n.CreateAppPrompt)).Height(130)
		ui.Row(c).Gap(8).Wrap().Children(func() {
			a.primaryButton(c, a.t(i18n.CreateAppStart), nil, a.coding.busy || a.coding.checking || strings.TrimSpace(a.coding.prompt) == "", a.startGeneration)
			if a.coding.busy {
				a.softButton(c, a.t(i18n.CreateAppCancel), nil, p.danger, a.cancelGeneration)
			}
			a.softButton(c, a.t(i18n.CreateAppSettings), nil, p.muted, func() { a.goTo(routeSettings) })
			a.softButton(c, a.t(i18n.CreateAppBack), nil, p.muted, func() { a.coding.open = false; a.invalidate() })
		})
		if a.coding.installationID != "" && !a.coding.busy {
			requestID := a.coding.installationID
			ui.Row(c).Gap(8).Wrap().Children(func() {
				a.primaryButton(c, a.t(i18n.CodingPreview), nil, a.coding.preview.loading, a.previewGeneratedApp)
				a.softButton(c, a.t(i18n.PendingReview), nil, p.accent, func() { a.reviewInstallation(requestID) })
			})
		}
		if a.coding.busy {
			ui.Text(c, a.t(i18n.CreateAppWorking)).FontSize(12).TextColor(p.accent)
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
		a.coding.err = a.t(i18n.CodingPromptEmpty)
		a.invalidate()
		return
	}
	target := a.coding.targetID
	if target == "" {
		slug := strings.TrimSpace(a.coding.slug)
		if slug == "" {
			a.coding.err = a.t(i18n.CodingSlugRequired)
			a.invalidate()
			return
		}
		target = "local.personal." + slug
		if len(slug) > 64 || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(slug) {
			a.coding.err = a.t(i18n.CodingSlugInvalid)
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
		// 调用方拥有工作目录：Run 返回后即可回收，冻结候选已复制到 Manager。
		if result.Workspace != "" {
			_ = os.RemoveAll(result.Workspace)
		}
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
				a.coding.notice = a.t(i18n.CodingCanceled)
			} else if err != nil {
				a.coding.err = a.t(i18n.CodingIncomplete, safeAgentText(a.pluginErrorText(err), 800))
			} else {
				a.coding.installationID = result.Installation.RequestID
				a.coding.notice = a.t(i18n.CodingValidated, result.Installation.Manifest.Name, result.Installation.Version)
				a.coding.initialization = result.Initialization
				a.coding.session = result.Session
			}
			a.invalidate()
		})
	})
	a.invalidate()
}

func (a *App) previewGeneratedApp() {
	if a.coding.installationID == "" || a.coding.preview.loading {
		return
	}
	requestID := a.coding.installationID
	job, err := a.pluginManager.GetInstallation(requestID)
	if err != nil || job.Status != "pending" || job.Via != "acp" {
		a.coding.err = a.t(i18n.PendingExpired)
		a.invalidate()
		return
	}
	if len(job.Manifest.Navigation) == 0 {
		a.coding.err = a.t(i18n.AppNoPages)
		a.invalidate()
		return
	}
	a.coding.preview.requestID = requestID
	a.coding.preview.manifest = job.Manifest
	a.coding.preview.page = job.Manifest.Navigation[0].Page
	a.route = route{kind: "preview"}
	a.loadGeneratedPreview("")
	a.invalidate()
}

func (a *App) loadGeneratedPreview(page string) {
	preview := &a.coding.preview
	if preview.requestID == "" || preview.loading {
		return
	}
	if page == "" {
		page = preview.page
	}
	requestID := preview.requestID
	preview.page = page
	preview.result = pages.Page{}
	preview.updated = time.Time{}
	preview.loading = true
	preview.err = ""
	preview.generation++
	generation := preview.generation
	a.goAsync(func() {
		result, err := a.PreviewInstallationPage(requestID, page, "acp")
		a.runOnMain(func() {
			if a.coding.preview.requestID != requestID || a.coding.preview.generation != generation || a.route.kind != "preview" {
				return
			}
			a.coding.preview.loading = false
			if err != nil {
				a.coding.preview.err = a.pluginErrorText(err)
			} else if value, ok := result.Page.(pages.Page); ok {
				a.coding.preview.result = value
				a.coding.preview.updated = result.EvaluatedAt
			} else {
				a.coding.preview.err = a.t(i18n.PluginLoadFailed)
			}
			a.invalidate()
		})
	})
}

func (a *App) closeGeneratedPreview() {
	a.coding.preview = candidatePreview{}
	a.route = routePlugins
	a.invalidate()
}

func (a *App) generatedPreviewView(c *ui.Context) {
	p := a.pal
	preview := a.coding.preview
	ui.Column(c).Gap(12).Children(func() {
		ui.Row(c).Gap(8).Wrap().Children(func() {
			a.softButton(c, a.t(i18n.CodingPreviewBack), nil, p.muted, a.closeGeneratedPreview)
			if preview.requestID != "" {
				a.softButton(c, a.t(i18n.PendingReview), nil, p.accent, func() { a.reviewInstallation(preview.requestID) })
			}
		})
		ui.Text(c, a.t(i18n.CodingPreviewNotice)).FontSize(12).TextColor(p.muted).MaxLines(3)
		if len(preview.manifest.Navigation) > 1 {
			labels := make([]string, len(preview.manifest.Navigation))
			selected := 0
			for i, nav := range preview.manifest.Navigation {
				labels[i] = nav.Title
				if nav.Page == preview.page {
					selected = i
				}
			}
			if ui.Tabs(c.Key("candidate-preview-pages"), &selected, labels...).Changed() {
				a.loadGeneratedPreview(preview.manifest.Navigation[selected].Page)
			}
		}
		if preview.err != "" {
			a.noticeCard(c, a.t(i18n.PluginErrorTitle), preview.err, true)
		}
		if preview.loading && preview.updated.IsZero() {
			a.noticeCard(c, a.t(i18n.PluginLoadingTitle), a.t(i18n.Refreshing), false)
			return
		}
		if preview.loading {
			ui.Text(c, a.t(i18n.Refreshing)).FontSize(12).TextColor(p.muted)
		}
		pages.Render(c, preview.result, func(page string) { a.loadGeneratedPreview(page) })
	})
}

func (a *App) cancelGeneration() {
	if a.coding.cancel != nil {
		a.coding.cancel()
	}
	if service := a.coding.service; service != nil {
		a.goAsync(service.Cancel)
	}
	a.coding.notice = a.t(i18n.CodingCanceling)
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
