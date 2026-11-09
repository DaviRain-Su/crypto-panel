package authoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/davirain-su/lintel/internal/acp"
)

func (p *fakePeer) SetConfigOption(ctx context.Context, session, key, value string) (acp.Session, error) {
	if p.setConfig != nil {
		return p.setConfig(ctx, session, key, value)
	}
	return acp.Session{}, acp.ErrUnsupportedConfig
}

func TestExplicitConfigValuesAppliedBeforePrompt(t *testing.T) {
	s, p, _, _ := harness(t)
	req := testRequest(t)
	req.ConfigValues = map[string]string{"mode": "safe", "model": "advertised-model"}
	var keys []string
	p.setConfig = func(_ context.Context, session, key, value string) (acp.Session, error) {
		if session != "test-session" || req.ConfigValues[key] != value {
			t.Fatal("配置值被猜测或改写")
		}
		keys = append(keys, key)
		return acp.Session{ID: session}, nil
	}
	p.prompt = func(context.Context, string, string) (acp.PromptResult, error) {
		if !reflect.DeepEqual(keys, []string{"mode", "model"}) {
			t.Fatalf("配置未在 prompt 前按确定顺序应用: %v", keys)
		}
		return acp.PromptResult{StopReason: "end_turn"}, writeApp(filepath.Join(p.cfg.Cwd, "app"), "local.personal.config", "1.0.0", testCode)
	}
	if _, err := s.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedConfigNeverPromptsOrProposes(t *testing.T) {
	s, p, m, _ := harness(t)
	req := testRequest(t)
	req.ConfigValues = map[string]string{"model": "invented-model"}
	if _, err := s.Run(context.Background(), req); !errors.Is(err, acp.ErrUnsupportedConfig) {
		t.Fatalf("未拒绝未知配置: %v", err)
	}
	if p.prompts.Load() != 0 {
		t.Fatal("未知配置仍发 prompt")
	}
	requireNoProposal(t, m)
}

func TestDiscoverCreatesOnlyTemporarySession(t *testing.T) {
	s, p, m, _ := harness(t)
	root := t.TempDir()
	init, session, err := s.Discover(context.Background(), testProfile(t), root)
	if err != nil || init.ProtocolVersion != acp.ProtocolVersion || session.ID != "test-session" {
		t.Fatalf("Discover: %+v %+v %v", init, session, err)
	}
	if p.sessions.Load() != 1 || p.prompts.Load() != 0 || p.closes.Load() != 1 {
		t.Fatal("Discover 发起了 prompt 或未清理")
	}
	if _, err := os.Stat(p.cfg.Cwd); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Discover 目录未清理")
	}
	requireNoProposal(t, m)
}
