package app

import (
	"fmt"
	"hash/fnv"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/passkey"
	"github.com/davirain-su/lintel/internal/version"
	"github.com/davirain-su/lintel/internal/wallet"
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
	collapsed := a.settings.SidebarCollapsed
	// Clear the window controls where they sit over the sidebar.
	top := float32(14)
	if bar := c.TitleBar(); bar.Height > 0 {
		top = bar.Height + 4
	}
	width, padX := float32(sidebarWidth), float32(14)
	if collapsed {
		width, padX = sidebarRailWidth, 10
	}
	side := ui.Column(c).Width(width).FillHeight()
	if !c.Vibrancy() {
		side.Background(p.sidebar)
	}
	side.Padding(top, padX, padX, 12).DragWindow().Children(func() {
		// The app: its mark, its name, and what it may do. Collapsed, the
		// mark itself is the way back.
		if collapsed {
			a.sidebarMark(c, true)
		} else {
			ui.Row(c).Gap(11).Padding(8, 8, 8, 6).AlignItems(ui.Center).Children(func() {
				a.sidebarMark(c, false)
				ui.Column(c).Gap(1).Grow(1).Children(func() {
					ui.Text(c, a.t(i18n.AppTitle)).FontSize(14).FontWeight(700).
						LetterSpacing(-0.2).MaxLines(1)
					ui.Text(c, a.t(i18n.WatchOnly)).FontSize(10.5).TextColor(p.muted).MaxLines(1)
				})
				a.sidebarToggle(c)
			})
		}
		selected := a.sidebarSelection()
		nav := ui.Sidebar(c.Key("nav"), &selected, func() {
			for _, item := range []struct {
				id   string
				text i18n.Key
				icon *ui.SVG
			}{
				{"portfolio", i18n.NavPortfolio, iconChart},
				{"wallet", i18n.NavWallet, iconWallet},
				{"plugins", i18n.NavApps, iconGrid},
				{"intents", i18n.NavIntents, iconSwap},
				{"apps", i18n.NavDapp, iconGlobe},
				{"settings", i18n.NavSettings, iconGear},
			} {
				label := a.t(item.text)
				visible := label
				if collapsed {
					visible = ""
				}
				ui.SidebarItem(c, item.id, item.icon, visible).
					Height(36).Label(label).Tooltip(label)
			}
		}).Grow(1).FillWidth().Background(ui.Transparent).Margin(0, -6)
		if nav.Changed() {
			a.selectSidebar(selected)
		}
		if collapsed {
			a.sidebarToggle(c)
		}
		a.sidebarFooter(c)
	})
}

// sidebarMark draws the app's mark. When it is the only thing left of the
// sidebar, clicking it brings the sidebar back.
func (a *App) sidebarMark(c *ui.Context, button bool) {
	p := a.pal
	mark := func() {
		ui.Box(c).Size(36, 36).Radius(11).
			Gradient(ui.Hex("#2c342b"), ui.Hex("#151713"), 135).Center().Children(func() {
			ui.Icon(c, iconPulse).Size(18, 18).TextColor(p.accent)
		})
	}
	if !button {
		mark()
		return
	}
	b := ui.Button(c, "").Size(44, 44).Padding(4).Radius(12).Center().
		Label(a.t(i18n.SidebarExpand)).Tooltip(a.t(i18n.SidebarExpand))
	b.Children(mark)
	if b.Clicked() {
		a.toggleSidebar()
	}
}

// sidebarToggle folds the sidebar into its icon rail and back.
func (a *App) sidebarToggle(c *ui.Context) {
	label := a.t(i18n.SidebarCollapse)
	if a.settings.SidebarCollapsed {
		label = a.t(i18n.SidebarExpand)
	}
	a.iconButton(c, iconSidebar, label, a.toggleSidebar)
}

// toggleSidebar folds or unfolds the sidebar and remembers the choice.
func (a *App) toggleSidebar() {
	a.settings.SidebarCollapsed = !a.settings.SidebarCollapsed
	a.save()
}

func (a *App) sidebarSelection() string {
	switch a.route.kind {
	case "":
		return "portfolio"
	case "plugin":
		return "plugins"
	default:
		return a.route.kind
	}
}

func (a *App) selectSidebar(id string) {
	switch id {
	case "portfolio":
		a.goTo(routePortfolio)
	case "wallet":
		a.goTo(routeWallet)
	case "plugins":
		a.openApps()
	case "intents":
		a.goTo(routeIntents)
	case "apps":
		a.goTo(routeApps)
	case "settings":
		a.goTo(routeSettings)
	}
}

// sidebarFooter ends the sidebar with Settings and how the last refresh
// went.
func (a *App) sidebarFooter(c *ui.Context) {
	p := a.pal
	text, dot := a.status()
	ui.Column(c).Gap(8).Children(func() {
		if a.settings.SidebarCollapsed {
			ui.Box(c).Size(40, 40).Radius(10).Background(p.inset).
				Border(1, p.hairline).Center().AlignSelf(ui.Center).
				Tooltip(a.network().Name + " · " + text).Children(func() {
				ui.Box(c).Size(7, 7).Radius(4).Background(dot)
			})
			return
		}
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
		if a.route.kind == "plugin" {
			a.backButton(c, a.t(i18n.AppAll), a.closePluginPage)
		}
		ui.Column(c).Grow(1).Gap(5).Children(func() {
			if subtitle != "" {
				ui.Text(c, strings.ToUpper(subtitle)).FontSize(10.5).FontWeight(700).
					TextColor(p.muted).LetterSpacing(0.8).MaxLines(1)
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(28).FontWeight(700).LetterSpacing(-0.6).MaxLines(1)
				if a.route.kind == "plugin" {
					a.trustBadge(c, a.pluginView.official, a.pluginView.unsigned)
				}
			})
		})
		if a.loading {
			ui.Spinner(c).Size(16, 16)
		}
		switch a.route.kind {
		case "":
			a.iconButton(c, iconRefresh, a.t(i18n.Refresh), a.requestRefresh)
		case "plugin":
			a.iconButton(c, iconGear, a.t(i18n.AppSettings), func() {
				a.openPluginDetail(a.route.id)
			})
			a.iconButton(c, iconRefresh, a.t(i18n.Refresh), a.requestRefresh)
		case "wallet":
			if a.walletUnlocked() > 0 {
				a.softButton(c, a.t(i18n.WalletLockAll), iconLock, p.muted, a.lockAll)
			}
			a.primaryButton(c, a.t(i18n.WalletNew), nil, false, a.createWalletAccount)
		case "plugins":
			if a.pluginTab == "store" {
				a.iconButton(c, iconRefresh, a.t(i18n.StoreRefresh), a.loadStore)
			}
		case "apps":
			if a.dappView != nil {
				a.iconButton(c, iconRefresh, a.t(i18n.Refresh), func() { a.dappView.Page().Reload() })
				a.iconButton(c, iconClose, a.t(i18n.DappClose), a.closeDapp)
			}
		}
	})
}

// iconButton 复用官方按钮的焦点、禁用、悬停和可访问性行为。
func (a *App) iconButton(c *ui.Context, icon *ui.SVG, label string, onClick func()) {
	ui.Button(c, "").Size(32, 32).Label(label).Tooltip(label).Children(func() {
		ui.Icon(c, icon).Size(16, 16)
	}).OnClick(onClick)
}

func (a *App) backButton(c *ui.Context, label string, onClick func()) {
	ui.Button(c, "").Height(30).Label(label).Children(func() {
		ui.Icon(c, iconChevron).Size(14, 14).Rotate(180)
		ui.Text(c, label).SingleLine()
	}).OnClick(onClick)
}

func (a *App) softButton(c *ui.Context, label string, icon *ui.SVG, color ui.Color, onClick func()) {
	ui.Button(c, "").Height(30).Label(label).TextColor(color).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, label).SingleLine()
	}).OnClick(onClick)
}

func (a *App) primaryButton(c *ui.Context, label string, icon *ui.SVG, disabled bool, onClick func()) {
	ui.PrimaryButton(c, "").Height(32).Label(label).Disabled(disabled).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, label).SingleLine()
	}).OnClick(onClick)
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
		return a.t(i18n.NavDapp), a.dappSubtitle()
	case "intents":
		return a.t(i18n.NavIntents), a.t(i18n.IntentsSubtitle)
	case "plugins":
		return a.t(i18n.NavApps), a.pluginSummary()
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
	total, known := a.totalNative()
	if known {
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
		if known {
			if fiat, ok := a.fiatText(total, network.Decimals, network.Symbol); ok {
				ui.Text(c, a.t(i18n.FiatApprox, fiat)).FontSize(12).TextColor(p.muted).MaxLines(1)
			}
		}
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
				if snap != nil {
					if fiat, ok := a.fiatText(snap.native, a.network().Decimals, a.network().Symbol); ok {
						ui.Text(c, fiat).FontSize(10).TextColor(p.faint).MaxLines(1)
					}
				}
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
				if fiat, ok := a.fiatText(tb.Amount, tb.Token.Decimals, tb.Token.Symbol); ok {
					ui.Text(c, fiat).FontSize(10).TextColor(p.faint).MaxLines(1)
				}
			})
		}
	})
}

// fiatText returns the approximate USD value of amount in symbol, when the
// last refresh priced it. A balance is always shown in its own asset first;
// this is the hint beside it, never the number the user acts on.
func (a *App) fiatText(amount *big.Int, decimals int, symbol string) (string, bool) {
	if a.settings.HideFiat || a.snap == nil || amount == nil || len(a.snap.usd) == 0 {
		return "", false
	}
	price, ok := a.snap.usd[symbol]
	if !ok || price <= 0 {
		return "", false
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	value := new(big.Float).SetInt(amount)
	value.Quo(value, new(big.Float).SetInt(scale))
	value.Mul(value, big.NewFloat(price))
	f, _ := value.Float64()
	return usdText(f), true
}

// usdText writes a dollar amount with a thousands separator, and enough
// decimals to keep a small value from reading as zero.
func usdText(v float64) string {
	if v > 0 && v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	s := fmt.Sprintf("%.2f", v)
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return "$" + groupDigits(s)
	}
	return "$" + groupDigits(s[:dot]) + s[dot:]
}

func groupDigits(s string) string {
	if len(s) <= 3 {
		return s
	}
	first := len(s) % 3
	if first == 0 {
		first = 3
	}
	var b strings.Builder
	b.WriteString(s[:first])
	for i := first; i < len(s); i += 3 {
		b.WriteByte(',')
		b.WriteString(s[i : i+3])
	}
	return b.String()
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
		a.fiatRow(c)
		a.settingsDivider(c)
		a.storeRow(c)
	})
	a.settingsGroup(c, a.t(i18n.AgentSection), func() {
		a.agentRow(c)
	})
	a.settingsGroup(c, "编码 Agent", func() { a.codingSettings(c) })
	a.settingsGroup(c, a.t(i18n.SecuritySection), func() {
		a.presenceRow(c)
		a.settingsDivider(c)
		a.passkeyRow(c)
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
	case chain.Near:
		return "NEAR"
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

// fiatRow turns the approximate USD values beside balances on or off.
func (a *App) fiatRow(c *ui.Context) {
	p := a.pal
	ui.Row(c).Padding(13, 16).AlignItems(ui.Center).Gap(14).Children(func() {
		ui.Box(c).Size(32, 32).Radius(9).Background(p.accentSoft).Center().Children(func() {
			ui.Icon(c, iconChart).Size(17, 17).TextColor(p.accent)
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, a.t(i18n.FiatTitle)).FontSize(12.5).FontWeight(600).MaxLines(1)
			ui.Text(c, a.t(i18n.FiatBody)).FontSize(11).TextColor(p.muted).MaxLines(3)
		})
		on := !a.settings.HideFiat
		if ui.Switch(c, &on).Changed() {
			a.settings.HideFiat = !on
			a.save()
			a.invalidate()
		}
	})
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
