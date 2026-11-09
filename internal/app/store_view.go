package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

// storeView is the catalog: one card per entry, with Install, Update or
// Installed, and the source it came from. With no registry set there is no
// catalog, only the way to set one.
func (a *App) storeView(c *ui.Context) {
	p := a.pal
	if a.settings.PluginStoreURL == "" {
		a.noRegistryStore(c)
		return
	}
	if a.storeErr == "" && len(a.storeItems) > 0 {
		ui.SearchField(c, &a.storeQuery).Label(a.t(i18n.StoreSearch)).
			Placeholder(a.t(i18n.StoreSearch)).Width(280).Margin(0, 0, 12, 0)
	}
	entries := a.filteredStoreItems()
	switch {
	case a.storeErr != "":
		ui.Column(c).Gap(8).Padding(20).Radius(12).Background(p.card).
			Border(1, p.hairline).Children(func() {
			ui.Text(c, a.t(i18n.StoreLoadFailed, a.storeErr)).FontSize(12.5).
				TextColor(p.danger).MaxLines(4)
			ui.Text(c, a.t(i18n.StoreSource, a.storeSourceName())).FontSize(11).
				TextColor(p.muted).MaxLines(2)
			ui.Row(c).Children(func() {
				ui.Button(c, a.t(i18n.StoreRetry)).OnClick(a.loadStore)
			})
		})
	case a.storeLoading && len(a.storeItems) == 0:
		ui.Text(c, a.t(i18n.StoreLoading)).FontSize(12).TextColor(p.muted)
	case len(a.storeItems) == 0:
		a.emptyStore(c)
	case len(entries) == 0:
		ui.Text(c, a.t(i18n.StoreNoMatches, strings.TrimSpace(a.storeQuery))).
			FontSize(12).TextColor(p.muted).MaxLines(2)
	default:
		ui.Column(c).Gap(10).Children(func() {
			for _, entry := range entries {
				a.storeCard(c, entry)
			}
		})
	}
	if a.storeErr == "" && len(a.storeItems) > 0 {
		ui.Text(c, a.t(i18n.StoreSource, a.storeSourceName())).FontSize(11).
			TextColor(p.faint).MaxLines(1).Margin(14, 0, 0, 0)
	}
}

// filteredStoreItems narrows the catalog to what the search box asks for:
// a name, an id, an author or a word of the description.
func (a *App) filteredStoreItems() []plugin.RegistryEntry {
	q := strings.ToLower(strings.TrimSpace(a.storeQuery))
	if q == "" {
		return a.storeItems
	}
	out := make([]plugin.RegistryEntry, 0, len(a.storeItems))
	for _, entry := range a.storeItems {
		hay := strings.ToLower(entry.Name + " " + entry.ID + " " + entry.Author + " " + entry.Description)
		if strings.Contains(hay, q) {
			out = append(out, entry)
		}
	}
	return out
}

// noRegistryStore is the store before a registry is set: the app ships no
// catalog of its own.
func (a *App) noRegistryStore(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(28).Center().Radius(12).
		Background(p.card).Border(1, p.hairline).Margin(20, 0, 0, 0).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Background(p.accentSoft).Center().Children(func() {
			iconStore.draw(c, 22, p.accent)
		})
		ui.Text(c, a.t(i18n.StoreNoRegistry)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.StoreNoRegistryWhy)).FontSize(12.5).TextColor(p.muted).
			TextAlign(ui.Center).MaxLines(3)
		ui.Button(c, a.t(i18n.StoreOpenSettings)).Margin(6, 0, 0, 0).OnClick(func() {
			a.goTo(routeSettings)
		})
	})
}

func (a *App) emptyStore(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(28).Center().Radius(12).
		Background(p.card).Border(1, p.hairline).Margin(20, 0, 0, 0).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Background(p.accentSoft).Center().Children(func() {
			iconStore.draw(c, 22, p.accent)
		})
		ui.Text(c, a.t(i18n.StoreEmpty)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.StoreEmptyWhy)).FontSize(12.5).TextColor(p.muted).TextAlign(ui.Center)
	})
}

func (a *App) storeCard(c *ui.Context, entry plugin.RegistryEntry) {
	p := a.pal
	inst, update := a.storeState(entry)
	ui.Column(c).Key("store:"+entry.ID).Gap(10).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			a.pluginTile(c, entry.Name)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, entry.Name).FontSize(13.5).FontWeight(600).MaxLines(1)
					ui.Text(c, "v"+entry.Version).FontSize(10.5).TextColor(p.faint)
				})
				line := entry.ID
				if entry.Author != "" {
					line += " · " + a.t(i18n.StoreBy, entry.Author)
				}
				ui.Text(c, line).FontSize(10.5).TextColor(p.faint).MaxLines(1)
			})
			switch {
			case a.storeInstalling == entry.ID:
				ui.Spinner(c).Size(16, 16)
			case inst == nil:
				a.primaryButton(c, a.t(i18n.StoreInstall), nil, a.storeInstalling != "", func() {
					a.installFromStore(entry)
				})
			case update:
				a.primaryButton(c, a.t(i18n.StoreUpdate), nil, a.storeInstalling != "", func() {
					a.installFromStore(entry)
				})
			default:
				ui.Text(c, a.t(i18n.StoreInstalled)).FontSize(11.5).
					FontWeight(600).TextColor(p.success)
			}
		})
		if entry.Description != "" {
			ui.Text(c, entry.Description).FontSize(12).TextColor(p.muted).MaxLines(3)
		}
		if inst != nil && inst.Err != "" {
			ui.Text(c, inst.Err).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
	})
}

// storeState finds the installed plugin an entry names, and whether the
// entry is newer than it.
func (a *App) storeState(entry plugin.RegistryEntry) (*plugin.Installed, bool) {
	for i := range a.pluginItems {
		item := &a.pluginItems[i]
		if item.Manifest.ID != entry.ID {
			continue
		}
		if item.Err != "" {
			return item, true
		}
		return item, plugin.CompareVersions(entry.Version, item.Manifest.Version) > 0
	}
	return nil, false
}

// openProvenanceConfirm is the native approval for re-saving one personal
// app's install provenance after a credential-store failure. It does not
// read install.json; ConfirmProvenance runs only after the user agrees.
func (a *App) openProvenanceConfirm(m plugin.Manifest, done func(bool)) {
	if a.win == nil || a.installOpen {
		done(false)
		return
	}
	a.installOpen = true
	var win *mygo.Window
	finish := func(ok bool) {
		a.installOpen = false
		if win != nil {
			win.Close()
		}
		done(ok)
	}
	var decided bool
	decide := func(ok bool) {
		if decided {
			return
		}
		decided = true
		finish(ok)
	}
	win = mygo.NewWindow(mygo.WindowOptions{
		Title:           a.t(i18n.ProvenanceLockedTitle),
		Width:           460,
		Height:          360,
		MinWidth:        400,
		MinHeight:       300,
		Parent:          a.win,
		Modal:           true,
		BackgroundColor: "light-dark(#f6f4ef, #141513)",
		Content: ui.View(func(c *ui.Context) {
			p := a.pal
			c.Root().Background(p.bg)
			ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
				ui.Text(c, a.t(i18n.ProvenanceLockedTitle)).FontSize(20).FontWeight(700).MaxLines(2)
				ui.Text(c, a.t(i18n.ProvenanceLockedBody, m.Name)).FontSize(13).MaxLines(5)
				ui.Row(c).Gap(8).Children(func() {
					a.primaryButton(c, a.t(i18n.ProvenanceLockedAction, m.Name), nil, false, func() { decide(true) })
					a.softButton(c, a.t(i18n.Cancel), nil, p.muted, func() { decide(false) })
				})
			})
		}),
	})
	win.OnClose(func(*mygo.CloseEvent) { decide(false) })
	win.Focus()
}

// askInstall is the shell's permission prompt: what the plugin may do, in
// the shell's words, and two buttons. It never blocks the main thread; the
// answer arrives through done.
func (a *App) askInstall(m plugin.Manifest, req installRequest, done func(bool)) {
	if a.win == nil || a.installOpen {
		done(false)
		return
	}
	a.installOpen = true
	ip := &installPrompt{t: a.t, m: m, req: req}
	var win *mygo.Window
	ip.onDecide = func(ok bool) {
		a.installOpen = false
		if win != nil {
			win.Close()
		}
		done(ok)
	}
	win = mygo.NewWindow(mygo.WindowOptions{
		Title:           ip.title(),
		Width:           460,
		Height:          620,
		MinWidth:        420,
		MinHeight:       520,
		Parent:          a.win,
		Modal:           true,
		BackgroundColor: "light-dark(#f6f4ef, #141513)",
		Content:         ui.View(ip.view),
	})
	win.OnClose(func(*mygo.CloseEvent) { ip.decide(false) })
	win.Focus()
}

// installPrompt is one install's permission window.
type installPrompt struct {
	t   func(i18n.Key, ...any) string
	m   plugin.Manifest
	req installRequest

	decided  bool
	onDecide func(bool)
}

func (ip *installPrompt) decide(ok bool) {
	if ip.decided {
		return
	}
	ip.decided = true
	if ip.onDecide != nil {
		ip.onDecide(ok)
	}
}

func (ip *installPrompt) view(c *ui.Context) {
	t := c.Theme()
	p := paletteFor(t)
	useAccent(t, p)
	c.Root().Background(p.bg)
	ui.Column(c).Fill().Padding(20).Gap(12).Children(func() {
		kicker := i18n.StoreKickerInstall
		if ip.req.unsigned {
			kicker = i18n.StoreInstall
		}
		if ip.req.update {
			kicker = i18n.StoreKickerUpdate
		}
		ui.Text(c, strings.ToUpper(ip.t(kicker))).FontSize(11).FontWeight(600).
			TextColor(p.accent).LetterSpacing(0.6).MaxLines(1)
		ui.Text(c, ip.title()).FontSize(22).FontWeight(700).LetterSpacing(-0.4).MaxLines(2)
		ui.Column(c).Gap(4).Padding(14).Radius(10).Background(p.card).
			Border(1, p.hairline).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, ip.m.Name).FontSize(14).FontWeight(600).MaxLines(1)
				ui.Text(c, "v"+ip.m.Version).FontSize(11).TextColor(p.muted)
			})
			if ip.m.Author != "" {
				ui.Text(c, ip.t(i18n.StoreBy, ip.m.Author)).FontSize(11).TextColor(p.faint).MaxLines(1)
			}
			if ip.m.Description != "" {
				ui.Text(c, ip.m.Description).FontSize(12).TextColor(p.muted).MaxLines(3)
			}
			if ip.req.source != "" {
				ui.Text(c, ip.t(i18n.StoreFrom, ip.req.source)).FontSize(10.5).
					TextColor(p.faint).MaxLines(1)
			}
		})
		ui.Text(c, ip.t(i18n.StorePermissionTitle)).FontSize(11).FontWeight(600).
			TextColor(p.muted)
		ui.Column(c).Gap(5).Children(func() {
			for _, line := range permissionLines(ip.t, ip.m) {
				ui.Row(c).Gap(7).AlignItems(ui.Start).Children(func() {
					ui.Text(c, "•").FontSize(12).TextColor(p.muted)
					ui.Text(c, line).FontSize(12).Grow(1).MaxLines(2)
				})
			}
		})
		if added := ip.added(); len(added) > 0 {
			ui.Text(c, ip.t(i18n.StoreNewPerms, strings.Join(added, " · "))).
				FontSize(11).TextColor(p.danger).MaxLines(3)
		}
		if ip.m.Permissions.Sign {
			ui.Text(c, ip.t(i18n.StoreSignWarn)).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
		if ip.req.unsigned {
			ui.Text(c, ip.t(i18n.StoreUnsignedWarn)).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
		ui.Text(c, ip.t(i18n.StoreInstallBody)).FontSize(10.5).TextColor(p.faint).MaxLines(2)
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
			ui.Button(c, ip.t(i18n.Cancel)).OnClick(func() { ip.decide(false) })
			label := i18n.StoreInstall
			if ip.req.update {
				label = i18n.StoreUpdate
			}
			ui.PrimaryButton(c, ip.t(label)).OnClick(func() { ip.decide(true) })
		})
	})
}

func (ip *installPrompt) title() string {
	if ip.req.update {
		return ip.t(i18n.StoreUpdateTitle, ip.m.Name, ip.m.Version)
	}
	return ip.t(i18n.StoreInstallTitle, ip.m.Name)
}

// added lists the permissions an update asks for that the installed version
// did not have.
func (ip *installPrompt) added() []string {
	if !ip.req.update || ip.req.old == nil {
		return nil
	}
	old, next := ip.req.old.Permissions, ip.m.Permissions
	var out []string
	for _, host := range next.Network {
		if !slices.Contains(old.Network, host) {
			out = append(out, ip.t(i18n.StorePermNetwork, host))
		}
	}
	for _, id := range next.Chains {
		if slices.Contains(old.Chains, id) {
			continue
		}
		name := id
		if n, ok := chains.ByID(id); ok {
			name = n.Name
		}
		out = append(out, ip.t(i18n.StorePermChains, name))
	}
	if slices.Contains(next.Read, "balances") && !slices.Contains(old.Read, "balances") {
		out = append(out, ip.t(i18n.StorePermRead))
	}
	if next.Sign && !old.Sign {
		out = append(out, ip.t(i18n.StorePermSign))
	}
	if next.StorageKb > old.StorageKb {
		out = append(out, ip.t(i18n.StorePermStorage, next.StorageKb))
	}
	return out
}

// permissionLines says what a manifest may do, in the shell's words.
func permissionLines(t func(i18n.Key, ...any) string, m plugin.Manifest) []string {
	p := m.Permissions
	lines := make([]string, 0, 6)
	if len(p.Network) > 0 {
		lines = append(lines, t(i18n.StorePermNetwork, strings.Join(p.Network, ", ")))
	} else {
		lines = append(lines, t(i18n.StorePermNoNetwork))
	}
	if len(p.Chains) > 0 {
		names := make([]string, 0, len(p.Chains))
		for _, id := range p.Chains {
			if n, ok := chains.ByID(id); ok {
				names = append(names, n.Name)
			} else {
				names = append(names, id)
			}
		}
		lines = append(lines, t(i18n.StorePermChains, strings.Join(names, ", ")))
	} else {
		lines = append(lines, t(i18n.StorePermNoChains))
	}
	if slices.Contains(p.Read, "balances") {
		lines = append(lines, t(i18n.StorePermRead))
	}
	if p.Sign {
		lines = append(lines, t(i18n.StorePermSign))
	}
	if p.StorageKb > 0 {
		lines = append(lines, t(i18n.StorePermStorage, p.StorageKb))
	}
	if len(m.Settings) > 0 {
		lines = append(lines, t(i18n.StorePermSettings, len(m.Settings)))
	}
	return lines
}
