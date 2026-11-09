package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/agent"
	"github.com/davirain-su/lintel/internal/plugin"
)

var _ agent.Management = (*App)(nil)

// ApplicationManager 在启动期间初始化，之后不替换，供 HTTP 安全地取得。
func (a *App) ApplicationManager() *plugin.Manager { return a.pluginManager }

// ApplicationsChanged 把后台变更归并回主线程，丢弃旧 runtime 和旧页面结果。
func (a *App) ApplicationsChanged(id string) {
	a.runOnMain(func() {
		if a.pluginKey().id == id {
			a.pluginGen++
		}
		a.stopRunner(id)
		a.invalidateInstallationVerification(id, "SETTINGS_CHANGED")
		for key := range a.pluginDrafts {
			if strings.HasPrefix(key, id+"/") {
				delete(a.pluginDrafts, key)
			}
		}
		a.refreshPlugins()
		if a.pluginKey().id == id {
			a.pluginView = pluginPageState{}
		}
		a.invalidate()
	})
}

// reviewInstallation 只由用户的审阅操作调用，不向 HTTP 或 ACP 暴露批准。
func (a *App) reviewInstallation(requestID string) {
	if a.pluginManager == nil || a.installOpen || a.confirmInstall == nil {
		return
	}
	job, err := a.pluginManager.GetInstallation(requestID)
	if err != nil || job.Status != "pending" {
		a.pluginNotice = "此请求已结束或过期，请重新提交"
		a.invalidate()
		return
	}
	req := installRequest{unsigned: true, source: job.Via, update: job.Operation == "update", old: job.OldManifest}
	a.confirmInstall(job.Manifest, req, func(allow bool) {
		a.goAsync(func() {
			result, err := a.pluginManager.Decide(requestID, allow)
			a.runOnMain(func() {
				if err != nil {
					a.pluginNotice = err.Error()
				} else {
					a.pluginNotice = job.Manifest.Name + " · " + result.Status
				}
				if result.Status == "installed" {
					a.pluginGen++
					a.stopRunner(job.PluginID)
					for key := range a.pluginDrafts {
						if strings.HasPrefix(key, job.PluginID+"/") {
							delete(a.pluginDrafts, key)
						}
					}
					a.refreshPlugins()
					a.openPluginDetail(job.PluginID)
					a.beginInstallationVerification(result)
				}
				a.invalidate()
			})
		})
	})
}

func (a *App) cancelInstallation(id string) {
	if a.pluginManager == nil {
		return
	}
	if _, err := a.pluginManager.Cancel(id); err != nil {
		a.pluginNotice = err.Error()
	}
	a.invalidate()
}

func (a *App) pendingApplications(c *ui.Context) {
	if a.pluginManager == nil {
		return
	}
	jobs := a.pluginManager.Installations()
	if len(jobs) == 0 {
		return
	}
	p := a.pal
	ui.Column(c).Gap(8).Margin(0, 0, 16, 0).Children(func() {
		ui.Text(c, "待确认的应用").FontSize(13).FontWeight(600)
		for _, job := range jobs {
			ui.Column(c).Key("pending-"+job.RequestID).Gap(6).Padding(14).Radius(10).Background(p.card).Border(1, p.hairline).Children(func() {
				label := "安装"
				if job.Operation == "update" {
					label = "更新"
				}
				ui.Text(c, fmt.Sprintf("%s · %s v%s", label, job.Manifest.Name, job.Version)).FontSize(13).FontWeight(600).MaxLines(2)
				ui.Text(c, job.PluginID+" · "+job.Via+" · "+job.Status).FontSize(10.5).TextColor(p.muted).MaxLines(2)
				ui.Text(c, "Unsigned · "+job.PackageHash).Font("monospace").FontSize(10).TextColor(p.faint).MaxLines(2)
				ui.Text(c, "确认前不会运行；到期时间 "+job.ExpiresAt.Local().Format("15:04:05")).FontSize(11).TextColor(p.muted)
				if job.Status == "pending" {
					ui.Row(c).Gap(8).Children(func() {
						a.softButton(c, "审阅权限", nil, p.accent, func() { a.reviewInstallation(job.RequestID) })
						a.softButton(c, "取消请求", nil, p.muted, func() { a.cancelInstallation(job.RequestID) })
					})
				}
			})
		}
	})
}

func (a *App) clearPluginSecret(id, key string) {
	if a.pluginManager == nil {
		return
	}
	if _, err := a.pluginManager.SetSettings(id, map[string]string{key: ""}); err != nil {
		a.pluginNotice = err.Error()
		a.invalidate()
		return
	}
	a.invalidateInstallationVerification(id, "SETTINGS_CHANGED")
	if a.pluginKey().id == id {
		a.pluginGen++
		a.pluginView = pluginPageState{}
	}
	a.stopRunner(id)
	if draft := a.pluginDrafts[id+"/"+key]; draft != nil {
		*draft = ""
	}
	a.pluginNotice = "已清除秘密设置"
	a.invalidate()
}
