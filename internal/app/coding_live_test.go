package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/davirain-su/lintel/internal/agent/authoring"
)

// 仅在显式指定本机 executable 时检查真实适配器，不发送付费生成 prompt。
func TestInstalledACPHandshake(t *testing.T) {
	executable := os.Getenv("LINTEL_TEST_ACP_EXECUTABLE")
	if executable == "" {
		t.Skip("需要 LINTEL_TEST_ACP_EXECUTABLE；此测试只做握手与 session/new")
	}
	a := newTestApp(t)
	a.initAuthoring()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	init, session, err := a.coding.service.Discover(ctx, authoring.Profile{ID: "live", Name: "Live test", Executable: executable}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != 1 || session.ID == "" {
		t.Fatal("缺少完整握手或会话")
	}
	if init.AgentInfo != nil {
		t.Logf("agent=%s version=%s protocol=%d configOptions=%d", safeAgentText(init.AgentInfo.Name, 120), safeAgentText(init.AgentInfo.Version, 80), init.ProtocolVersion, len(session.ConfigOptions))
	}
}
