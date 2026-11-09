package app

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/plugin"
)

type verificationSnapshot struct {
	RequestID   string     `json:"requestId"`
	PackageHash string     `json:"packageHash"`
	Page        string     `json:"page"`
	Status      string     `json:"status"`
	Code        string     `json:"code"`
	CheckedAt   *time.Time `json:"checkedAt"`
}

func installedVerification(t *testing.T, a *App, id string) *verificationSnapshot {
	t.Helper()
	for _, item := range a.Plugins() {
		if item.ID != id {
			continue
		}
		data, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Verification *verificationSnapshot `json:"verification"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out.Verification
	}
	return nil
}

func verificationSource(t *testing.T, id, code string, fields ...plugin.SettingField) plugin.Source {
	t.Helper()
	manifest, err := json.Marshal(plugin.Manifest{
		ID: id, Name: "Verification app", Version: "1.0.0", Main: "index.js",
		Permissions: plugin.Permissions{StorageKb: 4}, Settings: fields,
		Navigation: []plugin.NavItem{{Title: "Overview", Page: "overview"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plugin.Source{Kind: "inline", Package: &plugin.Package{Plugin: manifest, Main: code}}
}

func proposeVerification(t *testing.T, a *App, source plugin.Source, via string) plugin.Installation {
	t.Helper()
	job, err := a.ApplicationManager().Propose(source, "install", "", "", via)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestInstallationVerificationStartsOnlyAfterApproval(t *testing.T) {
	for _, via := range []string{"acp", "local-api"} {
		t.Run(via, func(t *testing.T) {
			a := newTestApp(t)
			job := proposeVerification(t, a, verificationSource(t, "local.personal.verify", `
export function page({name}) {
  if (name !== "overview") throw new Error("wrong page");
  host.kv.put("checked", true);
  return {title:"Verified", sections:[]};
}`), via)
			a.ApplicationsChanged("")
			if installedVerification(t, a, job.PluginID) != nil {
				t.Fatal("待确认任务已有运行验证结果")
			}
			if _, err := a.plugins.Get(job.PluginID); !errors.Is(err, plugin.ErrNotInstalled) {
				t.Fatal("未批准的候选已安装")
			}
			a.reviewInstallation(job.RequestID)
			got := installedVerification(t, a, job.PluginID)
			if got == nil || got.Status != "passed" || got.PackageHash != job.PackageHash || got.RequestID != job.RequestID || got.Page != "overview" || got.CheckedAt == nil || got.CheckedAt.IsZero() {
				t.Fatalf("安装后没有自动验证首页：%+v", got)
			}
			kv, err := a.plugins.KV(job.PluginID)
			if err != nil {
				t.Fatal(err)
			}
			if checked, _ := kv.Get("checked"); string(checked) != "true" {
				t.Fatal("通过状态没有对应实际页面执行")
			}
			if state, _ := a.ApplicationManager().GetInstallation(job.RequestID); state.Status != "installed" {
				t.Fatal("运行验证改变了安装任务语义")
			}
			tt := ui.NewTester(a.view, windowWidth, windowHeight)
			if !tt.HasText("The first page passed the run check.") {
				t.Fatalf("原生设置没有展示验证结果：%q", tt.Texts())
			}
		})
	}
}

func TestInstallationVerificationWaitsForSettingsSave(t *testing.T) {
	a := newTestApp(t)
	job := proposeVerification(t, a, verificationSource(t, "local.personal.configured", `
export function page() {
  if (host.setting("label") !== "ready") throw new Error("not configured");
  host.kv.put("checked", true);
  return {sections:[]};
}`, plugin.SettingField{Key: "label", Label: "Label"}), "acp")
	a.reviewInstallation(job.RequestID)
	got := installedVerification(t, a, job.PluginID)
	if got == nil || got.Status != "waiting_settings" || got.CheckedAt != nil {
		t.Fatalf("有设置的应用没有等待配置：%+v", got)
	}
	kv, err := a.plugins.KV(job.PluginID)
	if err != nil {
		t.Fatal(err)
	}
	if checked, _ := kv.Get("checked"); checked != nil {
		t.Fatal("保存设置前已执行页面")
	}
	*a.pluginDrafts[job.PluginID+"/label"] = "ready"
	a.savePluginSettings(job.Manifest)
	got = installedVerification(t, a, job.PluginID)
	if got == nil || got.Status != "passed" {
		t.Fatalf("保存成功没有触发验证：%+v", got)
	}
}

func TestInstallationVerificationRuntimeFailuresKeepInstalledApp(t *testing.T) {
	for name, code := range map[string]string{
		"module": `throw new Error("module failed"); export function page(){return {sections:[]};}`,
		"page":   `export function page(){throw new Error("page failed");}`,
		"schema": `export function page(){return {sections:"invalid"};}`,
	} {
		t.Run(name, func(t *testing.T) {
			a := newTestApp(t)
			job := proposeVerification(t, a, verificationSource(t, "local.personal.failed", code), "acp")
			a.reviewInstallation(job.RequestID)
			got := installedVerification(t, a, job.PluginID)
			if got == nil || got.Status != "failed" || got.Code != "EXECUTION_FAILED" {
				t.Fatalf("运行错误没有安全失败状态：%+v", got)
			}
			if _, err := a.plugins.Get(job.PluginID); err != nil {
				t.Fatal("运行失败错误地移除了已批准应用")
			}
		})
	}
}

func TestInstallationVerificationRefusesBothSigningFunctions(t *testing.T) {
	for name, call := range map[string]string{
		"evm":  `host.requestSign({chainId:"eip155:1",to:"0x2c7536e3605d9c16a7a3d7b1898e529396a65c23",value:"1"});`,
		"near": `host.requestNearSign({chainId:"near:testnet",receiver:"receiver.testnet",actions:[{type:"transfer",amount:"1"}]});`,
	} {
		for _, topLevel := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/page", true: "/module"}[topLevel], func(t *testing.T) {
				a := newTestApp(t)
				approvals := 0
				a.approve = func(approvalRequest) bool { approvals++; return true }
				body := `host.kv.put("attempted",true);` + call
				code := `export function page(){` + body + `return {sections:[]};}`
				if topLevel {
					code = body + `export function page(){return {sections:[]};}`
				}
				source := verificationSource(t, "local.personal.signing", code)
				var manifest plugin.Manifest
				if err := json.Unmarshal(source.Package.Plugin, &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.Permissions.Sign = true
				manifest.Permissions.Chains = []string{"eip155:1", "near:testnet"}
				source.Package.Plugin, _ = json.Marshal(manifest)
				job := proposeVerification(t, a, source, "acp")
				a.reviewInstallation(job.RequestID)
				got := installedVerification(t, a, job.PluginID)
				if got == nil || got.Status != "failed" || got.Code != "EXECUTION_FAILED" || approvals != 0 {
					t.Fatalf("安装后验证绕过禁签名上下文：%+v，批准次数 %d", got, approvals)
				}
				kv, err := a.plugins.KV(job.PluginID)
				if err != nil {
					t.Fatal(err)
				}
				if attempted, _ := kv.Get("attempted"); string(attempted) != "true" {
					t.Fatal("测试没有实际执行到签名调用前")
				}
			})
		}
	}
}

func TestInstallationVerificationDoesNotExposeSecretErrorsOrLogs(t *testing.T) {
	a := newTestApp(t)
	_ = a.pluginManager.Close()
	a.plugins = plugin.NewStore(a.plugins.Dir(), memStore{})
	var err error
	a.pluginManager, err = plugin.NewManager(a.plugins)
	if err != nil {
		t.Fatal(err)
	}
	secret := rand.Text()
	job := proposeVerification(t, a, verificationSource(t, "local.personal.secret", `
export function page(){
  host.log("error",host.setting("apiKey"));
  throw new Error(host.setting("apiKey"));
}`, plugin.SettingField{Key: "apiKey", Label: "API key", Secret: true}), "acp")
	a.reviewInstallation(job.RequestID)
	*a.pluginDrafts[job.PluginID+"/apiKey"] = secret
	a.savePluginSettings(job.Manifest)
	got := installedVerification(t, a, job.PluginID)
	if got == nil || got.Status != "failed" || got.Code != "EXECUTION_FAILED" {
		t.Fatalf("秘密值错误没有安全失败状态：%+v", got)
	}
	encoded, _ := json.Marshal(a.Plugins())
	if strings.Contains(string(encoded), secret) || strings.Contains(a.pluginNotice, secret) || strings.Contains(a.coding.notice, secret) {
		t.Fatal("验证结果泄露应用凭据")
	}
	for _, entry := range a.pluginLog(job.PluginID).Entries() {
		if strings.Contains(entry.Message, secret) {
			t.Fatal("验证把应用凭据写入日志")
		}
	}
}

func TestInstallationVerificationRejectedCandidateNeverRuns(t *testing.T) {
	a := newTestApp(t)
	a.confirmInstall = func(_ plugin.Manifest, _ installRequest, done func(bool)) { done(false) }
	job := proposeVerification(t, a, verificationSource(t, "local.personal.rejected", `
host.kv.put("loaded",true); export function page(){return {sections:[]};}`), "acp")
	a.reviewInstallation(job.RequestID)
	if installedVerification(t, a, job.PluginID) != nil {
		t.Fatal("拒绝安装后仍有运行验证")
	}
	if _, err := a.plugins.Get(job.PluginID); !errors.Is(err, plugin.ErrNotInstalled) {
		t.Fatal("拒绝的应用被运行或安装")
	}
}
