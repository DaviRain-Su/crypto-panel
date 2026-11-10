package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/davirain-su/lintel/internal/acp"
	"github.com/davirain-su/lintel/internal/i18n"
)

func TestCodingAgentSettingsAndCreateEntry(t *testing.T) {
	a := newTestApp(t)
	a.initAuthoring()
	a.route = routeSettings
	tt := ui.NewTester(a.view, windowWidth, windowHeight)
	if !tt.HasText("CODING AGENT") || !tt.HasText("Claude Code (ACP)") {
		t.Fatal("设置没有编码agent入口")
	}
	a.openCreator("")
	tt.Frame()
	if !tt.HasText("Create a personal app") || !tt.HasText("Generate") {
		t.Fatal("没有原生创建入口")
	}
}

type fakeCodingWindow struct {
	onClose func(*mygo.CloseEvent)
	opened  chan *fakeCodingWindow
	closed  bool
	title   string
}

func (f *fakeCodingWindow) OnClose(fn func(*mygo.CloseEvent)) func() {
	f.onClose = fn
	if f.opened != nil {
		f.opened <- f
	}
	return func() {}
}
func (f *fakeCodingWindow) Focus() {}
func (f *fakeCodingWindow) Close() { f.closed = true }

func TestClosingCodingPermissionWindowRefuses(t *testing.T) {
	for _, lang := range []string{i18n.English, i18n.Chinese} {
		t.Run(lang, func(t *testing.T) {
			testClosingCodingPermissionWindowRefuses(t, lang)
		})
	}
}

func testClosingCodingPermissionWindowRefuses(t *testing.T, lang string) {
	t.Helper()
	a := newTestApp(t)
	a.win = new(mygo.Window)
	opened := make(chan *fakeCodingWindow, 1)
	previous := openCodingPermissionWindow
	openCodingPermissionWindow = func(opts mygo.WindowOptions) codingPermissionWindow {
		return &fakeCodingWindow{opened: opened, title: opts.Title}
	}
	t.Cleanup(func() { openCodingPermissionWindow = previous })
	a.settings.Language = lang
	req := acp.PermissionRequest{Options: []acp.PermissionOption{{OptionID: "allow-once", Name: "Allow once", Kind: "allow_once"}}}
	type answer struct {
		id  string
		err error
	}
	got := make(chan answer, 1)
	go func() {
		id, err := a.requestCodingPermission(context.Background(), req)
		got <- answer{id, err}
	}()
	var win *fakeCodingWindow
	select {
	case win = <-opened:
	case <-time.After(time.Second):
		t.Fatal("没有打开编码许可窗口")
	}
	if win.title != a.t(i18n.CodingWindowTitle) {
		t.Fatalf("%s 窗口标题不是 i18n 文案: %q", lang, win.title)
	}
	win.onClose(nil)
	select {
	case result := <-got:
		if result.err != nil || result.id != "" || !win.closed {
			t.Fatalf("关闭窗口被当成批准: %q %v closed=%v", result.id, result.err, win.closed)
		}
	case <-time.After(time.Second):
		t.Fatal("关闭窗口没有回答许可请求")
	}
}

func TestCancelClosesCodingPermissionWindow(t *testing.T) {
	a := newTestApp(t)
	a.win = new(mygo.Window)
	opened := make(chan *fakeCodingWindow, 1)
	previous := openCodingPermissionWindow
	openCodingPermissionWindow = func(mygo.WindowOptions) codingPermissionWindow {
		return &fakeCodingWindow{opened: opened}
	}
	t.Cleanup(func() { openCodingPermissionWindow = previous })
	ctx, cancel := context.WithCancel(context.Background())
	req := acp.PermissionRequest{Options: []acp.PermissionOption{{OptionID: "allow-once", Name: "Allow once", Kind: "allow_once"}}}
	got := make(chan error, 1)
	go func() {
		_, err := a.requestCodingPermission(ctx, req)
		got <- err
	}()
	var win *fakeCodingWindow
	select {
	case win = <-opened:
	case <-time.After(time.Second):
		t.Fatal("没有打开编码许可窗口")
	}
	cancel()
	select {
	case err := <-got:
		if err == nil || !win.closed {
			t.Fatalf("取消没有关闭窗口: %v closed=%v", err, win.closed)
		}
	case <-time.After(time.Second):
		t.Fatal("取消没有结束许可请求")
	}
}

func TestChoosingPermissionOptionClosesWindowAndFiltersID(t *testing.T) {
	a := newTestApp(t)
	req := acp.PermissionRequest{Options: []acp.PermissionOption{
		{OptionID: "allow-once", Name: "Allow once", Kind: "allow_once"},
		{OptionID: "reject-once", Name: "Reject", Kind: "reject_once"},
	}}
	win := &fakeCodingWindow{}
	answer := make(chan string, 1)
	decided := false
	decide := func(id string) {
		finishCodingPermission(&decided, answer, win, req, id)
	}
	tt := ui.NewTester(codingPermissionView(a, context.Background(), req, "detail", decide), 620, 620)
	if !tt.HasText(a.t(i18n.CodingPermissionTitle)) {
		t.Fatal("许可正文没有使用 i18n 标题")
	}
	info, ok := tt.Access("Allow once")
	if !ok {
		t.Fatal("没有允许按钮")
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	if !win.closed {
		t.Fatal("选择许可没有关闭窗口")
	}
	if got := <-answer; got != "allow-once" {
		t.Fatalf("选择结果 %q", got)
	}
	win.closed = false
	decided = false
	finishCodingPermission(&decided, answer, win, req, "bypass")
	if got := <-answer; got != "" || !win.closed {
		t.Fatalf("未列出的 optionId 被接受: %q closed=%v", got, win.closed)
	}
}

func TestStartupSweepsStaleAuthoringWorkspaces(t *testing.T) {
	a := newTestApp(t)
	root := a.workspaceRoot()
	stale := filepath.Join(root, "lintel-authoring-stale")
	contextDir := filepath.Join(stale, "context")
	if err := os.MkdirAll(contextDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "catalog.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(contextDir, "catalog.json"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(contextDir, 0o500); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(root, "notes")
	if err := os.Mkdir(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	a.initAuthoring()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("启动后仍留着过期生成目录")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("启动清扫删除了无关目录")
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
