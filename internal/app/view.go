package app

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/version"
	"github.com/davirain-su/crypto-panel/internal/wallet"
)

// view builds the window: the sidebar of pages beside the selected page. It
// runs on the main thread for every frame, so it only reads state; the
// network work happens elsewhere.
func (a *App) view(c *ui.Context) {
	t := c.Theme()
	a.pal = paletteFor(t)
	useAccent(t, a.pal)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		a.content(c)
	})
}

func (a *App) sidebar(c *ui.Context) {
	p := a.pal
	// Clear the window controls where they sit over the sidebar.
	top := float32(14)
	if bar := c.TitleBar(); bar.Height > 0 {
		top = bar.Height + 4
	}
	side := ui.Column(c).Width(sidebarWidth).FillHeight()
	if !c.Vibrancy() {
		side.Background(p.sidebar)
	}
	side.Padding(top, 12, 12, 12).Gap(2).DragWindow().Children(func() {
		// The app: its mark, its name, and what it may do.
		ui.Row(c).Gap(10).Padding(8, 8, 16, 8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(32, 32).Radius(9).Background(p.accentSoft).Center().Children(func() {
				ui.Icon(c, iconPulse).Size(18, 18).TextColor(p.accent)
			})
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, a.t(i18n.AppTitle)).FontSize(14).FontWeight(700).MaxLines(1)
				ui.Text(c, a.t(i18n.WatchOnly)).FontSize(10.5).TextColor(p.muted).MaxLines(1)
			})
		})
		ui.Scroll(c.Key("nav")).Grow(1).Margin(0, -6).Padding(0, 6).Children(func() {
			ui.Column(c).Gap(1).Children(func() {
				a.navItem(c, a.t(i18n.NavPortfolio), iconChart, a.route == routePortfolio, func() {
					a.goTo(routePortfolio)
				})
				a.navItem(c, a.t(i18n.NavWallet), iconWallet, a.route == routeWallet, func() {
					a.goTo(routeWallet)
				})
				a.navItem(c, a.t(i18n.NavPlugins), iconGrid, a.route.kind == "plugins", func() {
					a.goTo(routePlugins)
				})
				a.navItem(c, a.t(i18n.NavSettings), iconGear, a.route == routeSettings, func() {
					a.goTo(routeSettings)
				})
				a.pluginNav(c)
			})
		})
		a.statusRow(c)
	})
}

// navItem is one row of the sidebar: an icon and a label, marked under the
// pointer and filled while it is the page shown.
func (a *App) navItem(c *ui.Context, label string, icon *ui.SVG, selected bool, onClick func()) {
	p := a.pal
	b := ui.ButtonBase(c.Key("nav:" + label)).Height(34).PaddingX(10).Gap(10).Radius(8).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	bg := ui.Transparent
	switch {
	case selected:
		bg = p.press
	case b.Hovered():
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	color := p.text
	if selected {
		color = p.accent
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(17, 17).TextColor(color)
		ui.Text(c, label).FontSize(13.5).FontWeight(500).TextColor(p.text).MaxLines(1)
	})
	if b.Clicked() {
		onClick()
	}
}

// pluginNav lists the pages enabled plugins added, under the shell's own.
func (a *App) pluginNav(c *ui.Context) {
	p := a.pal
	first := true
	for _, item := range a.pluginItems {
		if item.Err != "" || !item.Enabled || len(item.Manifest.Navigation) == 0 {
			continue
		}
		if first {
			ui.Divider(c).Padding(10, 8)
			first = false
		}
		if len(item.Manifest.Navigation) > 1 {
			ui.Text(c, item.Manifest.Name).FontSize(11).FontWeight(600).TextColor(p.faint).
				LetterSpacing(0.4).Margin(10, 10, 4, 10).MaxLines(1)
		}
		for _, nav := range item.Manifest.Navigation {
			r := route{kind: "plugin", id: item.Manifest.ID, page: nav.Page}
			id, page := r.id, r.page
			a.navItem(c, nav.Title, iconSparkle, a.route == r, func() { a.openPluginPage(id, page) })
		}
	}
}

// statusRow ends the sidebar with how the last refresh went.
func (a *App) statusRow(c *ui.Context) {
	p := a.pal
	text, dot := a.status()
	ui.Row(c).Gap(7).Padding(10, 8, 4, 8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).Background(dot)
		ui.Text(c, text).FontSize(10.5).TextColor(p.muted).MaxLines(2).Grow(1)
	})
}

// content draws the selected page under the window's title bar.
func (a *App) content(c *ui.Context) {
	p := a.pal
	ui.Column(c).Grow(1).FillHeight().Background(p.bg).Children(func() {
		a.pageHeader(c)
		ui.Scroll(c.Key("page")).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 40, 48, 40).Children(func() {
				a.pageBody(c)
			})
		})
	})
}

func (a *App) pageHeader(c *ui.Context) {
	p := a.pal
	title, subtitle := a.pageTitle()
	ui.Row(c).Padding(26, 40, 10, 40).AlignItems(ui.End).Gap(12).DragWindow().Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			if subtitle != "" {
				ui.Text(c, strings.ToUpper(subtitle)).FontSize(11).FontWeight(600).
					TextColor(p.muted).LetterSpacing(0.6).MaxLines(1)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(30).FontWeight(700).LetterSpacing(-0.6).MaxLines(1)
				if a.route.kind == "plugin" && a.pluginView.unsigned {
					ui.Badge(c, a.t(i18n.PluginUnsigned))
				}
			})
		})
		if a.loading {
			ui.Spinner(c)
		}
		switch a.route.kind {
		case "", "plugin":
			a.iconButton(c, iconRefresh, a.t(i18n.Refresh), a.requestRefresh)
		case "wallet":
			if a.walletUnlocked() > 0 {
				a.softButton(c, a.t(i18n.WalletLockAll), nil, p.muted, a.lockAll)
			}
			ui.Button(c, a.t(i18n.WalletNew)).OnClick(a.createWalletAccount)
		case "plugins":
			if a.pluginTab == "store" {
				a.iconButton(c, iconRefresh, a.t(i18n.StoreRefresh), a.loadStore)
			} else {
				ui.Button(c, a.t(i18n.PluginInstall)).OnClick(a.installPlugin)
			}
		}
	})
}

// iconButton is a borderless button showing an icon, which a soft square
// marks under the pointer.
func (a *App) iconButton(c *ui.Context, icon *ui.SVG, label string, onClick func()) {
	p := a.pal
	b := ui.ButtonBase(c).Size(34, 34).Radius(8).Center().Label(label).FocusRing(false)
	bg := ui.Transparent
	if b.Hovered() {
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() { ui.Icon(c, icon).Size(17, 17).TextColor(p.muted) })
	if b.Clicked() {
		onClick()
	}
}

// softButton is a small button on the content's own background, for the
// actions inside a card.
func (a *App) softButton(c *ui.Context, label string, icon *ui.SVG, color ui.Color, onClick func()) {
	p := a.pal
	b := ui.ButtonBase(c).Height(28).PaddingX(10).Gap(6).Radius(7).
		AlignItems(ui.Center).FocusRing(false)
	bg := p.hover
	if b.Hovered() {
		bg = p.press
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14).TextColor(color)
		}
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(color)
	})
	if b.Clicked() {
		onClick()
	}
}

// sectionTitle heads a group of the page.
func (a *App) sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(19).FontWeight(700).LetterSpacing(-0.3).Margin(20, 0, 12, 0)
}

func (a *App) pageTitle() (title, subtitle string) {
	switch a.route.kind {
	case "wallet":
		return a.t(i18n.NavWallet), a.walletSummary()
	case "plugins":
		return a.t(i18n.NavPlugins), a.pluginSummary()
	case "settings":
		return a.t(i18n.NavSettings), ""
	case "plugin":
		name := a.pluginView.name
		if name == "" {
			name = a.route.id
		}
		// The page's name is only worth saying when the plugin has more
		// than one.
		return name, a.pluginPageSubtitle()
	default:
		return a.t(i18n.NavPortfolio), a.headline()
	}
}

// pluginPageSubtitle names the open page of a plugin, for the header, when
// the plugin has more than one page to tell apart.
func (a *App) pluginPageSubtitle() string {
	for _, item := range a.pluginItems {
		if item.Manifest.ID != a.route.id || len(item.Manifest.Navigation) < 2 {
			continue
		}
		for _, nav := range item.Manifest.Navigation {
			if nav.Page == a.route.page {
				return nav.Title
			}
		}
	}
	return ""
}

// pluginSummary counts the installed plugins, for the manager's header.
func (a *App) pluginSummary() string {
	enabled := 0
	for _, item := range a.pluginItems {
		if item.Err == "" && item.Enabled {
			enabled++
		}
	}
	return a.t(i18n.PluginCount, len(a.pluginItems), enabled)
}

func (a *App) pageBody(c *ui.Context) {
	switch a.route.kind {
	case "wallet":
		a.walletView(c)
	case "plugins":
		a.pluginsView(c)
	case "settings":
		a.settingsView(c)
	case "plugin":
		a.pluginPageView(c)
	default:
		a.portfolio(c)
	}
}

func (a *App) headline() string {
	n := len(a.settings.WatchedOf(a.network().Family))
	var count string
	switch n {
	case 0:
		count = a.t(i18n.NoAccounts)
	case 1:
		count = a.t(i18n.AccountOne)
	default:
		count = a.t(i18n.AccountMany, n)
	}
	return a.network().Name + " · " + count
}

func (a *App) portfolio(c *ui.Context) {
	accounts := a.settings.WatchedOf(a.network().Family)
	a.totalBlock(c)
	if len(accounts) == 0 {
		a.emptyPortfolio(c)
	} else {
		a.sectionTitle(c, a.t(i18n.AccountsTitle))
		ui.Column(c).Gap(10).Children(func() {
			for _, acc := range accounts {
				a.accountCard(c, acc)
			}
		})
	}
	a.addForm(c)
}

func (a *App) totalBlock(c *ui.Context) {
	p := a.pal
	network := a.network()
	value := "—"
	if total, ok := a.totalNative(); ok {
		value = chain.FormatAmount(total, network.Decimals, 4)
	}
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).AlignItems(ui.Baseline).Gap(8).Children(func() {
			ui.Text(c, value).FontSize(40).FontWeight(700).LetterSpacing(-1)
			ui.Text(c, network.Symbol).FontSize(20).FontWeight(600).TextColor(p.muted)
		})
		ui.Text(c, a.t(i18n.TotalIn, network.Name)).FontSize(12).TextColor(p.muted)
	})
}

func (a *App) emptyPortfolio(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(28).Center().Radius(12).
		Background(p.card).Border(1, p.hairline).Margin(20, 0, 0, 0).Children(func() {
		ui.Box(c).Size(44, 44).Radius(12).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconWallet).Size(22, 22).TextColor(p.accent)
		})
		ui.Text(c, a.t(i18n.NoAccounts)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.NoAccountsWhy)).FontSize(12.5).TextColor(p.muted).TextAlign(ui.Center)
	})
}

func (a *App) accountCard(c *ui.Context, acc wallet.Address) {
	p := a.pal
	snap := a.accountSnapshot(acc.Addr)
	ui.Column(c).Key(acc.Addr).Gap(10).Padding(16).Radius(12).
		Background(p.card).Border(1, p.hairline).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				name := acc.Label
				if name == "" {
					name = wallet.Short(acc.Addr)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(14).FontWeight(600).MaxLines(1)
					if a.settings.IsWallet(acc.Addr) {
						ui.Text(c, a.t(i18n.WalletBadge)).FontSize(10.5).FontWeight(600).
							TextColor(p.accent)
					}
				})
				ui.Text(c, a.displayAddress(acc.Addr)).FontSize(11).TextColor(p.muted).MaxLines(1)
			})
			ui.Text(c, a.nativeText(snap)).FontSize(15).FontWeight(700)
			// A wallet account is removed on its own page, with its key.
			if !a.settings.IsWallet(acc.Addr) {
				a.iconButton(c, iconTrash, a.t(i18n.Remove), func() {
					a.removeAccount(acc.Addr)
				})
			}
		})
		a.tokenRows(c, snap)
		if snap != nil && snap.err != "" {
			ui.Text(c, snap.err).FontSize(11).TextColor(p.danger).MaxLines(2)
		}
	})
}

// tokenRows lists the non-zero token balances of one account, under a
// divider.
func (a *App) tokenRows(c *ui.Context, snap *accountSnapshot) {
	if snap == nil {
		return
	}
	p := a.pal
	shown := 0
	for _, tb := range snap.tokens {
		if tb.Amount.Sign() != 0 {
			shown++
		}
	}
	if shown == 0 {
		return
	}
	ui.Divider(c)
	ui.Column(c).Gap(6).Children(func() {
		for _, tb := range snap.tokens {
			if tb.Amount.Sign() == 0 {
				continue
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, tb.Token.Symbol).FontSize(12).TextColor(p.muted).Grow(1)
				ui.Text(c, chain.FormatAmount(tb.Amount, tb.Token.Decimals, tb.Token.DisplayFrac())).FontSize(12)
			})
		}
	})
}

func (a *App) nativeText(snap *accountSnapshot) string {
	if snap == nil || snap.err != "" || snap.native == nil {
		return "—"
	}
	network := a.network()
	return chain.FormatAmount(snap.native, network.Decimals, 4) + " " + network.Symbol
}

// displayAddress returns addr the way a person reads it: EIP-55 for EVM,
// unchanged base58 for Solana.
func (a *App) displayAddress(addr string) string {
	kind, ok := wallet.KindOf(addr)
	if !ok {
		return addr
	}
	return wallet.Display(kind, addr)
}

func (a *App) addForm(c *ui.Context) {
	p := a.pal
	a.sectionTitle(c, a.t(i18n.AddAddress))
	ui.Column(c).Gap(10).Padding(16).Radius(12).MaxWidth(560).
		Background(p.card).Border(1, p.hairline).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("address"), &a.draftAddr).
				Placeholder(a.t(i18n.AddressPlaceholder)).
				Label(a.t(i18n.AddressLabel)).
				Grow(1).
				OnSubmit(a.addAccount)
			ui.Button(c, a.t(i18n.Add)).
				Disabled(strings.TrimSpace(a.draftAddr) == "").
				OnClick(a.addAccount)
		})
		ui.TextInput(c.Key("label"), &a.draftLabel).
			Placeholder(a.t(i18n.LabelPlaceholder)).
			Label(a.t(i18n.LabelLabel))
		if a.addErr != "" {
			ui.Text(c, a.addErr).FontSize(11).TextColor(p.danger)
		}
	})
}

func (a *App) settingsView(c *ui.Context) {
	p := a.pal
	a.sectionTitle(c, a.t(i18n.NetworkSection))
	ui.Column(c).Gap(14).MaxWidth(560).Children(func() {
		a.familyField(c)
		a.networkField(c)
		a.rpcField(c)
	})
	a.sectionTitle(c, a.t(i18n.AppSection))
	ui.Column(c).Gap(14).MaxWidth(560).Children(func() {
		a.refreshField(c)
		a.languageField(c)
		a.storeField(c)
	})
	a.sectionTitle(c, a.t(i18n.About))
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, a.t(i18n.AboutBody)).FontSize(12.5).TextColor(p.muted)
		ui.Text(c, a.t(i18n.Version, version.Version)).FontSize(11).TextColor(p.faint)
	})
}

func (a *App) field(c *ui.Context, label string, fn func()) {
	ui.Column(c).Gap(5).Children(func() {
		ui.Text(c, label).FontSize(11).FontWeight(600).TextColor(a.pal.muted).LetterSpacing(0.3)
		fn()
	})
}

// familyField picks the chain family; the network field then offers that
// family's presets. Switching family lands on its first network.
func (a *App) familyField(c *ui.Context) {
	families := chains.Families()
	labels := make([]string, len(families))
	idx := 0
	for i, f := range families {
		labels[i] = familyName(f)
		if f == a.network().Family {
			idx = i
		}
	}
	selected := labels[idx]
	a.field(c, a.t(i18n.FamilyLabel), func() {
		if ui.Select(c.Key("family"), &selected, labels).Changed() {
			for i, label := range labels {
				if label != selected {
					continue
				}
				if networks := chains.NetworksOf(families[i]); len(networks) > 0 {
					a.setNetwork(networks[0].ID)
				}
				break
			}
		}
	})
}

func (a *App) networkField(c *ui.Context) {
	networks := chains.NetworksOf(a.network().Family)
	names := make([]string, len(networks))
	idx := 0
	for i, n := range networks {
		names[i] = n.Name
		if n.ID == a.settings.NetworkID {
			idx = i
		}
	}
	selected := names[idx]
	a.field(c, a.t(i18n.ChainLabel), func() {
		if ui.Select(c.Key("network"), &selected, names).Changed() {
			for i, name := range names {
				if name == selected {
					a.setNetwork(networks[i].ID)
					break
				}
			}
		}
	})
}

// familyName is the family's name as people write it; both are proper
// nouns, so neither is translated.
func familyName(f chain.Family) string {
	switch f {
	case chain.EVM:
		return "EVM"
	case chain.Solana:
		return "Solana"
	}
	return string(f)
}

func (a *App) rpcField(c *ui.Context) {
	p := a.pal
	a.field(c, a.t(i18n.RPCLabel), func() {
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("rpc"), &a.rpcDraft).
				Placeholder(a.network().RPCURL).
				Label(a.t(i18n.RPCLabel)).
				Grow(1).
				OnSubmit(a.applyRPC)
			ui.Button(c, a.t(i18n.Apply)).
				Disabled(strings.TrimSpace(a.rpcDraft) == a.settings.RPCURL).
				OnClick(a.applyRPC)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, a.t(i18n.RPCHelp)).FontSize(11).TextColor(p.muted).Grow(1).MaxLines(2)
			a.softButton(c, a.t(i18n.Reset), nil, p.muted, a.resetRPC)
		})
		if a.settingsErr != "" {
			ui.Text(c, a.settingsErr).FontSize(11).TextColor(p.danger)
		}
	})
}

func (a *App) refreshField(c *ui.Context) {
	values := []int{30, 60, 300}
	if !slices.Contains(values, a.settings.RefreshSeconds) {
		values = append(values, a.settings.RefreshSeconds)
		sort.Ints(values)
	}
	labels := make([]string, len(values))
	idx := 0
	for i, v := range values {
		if v < 60 {
			labels[i] = a.t(i18n.Seconds, v)
		} else {
			labels[i] = a.t(i18n.Minutes, v/60)
		}
		if v == a.settings.RefreshSeconds {
			idx = i
		}
	}
	selected := labels[idx]
	a.field(c, a.t(i18n.RefreshInterval), func() {
		if ui.Select(c.Key("refresh"), &selected, labels).Changed() {
			for i, label := range labels {
				if label == selected {
					a.setRefreshSeconds(values[i])
					break
				}
			}
		}
	})
}

// storeField sets the registry the plugin store reads. Empty means the
// catalog the app ships with.
func (a *App) storeField(c *ui.Context) {
	p := a.pal
	a.field(c, a.t(i18n.StoreRegistry), func() {
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("store-url"), &a.storeURLDraft).
				Placeholder(a.t(i18n.StoreFeatured)).
				Label(a.t(i18n.StoreRegistry)).
				Grow(1).
				OnSubmit(a.applyStoreURL)
			ui.Button(c, a.t(i18n.Apply)).
				Disabled(strings.TrimSpace(a.storeURLDraft) == a.settings.PluginStoreURL).
				OnClick(a.applyStoreURL)
		})
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, a.t(i18n.StoreSettingsHelp)).FontSize(11).TextColor(p.muted).
				Grow(1).MaxLines(2)
			a.softButton(c, a.t(i18n.Reset), nil, p.muted, a.resetStoreURL)
		})
		if a.storeFormErr != "" {
			ui.Text(c, a.storeFormErr).FontSize(11).TextColor(p.danger)
		}
	})
}

func (a *App) languageField(c *ui.Context) {
	tags := i18n.Languages()
	labels := make([]string, len(tags))
	idx := 0
	for i, tag := range tags {
		switch tag {
		case i18n.System:
			labels[i] = a.t(i18n.SystemLanguage)
		case i18n.Chinese:
			labels[i] = "简体中文"
		default:
			labels[i] = "English"
		}
		if tag == a.settings.Language {
			idx = i
		}
	}
	selected := labels[idx]
	a.field(c, a.t(i18n.Language), func() {
		if ui.Select(c.Key("language"), &selected, labels).Changed() {
			for i, label := range labels {
				if label == selected {
					a.setLanguage(tags[i])
					break
				}
			}
		}
	})
}

// status says how the last refresh went, and the dot that goes with it.
func (a *App) status() (string, ui.Color) {
	p := a.pal
	switch {
	case a.loading:
		return a.t(i18n.Refreshing), p.faint
	case a.lastErr != "":
		return a.lastErr, p.danger
	case a.lastUpdated.IsZero():
		return a.t(i18n.NeverUpdated), p.faint
	default:
		s := a.t(i18n.UpdatedAt, a.lastUpdated.Format("15:04"))
		if a.snap != nil && a.snap.block > 0 {
			s += " · " + a.t(i18n.Block, a.snap.block)
		}
		return s, p.success
	}
}
