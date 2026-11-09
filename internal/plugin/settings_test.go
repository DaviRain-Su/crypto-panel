package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCredential(t *testing.T) string {
	t.Helper()
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(random[:])
}

type failingSecrets struct {
	memSecrets
	failKey string
	raw     string
}

func (s *failingSecrets) Set(key string, value []byte) error {
	if key == s.failKey {
		return errors.New(s.raw + string(value))
	}
	return s.memSecrets.Set(key, value)
}
func TestSettingsSafeBatchAndClear(t *testing.T) {
	sec := &memSecrets{m: map[string][]byte{}}
	s, m := newTestManager(t, sec)
	i := installTest(t, m, personalPackage(t, "local.personal.settings", "1.0.0"))
	secret := testCredential(t)
	statuses, err := s.SetSettings(i.PluginID, map[string]string{"plain": "普通值", "token": secret})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(statuses)
	if strings.Contains(string(data), secret) {
		t.Fatal("响应泄露凭据")
	}
	for _, status := range statuses {
		if status.Secret && (status.Value != nil || status.Configured == nil || !*status.Configured) {
			t.Fatal("secret 元数据无效")
		}
	}
	state, _ := os.ReadFile(filepath.Join(s.Dir(), i.PluginID, "state.json"))
	if strings.Contains(string(state), secret) {
		t.Fatal("凭据落入 state.json")
	}
	if _, err := s.SetSettings(i.PluginID, map[string]string{"plain": "不应写入", "undeclared": "bad"}); err == nil {
		t.Fatal("接受非法批次")
	}
	if got, _ := s.Setting(i.PluginID, "plain"); got != "普通值" {
		t.Fatal("非法批次写入了普通字段")
	}
	if _, err := s.SetSettings(i.PluginID, map[string]string{"plain": strings.Repeat("x", MaxSettingValueBytes+1)}); err == nil {
		t.Fatal("接受超限值")
	}
	if _, err := s.SetSettings(i.PluginID, map[string]string{"plain": strings.Repeat("x", MaxSettingValueBytes)}); err != nil {
		t.Fatal(err)
	}
	statuses, err = s.SetSettings(i.PluginID, map[string]string{"token": ""})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sec.m[secretKey(i.PluginID, "token")]; ok {
		t.Fatal("清除没有删除凭据")
	}
	for _, status := range statuses {
		if status.Secret && (status.Value != nil || status.Configured == nil || *status.Configured) {
			t.Fatal("清除状态无效")
		}
	}
}
func TestSettingsPartialFailureDoesNotLeak(t *testing.T) {
	sec := &failingSecrets{memSecrets: memSecrets{m: map[string][]byte{}}, failKey: "local.personal.settings/token", raw: "platform private details"}
	s, m := newTestManager(t, sec)
	i := installTest(t, m, personalPackage(t, "local.personal.settings", "1.0.0"))
	credential := testCredential(t)
	_, err := s.SetSettings(i.PluginID, map[string]string{"plain": "kept", "token": credential})
	var partial *SettingsWriteError
	if !errors.As(err, &partial) || len(partial.AppliedKeys) != 1 || partial.AppliedKeys[0] != "plain" || len(partial.FailedKeys) != 1 || partial.FailedKeys[0] != "token" {
		t.Fatalf("部分失败键名不准确: %v", err)
	}
	if strings.Contains(err.Error(), sec.raw) || strings.Contains(err.Error(), credential) {
		t.Fatal("错误泄露平台原文或凭据")
	}
	if got, _ := s.Setting(i.PluginID, "plain"); got != "kept" {
		t.Fatal("未报告准确已写入值")
	}
}
