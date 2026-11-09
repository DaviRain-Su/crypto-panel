package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/agent"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/plugin"
)

// agentTokenKey is where the local API's bearer token lives in the keychain.
const agentTokenKey = "token"

// The app is the API's data source.
var _ agent.Data = (*App)(nil)

// startAgent 在主线程按设置启动、重启或关闭本机 API。
func (a *App) startAgent() {
	a.stopAgent()
	a.agentErr = ""
	if !a.settings.AgentEnabled || a.agentSecrets == nil {
		return
	}
	token, err := a.agentTokenValue()
	if err != nil {
		a.agentErr = err.Error()
		return
	}
	srv := agent.New(a, token)
	if err := srv.Start(a.settings.AgentPort); err != nil {
		a.agentErr = err.Error()
		return
	}
	a.agentSrv = srv
	a.agentAddr = srv.Addr()
	a.agentToken = token
}

// stopAgent stops the local API, if it runs.
func (a *App) stopAgent() {
	if a.pluginManager != nil {
		a.pluginManager.CancelVia("local-api")
	}
	if a.agentSrv != nil {
		_ = a.agentSrv.Close()
		a.agentSrv = nil
	}
	a.agentAddr = ""
	a.agentToken = ""
}

// agentTokenValue reads the token from the keychain, making one the first
// time the API is turned on.
func (a *App) agentTokenValue() (string, error) {
	if data, err := a.agentSecrets.Get(agentTokenKey); err == nil && len(data) > 0 {
		return string(data), nil
	}
	token, err := agent.NewToken()
	if err != nil {
		return "", err
	}
	if err := a.agentSecrets.Set(agentTokenKey, []byte(token)); err != nil {
		return "", err
	}
	return token, nil
}

func (a *App) setAgentEnabled(on bool) {
	if a.settings.AgentEnabled == on {
		return
	}
	a.settings.AgentEnabled = on
	a.settings = a.settings.Validate()
	a.save()
	a.startAgent()
	a.invalidate()
}

// applyAgentPort saves the port the API listens on and restarts it.
func (a *App) applyAgentPort() {
	raw := strings.TrimSpace(a.agentPortDraft)
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		a.agentErr = a.t(i18n.AgentPortInvalid)
		a.invalidate()
		return
	}
	if port == a.settings.AgentPort {
		return
	}
	a.settings.AgentPort = port
	a.settings = a.settings.Validate()
	a.agentPortDraft = strconv.Itoa(a.settings.AgentPort)
	a.save()
	a.startAgent()
	a.invalidate()
}

// rotateAgentToken replaces the bearer token. Every client of the old token
// is refused from the next request on.
func (a *App) rotateAgentToken() {
	if a.agentSecrets == nil {
		return
	}
	token, err := agent.NewToken()
	if err != nil {
		a.agentErr = err.Error()
		a.invalidate()
		return
	}
	if err := a.agentSecrets.Set(agentTokenKey, []byte(token)); err != nil {
		a.agentErr = err.Error()
		a.invalidate()
		return
	}
	a.startAgent()
	a.invalidate()
}

func (a *App) copyAgentToken() {
	if a.agentToken == "" {
		return
	}
	_ = mygo.Clipboard.Write(transfer.TextData(a.agentToken))
}

// agentRow is the Settings section for the local API: the switch, the
// address, the port, the token and the one request that proves it works.
func (a *App) agentRow(c *ui.Context) {
	p := a.pal
	ui.Column(c).Gap(10).Padding(13, 16).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(14).Children(func() {
			ui.Box(c).Size(32, 32).Radius(9).Background(p.accentSoft).Center().Children(func() {
				ui.Icon(c, iconTerminal).Size(17, 17).TextColor(p.accent)
			})
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, a.t(i18n.AgentTitle)).FontSize(12.5).FontWeight(600).MaxLines(1)
				ui.Text(c, a.t(i18n.AgentBody)).FontSize(11).TextColor(p.muted).MaxLines(3)
			})
			on := a.settings.AgentEnabled
			if ui.Switch(c, &on).Changed() {
				a.setAgentEnabled(on)
			}
		})
		if !a.settings.AgentEnabled {
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
			ui.TextInput(c.Key("agent-port"), &a.agentPortDraft).
				Label(a.t(i18n.AgentPort)).
				Width(110).
				OnSubmit(a.applyAgentPort)
			a.primaryButton(c, a.t(i18n.Apply), nil,
				strings.TrimSpace(a.agentPortDraft) == strconv.Itoa(a.settings.AgentPort),
				a.applyAgentPort)
		})
		if a.agentErr != "" {
			ui.Text(c, a.agentErr).FontSize(11).TextColor(p.danger).MaxLines(3)
		} else if a.agentAddr != "" {
			ui.Text(c, a.t(i18n.AgentListening, a.agentAddr)).FontSize(11).
				TextColor(p.muted).MaxLines(1)
		}
		ui.Text(c, a.t(i18n.AgentToken)).FontSize(11).FontWeight(600).TextColor(p.muted)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.agentToken).Font("monospace").FontSize(10.5).
				TextColor(p.text).Grow(1).MaxLines(1)
			a.softButton(c, a.t(i18n.WalletCopy), nil, p.muted, a.copyAgentToken)
			a.softButton(c, a.t(i18n.AgentRotate), nil, p.muted, a.rotateAgentToken)
		})
		ui.Text(c, a.t(i18n.AgentCurl, a.agentAddr)).Font("monospace").FontSize(10).
			TextColor(p.faint).MaxLines(2)
	})
}

// Portfolio implements agent.Data: the last refresh, in the API's shape.
func (a *App) Portfolio() agent.Portfolio {
	var out agent.Portfolio
	a.runOnMain(func() { out = a.agentPortfolio() })
	return out
}

func (a *App) agentPortfolio() agent.Portfolio {
	network := a.network()
	out := agent.Portfolio{
		Network: network.Name,
		ChainID: network.ID,
		Updated: a.lastUpdated,
		Error:   a.lastErr,
	}
	if a.snap == nil {
		return out
	}
	out.Block = a.snap.block
	labels := make(map[string]string)
	for _, acc := range a.settings.WatchedOf(network.Family) {
		labels[acc.Addr] = acc.Label
	}
	for _, acc := range a.snap.accounts {
		native := agent.Amount{Symbol: network.Symbol, Decimals: network.Decimals, Amount: "0"}
		if acc.native != nil {
			native.Amount = acc.native.String()
		}
		item := agent.Account{
			Address: acc.addr,
			Label:   labels[acc.addr],
			Native:  native,
			Error:   acc.err,
		}
		for _, tb := range acc.tokens {
			item.Tokens = append(item.Tokens, agent.Amount{
				Symbol:   tb.Token.Symbol,
				Decimals: tb.Token.Decimals,
				Amount:   tb.Amount.String(),
				Contract: tb.Token.Address,
			})
		}
		out.Accounts = append(out.Accounts, item)
	}
	return out
}

// Plugins implements agent.Data: the installed apps and their permissions.
func (a *App) Plugins() []agent.Plugin {
	var out []agent.Plugin
	a.runOnMain(func() { out = a.agentPlugins() })
	return out
}

func (a *App) agentPlugins() []agent.Plugin {
	out := make([]agent.Plugin, 0, len(a.pluginItems))
	for _, item := range a.pluginItems {
		if item.Err != "" {
			out = append(out, agent.Plugin{
				Folder:  filepath.Base(item.Dir),
				Broken:  item.Err,
				Enabled: item.Enabled,
			})
			continue
		}
		m := item.Manifest
		p := agent.Plugin{
			ID:          m.ID,
			Name:        m.Name,
			Version:     m.Version,
			Description: m.Description,
			Author:      m.Author,
			Enabled:     item.Enabled,
			Signed:      item.Signed,
			Official:    item.Official,
			Permissions: agent.Permissions{
				Network:   m.Permissions.Network,
				Chains:    m.Permissions.Chains,
				Read:      m.Permissions.Read,
				Sign:      m.Permissions.Sign,
				StorageKb: m.Permissions.StorageKb,
			},
		}
		p.Source = "unsigned"
		if item.Official {
			p.Source = "official"
		} else if item.Signed {
			p.Source = "signed"
		}
		p.InstalledVia = item.InstalledVia
		if p.InstalledVia == "" {
			p.InstalledVia = "unknown"
		}
		p.PackageHash, _ = a.plugins.PackageHash(m.ID)
		p.Settings, _ = a.plugins.DescribeSettings(m.ID)
		if a.pluginManager != nil {
			for _, job := range a.pluginManager.Installations() {
				if job.PluginID == m.ID {
					p.PendingInstallationID = job.RequestID
					break
				}
			}
		}
		for _, nav := range m.Navigation {
			p.Pages = append(p.Pages, nav.Page)
		}
		out = append(out, p)
	}
	return out
}

// Refresh implements agent.Data: the app's first page, drawn for the API.
// The runtime is fresh and its host refuses signing, so a page cannot open
// an approval window from an HTTP request.
func (a *App) Refresh(id string) (any, error) {
	result, err := a.RefreshPage(id, "")
	return result.Page, err
}

// RefreshPage 的 lease 包含模块构造和执行；每次建立禁用签名的新 runtime。
func (a *App) RefreshPage(id, page string) (agent.PageResult, error) {
	var result agent.PageResult
	if a.pluginManager == nil {
		return result, &plugin.Error{Code: "MANAGEMENT_UNAVAILABLE", Message: "应用管理不可用", Status: 503}
	}
	release, err := a.pluginManager.TryExecution(id)
	if err != nil {
		return result, err
	}
	defer release()
	inst, err := a.plugins.Get(id)
	if errors.Is(err, plugin.ErrNotInstalled) {
		return result, agent.ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if inst.Err != "" {
		return result, errors.New("应用完整性检查失败")
	}
	if !inst.Enabled {
		return result, agent.ErrDisabled
	}
	if len(inst.Manifest.Navigation) == 0 {
		return result, agent.ErrNoPages
	}
	if page == "" {
		page = inst.Manifest.Navigation[0].Page
	}
	declared := false
	for _, nav := range inst.Manifest.Navigation {
		if nav.Page == page {
			declared = true
			break
		}
	}
	if !declared {
		return result, &plugin.Error{Code: "PAGE_NOT_FOUND", Message: "页面未在应用中声明", Status: 404}
	}
	result.PackageHash, err = a.plugins.PackageHash(id)
	if err != nil {
		return result, err
	}
	runner, err := a.agentRunner(inst)
	if err != nil {
		return result, err
	}
	defer runner.Close()
	result.Page, err = runner.Page(page)
	if err == nil {
		result.EvaluatedAt = time.Now().UTC()
	}
	return result, err
}

// agentRunner builds a runtime for one API request. The host is the app's,
// with both signing functions replaced by a refusal.
func (a *App) agentRunner(inst plugin.Installed) (*plugin.Runner, error) {
	var runner *plugin.Runner
	var err error
	a.runOnMain(func() {
		source, rerr := os.ReadFile(filepath.Join(inst.Dir, filepath.FromSlash(inst.Manifest.Main)))
		if rerr != nil {
			err = fmt.Errorf("app %s: %w", inst.Manifest.ID, rerr)
			return
		}
		deps := a.pluginHostDeps(inst.Manifest)
		refuse := func() (plugin.SignResult, error) {
			return plugin.SignResult{}, errors.New("signing is not available over the local API")
		}
		deps.Sign = func(plugin.SignRequest) (plugin.SignResult, error) { return refuse() }
		deps.NearSign = func(plugin.NearSignRequest) (plugin.SignResult, error) { return refuse() }
		runner, err = plugin.NewRunner(inst.Manifest, source, plugin.NewHost(deps), a.lang())
	})
	return runner, err
}
