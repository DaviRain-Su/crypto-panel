package app

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/pages"
	"github.com/davirain-su/lintel/internal/plugin"
)

// pluginsView is the Apps page: the installed apps as a launcher, the
// settings of one app, and the store beside them.
func (a *App) pluginsView(c *ui.Context) {
	p := a.pal
	a.pluginTabs(c)
	a.pendingApplications(c)
	if a.pluginNotice != "" {
		ui.Text(c, a.pluginNotice).FontSize(11).TextColor(p.muted).MaxLines(2).
			Margin(0, 0, 12, 0)
	}
	a.provenanceLockNotice(c)
	if a.coding.open {
		a.creatorView(c)
		return
	}
	if a.pluginTab == "store" {
		a.storeView(c)
		return
	}
	if a.pluginDetail != "" {
		a.appDetail(c)
		return
	}
	if len(a.pluginItems) == 0 {
		a.emptyPlugins(c)
		return
	}
	a.appGrid(c)
}

func (a *App) provenanceLockNotice(c *ui.Context) {
	locked, err := a.pluginsLocked()
	if err != nil || len(locked) == 0 {
		return
	}
	p := a.pal
	ui.Column(c).Gap(8).Padding(14).Radius(12).Background(p.card).
		Border(1, p.hairline).Margin(0, 0, 12, 0).Children(func() {
		ui.Text(c, a.t(i18n.ProvenanceLockedNotice)).FontSize(12).TextColor(p.text).MaxLines(4)
		for _, item := range locked {
			id, name := item.Manifest.ID, item.Manifest.Name
			a.softButton(c, a.t(i18n.ProvenanceLockedAction, name), nil, p.accent, func() {
				a.askProvenanceConfirm(id, name)
			})
		}
	})
}

func (a *App) pluginsLocked() ([]plugin.Installed, error) {
	if a.plugins == nil {
		return nil, nil
	}
	return a.plugins.LockedLocalApps()
}

func (a *App) askProvenanceConfirm(id, name string) {
	manifest := plugin.Manifest{ID: id, Name: name}
	ask := a.confirmProvenance
	if ask == nil {
		ask = a.openProvenanceConfirm
	}
	ask(manifest, func(ok bool) {
		if !ok || a.plugins == nil {
			return
		}
		if err := a.plugins.ConfirmProvenance(id); err != nil {
			a.pluginNotice = err.Error()
			return
		}
		a.pluginNotice = a.t(i18n.ProvenanceLockedDone, name)
		a.refreshPlugins()
	})
}

// pluginTabs switches the Apps page between the launcher and the store,
// with the two ways to install a local plugin beside them.
func (a *App) pluginTabs(c *ui.Context) {
	p := a.pal
	ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Margin(0, 0, 16, 0).Children(func() {
		selected := 0
		if a.pluginTab == "store" {
			selected = 1
		}
		if ui.Tabs(c.Key("apps-tabs"), &selected, a.t(i18n.InstalledTab), a.t(i18n.StoreTab)).Changed() {
			if selected == 1 {
				a.setPluginTab("store")
			} else {
				a.setPluginTab("installed")
			}
		}
		ui.Box(c).Grow(1)
		a.softButton(c, a.t(i18n.CreateApp), iconSparkle, p.accent, func() { a.openCreator("") })
		// The empty state already offers the folder install, so the toolbar
		// does not repeat it.
		if len(a.pluginItems) > 0 {
			a.softButton(c, a.t(i18n.PluginInstall), nil, p.text, a.installPlugin)
		}
		a.softButton(c, a.t(i18n.AppZip), nil, p.text, a.installPluginZip)
	})
}

func (a *App) emptyPlugins(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(9).Padding(30).Center().Radius(14).
		Background(p.card).Border(1, p.hairline).Margin(24, 0, 0, 0).Children(func() {
		ui.Box(c).Size(46, 46).Radius(13).Background(p.accentSoft).Center().Children(func() {
			iconGrid.draw(c, 22, p.accent)
		})
		ui.Text(c, a.t(i18n.PluginEmpty)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.PluginEmptyWhy)).FontSize(12.5).TextColor(p.muted).
			TextAlign(ui.Center).MaxLines(3)
		ui.Row(c).Gap(8).Margin(6, 0, 0, 0).Children(func() {
			a.primaryButton(c, a.t(i18n.PluginInstall), nil, false, a.installPlugin)
			a.softButton(c, a.t(i18n.PluginOpenStore), iconStore, p.muted, func() {
				a.setPluginTab("store")
			})
		})
	})
}

// appGrid is the launcher: one tile per installed app, as a desktop's app
// folder.
func (a *App) appGrid(c *ui.Context) {
	// pageBox is the shared page column. A wide window gets three cards;
	// the default 1060 window stays at two. Each card stops growing past
	// 400 so a lone tile does not span the row.
	cols := 2
	if w := a.pageBox.Bounds(c).W; w >= 980 {
		cols = 3
	}
	ui.Grid(c).Columns(cols).Gap(12).JustifyItems(ui.Start).Children(func() {
		for _, item := range a.pluginItems {
			a.appTile(c, item)
		}
	})
}

// appTile is one installed app: its mark, name, description and the two
// ways in — open it, or open its settings.
func (a *App) appTile(c *ui.Context, item plugin.Installed) {
	p := a.pal
	if item.Err != "" {
		a.brokenAppTile(c, item)
		return
	}
	m := item.Manifest
	id := m.ID
	enabled := item.Enabled
	ui.Column(c).Key("app:"+id).Gap(10).Padding(16).Radius(14).MaxWidth(400).FillWidth().
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			a.pluginTile(c, m.Name)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.Name).FontSize(14).FontWeight(600).MaxLines(1)
					a.trustBadge(c, item.Official, !item.Signed && !item.Official)
				})
				ui.Text(c, "v"+m.Version).FontSize(10.5).TextColor(p.faint).MaxLines(1)
			})
			if !enabled {
				ui.Text(c, a.t(i18n.AppOff)).FontSize(10.5).FontWeight(600).
					TextColor(p.faint).MaxLines(1)
			}
		})
		if m.Description != "" {
			ui.Text(c, m.Description).FontSize(12).TextColor(p.muted).MaxLines(2)
		}
		if current := a.installationVerifications[id]; current != nil {
			ui.Text(c, installationVerificationMessage(current.result)).FontSize(11).TextColor(p.muted).MaxLines(2)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if enabled && len(m.Navigation) > 0 {
				a.softButton(c, a.t(i18n.AppOpen), nil, p.text, func() { a.openPluginApp(id) })
			}
			a.softButton(c, a.t(i18n.AppSettings), iconGear, p.muted, func() {
				a.openPluginDetail(id)
			})
		})
	})
}

func (a *App) brokenAppTile(c *ui.Context, item plugin.Installed) {
	p := a.pal
	name := filepath.Base(item.Dir)
	ui.Column(c).Key("app:"+name).Gap(8).Padding(16).Radius(14).MaxWidth(400).FillWidth().
		Background(p.card).Border(1, p.hairline).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			a.pluginTile(c, "?")
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, name).FontSize(13.5).FontWeight(600).MaxLines(1)
				ui.Text(c, a.t(i18n.AppBroken)).FontSize(12).TextColor(p.danger).MaxLines(1)
			})
		})
		ui.Text(c, a.pluginErrText(item.Err)).FontSize(12).TextColor(p.danger).MaxLines(3)
		ui.Row(c).Children(func() {
			a.softButton(c, a.t(i18n.Remove), iconTrash, p.danger, func() {
				a.removeBrokenPlugin(item.Dir)
			})
		})
	})
}

// appDetail is one app's own page: its switch, its pages, its settings and
// its logs, with the way back to the launcher at the top.
func (a *App) appDetail(c *ui.Context) {
	item, ok := a.installedPlugin(a.pluginDetail)
	if !ok {
		a.pluginDetail = ""
		a.invalidate()
		return
	}
	a.backButton(c, a.t(i18n.AppAll), a.closePluginDetail)
	if item.Err != "" {
		a.brokenAppTile(c, item)
		return
	}
	a.appCard(c, item)
}

// appCard is the manager of one app: what it is, whether it runs, its
// pages, its settings and its logs.
func (a *App) appCard(c *ui.Context, item plugin.Installed) {
	p := a.pal
	m := item.Manifest
	id := m.ID
	ui.Column(c).Key(id).Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			a.pluginTile(c, m.Name)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, m.Name).FontSize(13.5).FontWeight(600).MaxLines(1)
					a.trustBadge(c, item.Official, !item.Signed && !item.Official)
					ui.Text(c, "v"+m.Version).FontSize(10.5).TextColor(p.faint).MaxLines(1)
				})
				ui.Text(c, id).FontSize(10.5).TextColor(p.faint).MaxLines(1)
			})
			if on := a.pluginEnabled[id]; on != nil {
				if ui.Switch(c, on).Changed() {
					a.setPluginEnabled(id, *on)
				}
			}
		})
		if m.Description != "" {
			ui.Text(c, m.Description).FontSize(12).TextColor(p.muted).MaxLines(3)
		}
		if item.Enabled && len(m.Navigation) > 0 {
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, nav := range m.Navigation {
					page := nav.Page
					a.softButton(c, nav.Title, iconSparkle, p.text, func() { a.openPluginPage(id, page) })
				}
			})
		}
		if len(m.Settings) > 0 {
			a.pluginSettingsForm(c, m)
		}
		a.installationVerificationView(c, id)
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			a.softButton(c, a.t(i18n.PluginLogs), iconTerminal, p.text, func() {
				a.pluginLogsOpen[id] = !a.pluginLogsOpen[id]
				a.invalidate()
			})
			if !item.Official && !item.Signed && strings.HasPrefix(id, "local.") {
				a.softButton(c, a.t(i18n.EditApp), iconSparkle, p.accent, func() { a.openCreator(id) })
			}
			a.softButton(c, a.t(i18n.Remove), iconTrash, p.danger, func() { a.removePlugin(id) })
			if runner, ok := a.pluginRunners[id]; ok {
				if n := runner.Restarts(); n > 0 {
					ui.Text(c, a.t(i18n.PluginRestarted, n)).FontSize(11).
						TextColor(p.danger).Grow(1)
				}
			}
		})
		if a.pluginLogsOpen[id] {
			a.pluginLogsView(c, id)
		}
	})
}

// pluginTile is a plugin's mark: its first letter in the accent.
func (a *App) pluginTile(c *ui.Context, name string) {
	p := a.pal
	letter := "?"
	if runes := []rune(strings.TrimSpace(name)); len(runes) > 0 {
		letter = strings.ToUpper(string(runes[0]))
	}
	ui.Box(c).Size(34, 34).Radius(10).Background(p.accentSoft).Center().Children(func() {
		ui.Text(c, letter).FontSize(14).FontWeight(700).TextColor(p.accent)
	})
}

func (a *App) pluginSettingsForm(c *ui.Context, m plugin.Manifest) {
	p := a.pal
	ui.Column(c).Gap(8).Children(func() {
		for _, field := range m.Settings {
			key := m.ID + "/" + field.Key
			draft := a.pluginDrafts[key]
			if draft == nil {
				empty := ""
				draft = &empty
				a.pluginDrafts[key] = draft
			}
			ui.Column(c).Gap(4).Children(func() {
				input := ui.TextInput(c.Key("plugin-setting-"+key), draft).Label(field.Label)
				if field.Secret {
					input.Password()
					configured := false
					if statuses, err := a.plugins.DescribeSettings(m.ID); err == nil {
						for _, status := range statuses {
							if status.Key == field.Key && status.Configured != nil {
								configured = *status.Configured
							}
						}
					}
					if configured {
						ui.Row(c).Gap(8).Children(func() {
							ui.Text(c, a.t(i18n.SecretConfigured)).FontSize(12).TextColor(p.muted)
							a.softButton(c, a.t(i18n.SecretClear), nil, p.danger, func() { a.clearPluginSecret(m.ID, field.Key) })
						})
					}
				}
				if field.Help != "" {
					ui.Text(c, field.Help).FontSize(11).TextColor(p.muted).MaxLines(2)
				}
			})
		}
		ui.Row(c).Children(func() {
			a.primaryButton(c, a.t(i18n.PluginSave), nil, false, func() { a.savePluginSettings(m) })
		})
	})
}

func (a *App) pluginLogsView(c *ui.Context, id string) {
	p := a.pal
	entries := a.pluginLog(id).Entries()
	if len(entries) == 0 {
		ui.Text(c, a.t(i18n.PluginLogsEmpty)).FontSize(11).TextColor(p.muted)
		return
	}
	if len(entries) > 20 {
		entries = entries[len(entries)-20:]
	}
	ui.Column(c).Gap(2).Padding(10).Background(p.inset).Radius(8).Children(func() {
		for _, entry := range entries {
			color := p.muted
			switch entry.Level {
			case "warn":
				color = p.danger
			case "error":
				color = p.danger
			}
			ui.Text(c, entry.Time.Format("15:04:05")+"  "+entry.Level+"  "+entry.Message).
				Font("monospace").FontSize(10).TextColor(color).MaxLines(2)
		}
	})
}

// trustBadge marks an app as an official example or as an unsigned local
// install. A store install is signed and carries neither badge.
func (a *App) trustBadge(c *ui.Context, official, unsigned bool) {
	switch {
	case official:
		ui.Badge(c, a.t(i18n.PluginOfficial))
	case unsigned:
		ui.Badge(c, a.t(i18n.PluginUnsigned))
	}
}

// pluginPageView draws one page of a plugin. The header (the plugin's name,
// the official or unsigned badge, and the refresh button) is the shell's,
// never the plugin's to draw.
func (a *App) pluginPageView(c *ui.Context) {
	p := a.pal
	a.pluginPageTabs(c)
	view := a.pluginView
	if view.key != a.pluginKey() {
		return
	}
	if view.err != "" && !view.hasPage {
		a.noticeCard(c, a.t(i18n.PluginErrorTitle), view.err, true)
		return
	}
	if view.loading && !view.hasPage {
		a.noticeCard(c, a.t(i18n.PluginLoadingTitle), a.t(i18n.Refreshing), false)
		return
	}
	if view.loading {
		a.noticeCard(c, a.t(i18n.Refreshing), a.t(i18n.PluginStale), false)
	}
	if view.err != "" {
		a.noticeCard(c, a.t(i18n.PluginErrorTitle), view.err, true)
	}
	if !view.updated.IsZero() {
		ui.Text(c, a.t(i18n.PluginRefreshedAt, view.updated.Local().Format("15:04:05"))).
			FontSize(12).TextColor(p.muted)
	}
	if view.hasPage || (view.err == "" && !view.loading) {
		pages.RenderBody(c, view.page, func(page string) { a.openPluginPage(a.route.id, page) })
	}
}

// noticeCard is the shared empty, error and loading block.
func (a *App) noticeCard(c *ui.Context, title, body string, danger bool) {
	p := a.pal
	color := p.muted
	if danger {
		color = p.danger
	}
	ui.Column(c).Gap(8).Padding(22).Radius(14).Background(p.card).
		Border(1, p.hairline).Margin(8, 0, 0, 0).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(700).TextColor(color)
		if body != "" {
			ui.Text(c, body).FontSize(12.5).TextColor(color).MaxLines(6)
		}
	})
}

// pluginErrText translates the errors the shell itself wrote. A checker's
// own sentence is shown as it arrived.
func (a *App) pluginErrText(err string) string {
	if err == plugin.ErrBrokenManifest {
		return a.t(i18n.BrokenManifest)
	}
	return err
}

// pluginPageTabs switches between the pages an app declares, when it has
// more than one.
func (a *App) pluginPageTabs(c *ui.Context) {
	item, ok := a.installedPlugin(a.route.id)
	if !ok || len(item.Manifest.Navigation) < 2 {
		return
	}
	labels := make([]string, len(item.Manifest.Navigation))
	selected := 0
	for i, nav := range item.Manifest.Navigation {
		labels[i] = nav.Title
		if nav.Page == a.route.page {
			selected = i
		}
	}
	if ui.Tabs(c.Key("plugin-pages"), &selected, labels...).Margin(0, 0, 16, 0).Changed() {
		a.openPluginPage(a.route.id, item.Manifest.Navigation[selected].Page)
	}
}
