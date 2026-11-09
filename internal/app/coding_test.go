package app

import (
	"encoding/json"
	"testing"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/egoist/mygo/ui"
)

func TestCodingAgentSettingsAndCreateEntry(t *testing.T) {
	a := newTestApp(t)
	a.initAuthoring()
	a.route = routeSettings
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("编码 Agent") || !tt.HasText("Claude Code (ACP)") {
		t.Fatal("设置没有编码agent入口")
	}
	a.openCreator("")
	tt.Frame()
	if !tt.HasText("创建个人应用") || !tt.HasText("开始生成") {
		t.Fatal("没有原生创建入口")
	}
}
func TestPermissionChoiceRequiresAdvertisedOption(t *testing.T) {
	req := acp.PermissionRequest{Options: []acp.PermissionOption{{OptionID: "once", Name: "Allow once", Kind: "allow_once"}, {OptionID: "deny", Name: "Reject", Kind: "reject_once"}}}
	for _, id := range []string{"", "bypass", "full-access"} {
		if codingOption(req, id) != "" {
			t.Fatal("接受了虚构许可")
		}
	}
	if codingOption(req, "once") != "once" {
		t.Fatal("丢失有效optionId")
	}
}
func TestCreatorCannotModifyOfficialOrSignedApp(t *testing.T) {
	a := newTestApp(t)
	a.initAuthoring()
	a.openCreator("com.example.gas")
	if a.coding.open || a.coding.targetID != "" {
		t.Fatal("官方应用进入覆盖流程")
	}
}
func TestProfileArgumentsAreJSONArray(t *testing.T) {
	a := newTestApp(t)
	a.initAuthoring()
	a.coding.profileID = "custom"
	a.coding.executable = "/usr/local/bin/custom-acp"
	a.coding.args = `["--name", "with spaces"]`
	if !a.saveCodingProfile() {
		t.Fatal("合法参数数组被拒绝")
	}
	var values []string
	if json.Unmarshal([]byte(a.coding.args), &values) != nil || len(values) != 2 {
		t.Fatal("参数被shell拆分")
	}
	a.coding.args = `--unsafe; curl example.invalid`
	if a.saveCodingProfile() {
		t.Fatal("把shell字符串当成argv执行")
	}
}
