package app

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/pages"
	"github.com/davirain-su/crypto-panel/internal/plugin"
)

// pluginsView is the Plugins page: the manager of the installed plugins,
// and the store beside it.
func (a *App) pluginsView(c *ui.Context) {
	p := a.pal
	a.pluginTabs(c)
	if a.pluginNotice != "" {
		ui.Text(c, a.pluginNotice).FontSize(11).TextColor(p.muted).MaxLines(2).Margin(0, 0, 10, 0)
	}
	if a.pluginTab == "store" {
		a.storeView(c)
		return
	}
	if len(a.pluginItems) == 0 {
		a.emptyPlugins(c)
		return
	}
	ui.Column(c).Gap(10).Children(func() {
		for _, item := range a.pluginItems {
			a.pluginCard(c, item)
		}
	})
}

func (a *App) emptyPlugins(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(28).Center().Radius(12).
		Background(p.card).Border(1, p.hairline).Margin(20, 0, 0, 0).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconGrid).Size(22, 22).TextColor(p.accent)
		})
		ui.Text(c, a.t(i18n.PluginEmpty)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.PluginEmptyWhy)).FontSize(12.5).TextColor(p.muted).TextAlign(ui.Center)
	})
}

func (a *App) pluginCard(c *ui.Context, item plugin.Installed) {
	p := a.pal
	if item.Err != "" {
		ui.Column(c).Gap(3).Padding(16).Radius(12).
			Background(p.card).Border(1, p.hairline).Children(func() {
			ui.Text(c, filepath.Base(item.Dir)).FontSize(13).FontWeight(600)
			ui.Text(c, item.Err).FontSize(11).TextColor(p.danger).MaxLines(3)
		})
		return
	}
	m := item.Manifest
	id := m.ID
	ui.Column(c).Key(id).Gap(10).Padding(16).Radius(12).
		Background(p.card).Border(1, p.hairline).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, m.Name).FontSize(14).FontWeight(600).MaxLines(1)
			if !item.Signed {
				ui.Badge(c, a.t(i18n.PluginUnsigned))
			}
			ui.Text(c, "v"+m.Version).FontSize(11).TextColor(p.muted).Grow(1)
			if on := a.pluginEnabled[id]; on != nil {
				if ui.Switch(c, on).Changed() {
					a.setPluginEnabled(id, *on)
				}
			}
		})
		ui.Text(c, id).FontSize(11).TextColor(p.faint).MaxLines(1)
		if m.Description != "" {
			ui.Text(c, m.Description).FontSize(12.5).TextColor(p.muted).MaxLines(3)
		}
		if item.Enabled && len(m.Navigation) > 0 {
			ui.Row(c).Gap(6).Children(func() {
				for _, nav := range m.Navigation {
					page := nav.Page
					a.softButton(c, nav.Title, iconSparkle, p.text, func() { a.openPluginPage(id, page) })
				}
			})
		}
		if len(m.Settings) > 0 {
			a.pluginSettingsForm(c, m)
		}
		ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
			a.softButton(c, a.t(i18n.PluginLogs), iconTerminal, p.text, func() {
				a.pluginLogsOpen[id] = !a.pluginLogsOpen[id]
				a.invalidate()
			})
			a.softButton(c, a.t(i18n.Remove), iconTrash, p.danger, func() { a.removePlugin(id) })
			if runner, ok := a.pluginRunners[id]; ok {
				if n := runner.Restarts(); n > 0 {
					ui.Text(c, a.t(i18n.PluginRestarted, n)).FontSize(11).TextColor(p.danger).Grow(1)
				}
			}
		})
		if a.pluginLogsOpen[id] {
			a.pluginLogsView(c, id)
		}
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
				}
				if field.Help != "" {
					ui.Text(c, field.Help).FontSize(11).TextColor(p.muted).MaxLines(2)
				}
			})
		}
		ui.Row(c).Children(func() {
			ui.Button(c, a.t(i18n.PluginSave)).OnClick(func() { a.savePluginSettings(m) })
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
	ui.Column(c).Gap(2).Padding(10).Background(p.bg).Radius(8).Children(func() {
		for _, entry := range entries {
			color := p.muted
			switch entry.Level {
			case "warn":
				color = p.danger
			case "error":
				color = p.danger
			}
			ui.Text(c, entry.Time.Format("15:04:05")+"  "+entry.Level+"  "+entry.Message).
				FontSize(10).TextColor(color).MaxLines(2)
		}
	})
}

// pluginPageView draws one page of a plugin. The header (the plugin's name,
// the unsigned badge and the refresh button) is the shell's, never the
// plugin's to draw.
func (a *App) pluginPageView(c *ui.Context) {
	p := a.pal
	view := a.pluginView
	switch {
	case view.key != a.pluginKey():
		// A page is being opened; the next frame draws it.
	case view.err != "":
		ui.Text(c, view.err).FontSize(12).TextColor(p.danger).MaxLines(6)
	case view.loading:
		ui.Text(c, a.t(i18n.Refreshing)).FontSize(12).TextColor(p.muted)
	default:
		// The window's header already names the page, so the page's own
		// title is not drawn again.
		pages.RenderBody(c, view.page, func(page string) {
			a.openPluginPage(a.route.id, page)
		})
	}
}
