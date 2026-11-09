package app

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/agent"
	"github.com/davirain-su/lintel/internal/plugin"
)

const maxInstallationVerifications = 5

// installationVerification 只跟踪本进程中已批准的包，不保存页面或异常原文。
// result 只在主线程读写；后台仅通过原子标记检查本轮是否已失效。
type installationVerification struct {
	result    agent.Verification
	cancelled atomic.Bool
}

func (a *App) beginInstallationVerification(job plugin.Installation) {
	if a.quitting || job.Status != "installed" {
		return
	}
	a.forgetInstallationVerification(job.PluginID)
	if a.installationVerifications == nil {
		a.installationVerifications = make(map[string]*installationVerification)
	}
	result := agent.Verification{
		RequestID: job.RequestID, PackageHash: job.PackageHash, Status: "waiting_settings",
	}
	if len(job.Manifest.Navigation) > 0 {
		result.Page = job.Manifest.Navigation[0].Page
	}
	a.installationVerifications[job.PluginID] = &installationVerification{result: result}
	a.installationVerificationFeedback(result)
	if len(job.Manifest.Settings) == 0 {
		a.verifyInstalledApp(job.PluginID)
	}
}

func (a *App) verifyInstalledApp(id string) {
	previous := a.installationVerifications[id]
	if a.quitting || previous == nil || previous.result.Status == "running" {
		return
	}
	if a.runningInstallationVerifications >= maxInstallationVerifications {
		a.invalidateInstallationVerification(id, "APP_BUSY")
		a.invalidate()
		return
	}
	previous.cancelled.Store(true)
	result := previous.result
	result.Status, result.Code, result.CheckedAt = "running", "", nil
	run := &installationVerification{result: result}
	a.installationVerifications[id] = run
	a.runningInstallationVerifications++
	a.installationVerificationFeedback(result)
	a.invalidate()
	// 不把可变 UI 状态带入后台，也不复用允许原生交易批准的页面 runner。
	expectedHash := result.PackageHash
	a.goAsync(func() {
		_, err := a.readOnlyPage(id, "", readOnlyPageOptions{
			expectedHash: expectedHash, quiet: true,
			active: func() bool { return !run.cancelled.Load() },
		})
		a.runOnMain(func() {
			a.runningInstallationVerifications--
			if a.quitting || a.installationVerifications[id] != run || run.cancelled.Load() {
				return
			}
			// lease 释放后可能已有更新、设置变更或卸载；迟到结果不能冒充新包。
			inst, currentErr := a.plugins.Get(id)
			if currentErr != nil {
				a.forgetInstallationVerification(id)
				a.invalidate()
				return
			}
			hash, hashErr := a.plugins.PackageHash(id)
			if hashErr != nil || hash != expectedHash || inst.Err != "" {
				a.forgetInstallationVerification(id)
				a.invalidate()
				return
			}
			if !inst.Enabled {
				err = agent.ErrDisabled
			}
			at := time.Now().UTC()
			run.result.CheckedAt = &at
			run.result.Status = "passed"
			if err != nil {
				run.result.Status = "failed"
				run.result.Code = installationVerificationCode(err)
			}
			a.installationVerificationFeedback(run.result)
			a.invalidate()
		})
	})
}

func installationVerificationCode(err error) string {
	switch {
	case errors.Is(err, agent.ErrDisabled):
		return "APP_DISABLED"
	case errors.Is(err, agent.ErrNoPages):
		return "NO_PAGES"
	case errors.Is(err, agent.ErrNotFound), errors.Is(err, plugin.ErrNotInstalled):
		return "APP_NOT_FOUND"
	}
	var known *plugin.Error
	if errors.As(err, &known) {
		switch known.Code {
		case "APP_BUSY", "CONTENT_CHANGED":
			return known.Code
		}
	}
	return "EXECUTION_FAILED"
}

func (a *App) forgetInstallationVerification(id string) {
	if old := a.installationVerifications[id]; old != nil {
		old.cancelled.Store(true)
		delete(a.installationVerifications, id)
	}
}

func (a *App) invalidateInstallationVerification(id, code string) {
	old := a.installationVerifications[id]
	if old == nil {
		return
	}
	old.cancelled.Store(true)
	result := old.result
	result.Status, result.Code, result.CheckedAt = "failed", code, nil
	if code == "SETTINGS_CHANGED" {
		result.Status = "waiting_settings"
	}
	a.installationVerifications[id] = &installationVerification{result: result}
	a.installationVerificationFeedback(result)
}

// ApplicationSettingsChanged 仅在保存成功且没有清空秘密时自动验证。
func (a *App) ApplicationSettingsChanged(id string, success, clearedSecret bool) {
	a.runOnMain(func() {
		a.ApplicationsChanged(id)
		if !success {
			a.invalidateInstallationVerification(id, "SETTINGS_WRITE_FAILED")
		} else if !clearedSecret {
			a.verifyInstalledApp(id)
		}
		a.invalidate()
	})
}

func (a *App) installationVerificationResult(id, hash string) *agent.Verification {
	current := a.installationVerifications[id]
	if current == nil || hash == "" || current.result.PackageHash != hash {
		return nil
	}
	result := current.result
	return &result
}

func (a *App) installationVerificationFeedback(result agent.Verification) {
	if result.RequestID != "" && result.RequestID == a.coding.installationID {
		a.coding.notice = "应用已安装 · " + installationVerificationMessage(result)
	}
}

func installationVerificationMessage(result agent.Verification) string {
	switch result.Status {
	case "waiting_settings":
		return "等待保存设置后验证"
	case "running":
		return "正在验证首页…"
	case "passed":
		return "首页已通过运行检查"
	}
	switch result.Code {
	case "APP_BUSY":
		return "应用或验证任务正忙，请稍后重试"
	case "APP_DISABLED":
		return "应用已禁用，启用后可验证"
	case "NO_PAGES":
		return "应用没有可验证的页面"
	case "SETTINGS_WRITE_FAILED":
		return "设置未完整保存，请重新保存后验证"
	case "HOST_CHANGED":
		return "运行环境已变化，请重新验证"
	case "CONTENT_CHANGED", "APP_NOT_FOUND":
		return "应用内容已变化，请检查安装状态"
	default:
		return "首页验证失败，请检查设置或修改应用"
	}
}

func (a *App) installationVerificationView(c *ui.Context, id string) {
	current := a.installationVerifications[id]
	if current == nil {
		return
	}
	result, p := current.result, a.pal
	color := p.muted
	if result.Status == "passed" {
		color = p.success
	} else if result.Status == "failed" {
		color = p.danger
	}
	ui.Column(c).Gap(7).Padding(12).Radius(9).Background(p.inset).Children(func() {
		ui.Text(c, "安装后运行验证").FontSize(12).FontWeight(600)
		ui.Text(c, installationVerificationMessage(result)).FontSize(12).TextColor(color).MaxLines(3)
		ui.Text(c, "检查首页执行和页面结构；请打开应用核对数据。验证不会请求钱包签名。").FontSize(11).TextColor(p.muted).MaxLines(3)
		if result.CheckedAt != nil {
			ui.Text(c, "检查时间 "+result.CheckedAt.Local().Format("15:04:05")).FontSize(10.5).TextColor(p.faint)
		}
		if result.Status != "running" {
			ui.Row(c).Gap(8).Wrap().Children(func() {
				label := "验证首页"
				if result.Status == "passed" {
					label = "重新验证"
					a.softButton(c, "打开应用", nil, p.text, func() { a.openPluginApp(id) })
				}
				a.softButton(c, label, nil, p.accent, func() { a.verifyInstalledApp(id) })
			})
		}
	})
}
