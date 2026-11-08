package app

import (
	"hash/fnv"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/passkey"
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
	side.Padding(top, 14, 12, 12).DragWindow().Children(func() {
		// The app: its mark, its name, and what it may do.
		ui.Row(c).Gap(11).Padding(8, 8, 16, 6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Radius(11).
				Gradient(ui.Hex("#2c342b"), ui.Hex("#151713"), 135).Center().Children(func() {
				ui.Icon(c, iconPulse).Size(18, 18).TextColor(p.accent)
			})
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, a.t(i18n.AppTitle)).FontSize(14).FontWeight(700).
					LetterSpacing(-0.2).MaxLines(1)
				ui.Text(c, a.t(i18n.WatchOnly)).FontSize(10.5).TextColor(p.muted).MaxLines(1)
			})
		})
		ui.Scroll(c.Key("nav")).Grow(1).Margin(0, -6).Padding(0, 6).Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				a.navItem(c, a.t(i18n.NavPortfolio), iconChart, a.route == routePortfolio, func() {
					a.goTo(routePortfolio)
				})
				a.navItem(c, a.t(i18n.NavWallet), iconWallet, a.route == routeWallet, func() {
					a.goTo(routeWallet)
				})
				a.navItem(c, a.t(i18n.NavApps), iconGlobe, a.route.kind == "apps", func() {
					a.goTo(routeApps)
				})
				a.navItem(c, a.t(i18n.NavIntents), iconSwap, a.route == routeIntents, func() {
					a.goTo(routeIntents)
				})
				a.navItem(c, a.t(i18n.NavPlugins), iconGrid, a.route.kind == "plugins", func() {
					a.goTo(routePlugins)
				})
				a.pluginNav(c)
			})
		})
		a.sidebarFooter(c)
	})
}

// navItem is one row of the sidebar: an icon and a label, marked under the
// pointer and filled with the accent while it is the page shown.
func (a *App) navItem(c *ui.Context, label string, icon *ui.SVG, selected bool, onClick func()) {
	p := a.pal
	b := ui.ButtonBase(c.Key("nav:" + label)).Height(36).PaddingX(11).Gap(10).Radius(9).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	bg := ui.Transparent
	switch {
	case selected:
		bg = p.accentSoft
	case b.Hovered():
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	color, iconColor := p.text, p.muted
	if selected {
		color, iconColor = p.text, p.accent
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(17, 17).TextColor(iconColor)
		ui.Text(c, label).FontSize(13).FontWeight(500).TextColor(color).MaxLines(1)
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
			ui.Text(c, strings.ToUpper(a.t(i18n.NavPlugins))).FontSize(9.5).FontWeight(700).
				TextColor(p.faint).LetterSpacing(0.9).Margin(16, 10, 5, 11).MaxLines(1)
			first = false
		}
		if len(item.Manifest.Navigation) > 1 {
			ui.Text(c, item.Manifest.Name).FontSize(10.5).FontWeight(600).TextColor(p.faint).
				LetterSpacing(0.3).Margin(11, 10, 3, 11).MaxLines(1)
		}
		for _, nav := range item.Manifest.Navigation {
			r := route{kind: "plugin", id: item.Manifest.ID, page: nav.Page}
			id, page := r.id, r.page
			a.navItem(c, nav.Title, iconSparkle, a.route == r, func() { a.openPluginPage(id, page) })
		}
	}
}

// sidebarFooter ends the sidebar with Settings and how the last refresh
// went.
func (a *App) sidebarFooter(c *ui.Context) {
	p := a.pal
	text, dot := a.status()
	ui.Column(c).Gap(8).Children(func() {
		a.navItem(c, a.t(i18n.NavSettings), iconGear, a.route == routeSettings, func() {
			a.goTo(routeSettings)
		})
		ui.Column(c).Gap(5).Padding(10).Radius(10).Background(p.inset).
			Border(1, p.hairline).Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(dot)
				ui.Text(c, a.network().Name).FontSize(11).FontWeight(600).Grow(1).MaxLines(1)
			})
			ui.Text(c, text).FontSize(10).TextColor(p.muted).MaxLines(2)
		})
	})
}

// content draws the selected page under the window's title bar.
func (a *App) content(c *ui.Context) {
	p := a.pal
	ui.Column(c).Grow(1).FillHeight().Background(p.bg).Children(func() {
		a.pageHeader(c)
		if a.route.kind == "apps" {
			// The DApp page fills its height: a web view does not scroll
			// with the shell's page.
			a.dappBody(c)
			return
		}
		ui.Scroll(c.Key("page")).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 44, 56, 44).MaxWidth(920).Children(func() {
				a.pageBody(c)
			})
		})
	})
}

func (a *App) pageHeader(c *ui.Context) {
	p := a.pal
	title, subtitle := a.pageTitle()
	ui.Row(c).Padding(28, 44, 18, 44).AlignItems(ui.End).Gap(12).DragWindow().Children(func() {
		ui.Column(c).Grow(1).Gap(5).Children(func() {
			if subtitle != "" {
				ui.Text(c, strings.ToUpper(subtitle)).FontSize(10.5).FontWeight(700).
					TextColor(p.muted).LetterSpacing(0.8).MaxLines(1)
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(28).FontWeight(700).LetterSpacing(-0.6).MaxLines(1)
				if a.route.kind == "plugin" && a.pluginView.unsigned {
					ui.Badge(c, a.t(i18n.PluginUnsigned))
				}
			})
		})
		if a.loading {
			ui.Spinner(c).Size(16, 16)
		}
		switch a.route.kind {
		case "", "plugin":
			a.iconButton(c, iconRefresh, a.t(i18n.Refresh), a.requestRefresh)
		case "wallet":
			if a.walletUnlocked() > 0 {
				a.softButton(c, a.t(i18n.WalletLockAll), iconLock, p.muted, a.lockAll)
			}
			if a.network().Family == chain.Near {
				a.primaryButton(c, a.t(i18n.WalletNearNew), nil, false, a.createNearAccount)
			} else {
				a.primaryButton(c, a.t(i18n.WalletNew), nil, false, a.createWalletAccount)
			}
		case "plugins":
			if a.pluginTab == "store" {
				a.iconButton(c, iconRefresh, a.t(i18n.StoreRefresh), a.loadStore)
			} else {
				a.primaryButton(c, a.t(i18n.PluginInstall), nil, false, a.installPlugin)
			}
		case "apps":
			if a.dappView != nil {
				a.iconButton(c, iconRefresh, a.t(i18n.Refresh), func() { a.dappView.Page().Reload() })
				a.iconButton(c, iconClose, a.t(i18n.DappClose), a.closeDapp)
			}
		}
	})
}

// iconButton is a borderless button showing an icon, which a soft square
// marks under the pointer.
func (a *App) iconButton(c *ui.Context, icon *ui.SVG, label string, onClick func()) {
	p := a.pal
	b := ui.ButtonBase(c).Size(32, 32).Radius(8).Center().Label(label).FocusRing(false)
	bg := ui.Transparent
	if b.Hovered() {
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() { ui.Icon(c, icon).Size(16, 16).TextColor(p.muted) })
	if b.Clicked() {
		onClick()
	}
}

// softButton is a small button on the content's own background, for the
// actions inside a card.
func (a *App) softButton(c *ui.Context, label string, icon *ui.SVG, color ui.Color, onClick func()) {
	p := a.pal
	b := ui.ButtonBase(c).Height(30).PaddingX(11).Gap(6).Radius(8).
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

// primaryButton is the page's main action: filled with the accent.
func (a *App) primaryButton(c *ui.Context, label string, icon *ui.SVG, disabled bool, onClick func()) {
	p := a.pal
	b := ui.ButtonBase(c).Height(32).PaddingX(13).Gap(7).Radius(8).
		AlignItems(ui.Center).FocusRing(false)
	bg, fg := p.accent, p.onAccent
	if disabled {
		bg, fg = p.inset, p.faint
	}
	b.Background(bg)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14).TextColor(fg)
		}
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(fg)
	})
	if !disabled && b.Clicked() {
		onClick()
	}
}

// sectionTitle heads a group of the page.
func (a *App) sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(15).FontWeight(700).LetterSpacing(-0.2).
		Margin(28, 0, 12, 0)
}

func (a *App) pageTitle() (title, subtitle string) {
	switch a.route.kind {
	case "wallet":
		return a.t(i18n.NavWallet), a.walletSummary()
	case "apps":
		return a.t(i18n.NavApps), a.dappSubtitle()
	case "intents":
		return a.t(i18n.NavIntents), a.t(i18n.IntentsSubtitle)
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
	case "apps":
		a.dappBody(c)
	case "intents":
		a.intentsBody(c)
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
	a.heroCard(c)
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

// heroCard is the page's headline: the total, on a card that carries the
// accent.
func (a *App) heroCard(c *ui.Context) {
	p := a.pal
	network := a.network()
	value := "—"
	if total, ok := a.totalNative(); ok {
		value = chain.FormatAmount(total, network.Decimals, 4)
	}
	ui.Column(c).Gap(10).Padding(22).Radius(16).Border(1, p.hairline).
		Gradient(p.accentSoft, p.card, 115).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, strings.ToUpper(a.t(i18n.TotalIn, network.Name))).FontSize(10.5).
				FontWeight(700).TextColor(p.muted).LetterSpacing(0.8).Grow(1).MaxLines(1)
			ui.Row(c).Gap(6).Padding(5, 9).Radius(99).Background(p.inset).
				AlignItems(ui.Center).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(p.success)
				ui.Text(c, network.Symbol).FontSize(10.5).FontWeight(600).
					TextColor(p.muted).MaxLines(1)
			})
		})
		ui.Row(c).AlignItems(ui.Baseline).Gap(9).Children(func() {
			ui.Text(c, value).FontSize(44).FontWeight(700).LetterSpacing(-1.2)
			ui.Text(c, network.Symbol).FontSize(18).FontWeight(600).TextColor(p.muted)
		})
		ui.Text(c, a.headline()).FontSize(11.5).TextColor(p.muted).MaxLines(1)
	})
}

func (a *App) emptyPortfolio(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(30).Center().Radius(14).
		Background(p.card).Border(1, p.hairline).Margin(24, 0, 0, 0).Children(func() {
		ui.Box(c).Size(46, 46).Radius(13).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconWallet).Size(22, 22).TextColor(p.accent)
		})
		ui.Text(c, a.t(i18n.NoAccounts)).FontSize(15).FontWeight(700)
		ui.Text(c, a.t(i18n.NoAccountsWhy)).FontSize(12.5).TextColor(p.muted).TextAlign(ui.Center)
	})
}

func (a *App) accountCard(c *ui.Context, acc wallet.Address) {
	p := a.pal
	snap := a.accountSnapshot(acc.Addr)
	ui.Column(c).Key(acc.Addr).Gap(12).Padding(16).Radius(14).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			a.identicon(c, acc.Addr)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				name := acc.Label
				if name == "" {
					name = wallet.Short(acc.Addr)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, name).FontSize(13.5).FontWeight(600).MaxLines(1)
					if a.settings.IsWalletAccount(acc.Addr) {
						ui.Text(c, a.t(i18n.WalletBadge)).FontSize(9.5).FontWeight(700).
							TextColor(p.accent).Padding(3, 6).Radius(99).Background(p.accentSoft)
					}
				})
				ui.Text(c, a.displayAddress(acc.Addr)).Font("monospace").FontSize(10.5).
					TextColor(p.muted).MaxLines(1)
			})
			ui.Column(c).AlignItems(ui.End).Gap(1).Children(func() {
				ui.Text(c, a.nativeText(snap)).FontSize(15).FontWeight(700)
				ui.Text(c, a.network().Symbol).FontSize(10).TextColor(p.faint)
			})
			// A wallet account is removed on its own page, with its key.
			if !a.settings.IsWalletAccount(acc.Addr) {
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

// identicon draws a small deterministic mark for an address: a three by
// three grid of squares from its hash.
func (a *App) identicon(c *ui.Context, addr string) {
	p := a.pal
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(addr)))
	bits := h.Sum32()
	ui.Box(c).Size(34, 34).Radius(10).Background(p.inset).Center().Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			for row := 0; row < 3; row++ {
				ui.Row(c).Gap(2).Children(func() {
					for col := 0; col < 3; col++ {
						fill := p.inset
						if bits&(1<<uint(row*3+col)) != 0 {
							fill = p.accent
						}
						ui.Box(c).Size(5, 5).Radius(1.5).Background(fill)
					}
				})
			}
		})
	})
}

// tokenRows lists the non-zero token balances of one account, as chips
// under a divider.
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
	ui.Row(c).Gap(6).Wrap().Children(func() {
		for _, tb := range snap.tokens {
			if tb.Amount.Sign() == 0 {
				continue
			}
			ui.Row(c).Gap(6).Padding(5, 8).Radius(7).Background(p.inset).
				AlignItems(ui.Center).Children(func() {
				ui.Text(c, tb.Token.Symbol).FontSize(10.5).FontWeight(600).
					TextColor(p.muted).MaxLines(1)
				ui.Text(c, chain.FormatAmount(tb.Amount, tb.Token.Decimals, tb.Token.DisplayFrac())).
					FontSize(10.5).FontWeight(600).MaxLines(1)
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
	ui.Column(c).Gap(10).Padding(16).Radius(14).MaxWidth(600).
		Background(p.card).Border(1, p.hairline).Shadow(0, 1, 2, 0, p.shadow).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("address"), &a.draftAddr).
				Placeholder(a.t(i18n.AddressPlaceholder)).
				Label(a.t(i18n.AddressLabel)).
				Grow(1).
				OnSubmit(a.addAccount)
			a.primaryButton(c, a.t(i18n.Add), nil, strings.TrimSpace(a.draftAddr) == "", a.addAccount)
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
	a.settingsGroup(c, a.t(i18n.NetworkSection), func() {
		a.settingsRow(c, a.t(i18n.FamilyLabel), func() { a.familySelect(c) })
		a.settingsDivider(c)
		a.settingsRow(c, a.t(i18n.ChainLabel), func() { a.networkSelect(c) })
		a.settingsDivider(c)
		a.rpcRow(c)
	})
	a.settingsGroup(c, a.t(i18n.AppSection), func() {
		a.settingsRow(c, a.t(i18n.RefreshInterval), func() { a.refreshSelect(c) })
		a.settingsDivider(c)
		a.settingsRow(c, a.t(i18n.Language), func() { a.languageSelect(c) })
		a.settingsDivider(c)
		a.storeRow(c)
	})
	a.settingsGroup(c, a.t(i18n.SecuritySection), func() {
		a.presenceRow(c)
	})
	a.settingsGroup(c, a.t(i18n.About), func() {
		ui.Row(c).Padding(14, 16).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, a.t(i18n.AboutBody)).FontSize(12).TextColor(p.muted).
				Grow(1).MaxLines(2)
			ui.Text(c, a.t(i18n.Version, version.Version)).FontSize(10.5).
				TextColor(p.faint).MaxLines(1)
		})
	})
}

// settingsGroup is one titled card of the settings page.
func (a *App) settingsGroup(c *ui.Context, title string, body func()) {
	p := a.pal
	ui.Text(c, strings.ToUpper(title)).FontSize(10.5).FontWeight(700).
		TextColor(p.faint).LetterSpacing(0.8).Margin(28, 2, 10, 2)
	ui.Column(c).Radius(14).Background(p.card).Border(1, p.hairline).
		Shadow(0, 1, 2, 0, p.shadow).Children(body)
}

func (a *App) settingsRow(c *ui.Context, label string, control func()) {
	ui.Row(c).Padding(13, 16).AlignItems(ui.Center).Gap(16).Children(func() {
		ui.Text(c, label).FontSize(12.5).FontWeight(500).Grow(1).MaxLines(1)
		control()
	})
}

func (a *App) settingsDivider(c *ui.Context) {
	ui.Divider(c).Margin(0, 16)
}

// familySelect picks the chain family; the network field then offers that
// family's presets. Switching family lands on its first network.
func (a *App) familySelect(c *ui.Context) {
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
	if ui.Select(c.Key("family"), &selected, labels).Width(190).Changed() {
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
}

func (a *App) networkSelect(c *ui.Context) {
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
	if ui.Select(c.Key("network"), &selected, names).Width(190).Changed() {
		for i, name := range names {
			if name == selected {
				a.setNetwork(networks[i].ID)
				break
			}
		}
	}
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

func (a *App) rpcRow(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(13, 16).Children(func() {
		ui.Text(c, a.t(i18n.RPCLabel)).FontSize(12.5).FontWeight(500)
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("rpc"), &a.rpcDraft).
				Placeholder(a.network().RPCURL).
				Label(a.t(i18n.RPCLabel)).
				Grow(1).
				OnSubmit(a.applyRPC)
			a.primaryButton(c, a.t(i18n.Apply), nil,
				strings.TrimSpace(a.rpcDraft) == a.settings.RPCURL, a.applyRPC)
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

func (a *App) refreshSelect(c *ui.Context) {
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
	if ui.Select(c.Key("refresh"), &selected, labels).Width(190).Changed() {
		for i, label := range labels {
			if label == selected {
				a.setRefreshSeconds(values[i])
				break
			}
		}
	}
}

// storeRow sets the registry the plugin store reads. Empty means no store.
func (a *App) storeRow(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(8).Padding(13, 16).Children(func() {
		ui.Text(c, a.t(i18n.StoreRegistry)).FontSize(12.5).FontWeight(500)
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("store-url"), &a.storeURLDraft).
				Placeholder("https://example.com/registry.json").
				Label(a.t(i18n.StoreRegistry)).
				Grow(1).
				OnSubmit(a.applyStoreURL)
			a.primaryButton(c, a.t(i18n.Apply), nil,
				strings.TrimSpace(a.storeURLDraft) == a.settings.PluginStoreURL, a.applyStoreURL)
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

func (a *App) languageSelect(c *ui.Context) {
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
	if ui.Select(c.Key("language"), &selected, labels).Width(190).Changed() {
		for i, label := range labels {
			if label == selected {
				a.setLanguage(tags[i])
				break
			}
		}
	}
}

// presenceRow turns the system's confirmation on or off, and says what it
// guards.
func (a *App) presenceRow(c *ui.Context) {
	p := a.pal
	ui.Row(c).Padding(13, 16).AlignItems(ui.Center).Gap(14).Children(func() {
		ui.Box(c).Size(32, 32).Radius(9).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconFingerprint).Size(17, 17).TextColor(p.accent)
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, a.t(i18n.AuthTitle, a.authName())).FontSize(12.5).FontWeight(600).MaxLines(1)
			body := a.t(i18n.AuthBody, a.authName())
			if a.passkeyKind == passkey.KindNone {
				body = a.t(i18n.AuthUnavailable)
			}
			ui.Text(c, body).FontSize(11).TextColor(p.muted).MaxLines(3)
		})
		if a.passkeyKind != passkey.KindNone {
			on := !a.settings.DisablePresence
			if ui.Switch(c, &on).Changed() {
				a.setPresenceRequired(on)
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
