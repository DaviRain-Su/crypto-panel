package authoring

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/davirain-su/lintel/internal/acp"
)

// 子进程复用测试二进制，仅实现本测试所需的假 peer，不访问模型或网络。
func TestAuthoringPeerProcess(t *testing.T) {
	i := slices.Index(os.Args, "lintel-authoring-test-peer")
	if i < 0 {
		return
	}
	if i+1 >= len(os.Args) {
		os.Exit(2)
	}
	if err := serveAuthoringPeer(os.Args[i+1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func serveAuthoringPeer(mode string) error {
	type message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var cwd string
	var promptID json.RawMessage
	send := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(data, '\n'))
		return err
	}
	reply := func(id json.RawMessage, result any) error {
		return send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	options := []any{map[string]any{"id": "model", "name": "Model", "type": "select", "currentValue": "local-test-model", "options": []any{map[string]string{"value": "local-test-model", "name": "Local test"}}}}
	for scanner.Scan() {
		var msg message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			return err
		}
		switch msg.Method {
		case "initialize":
			if err := reply(msg.ID, map[string]any{"protocolVersion": acp.ProtocolVersion, "agentInfo": map[string]string{"name": "local-test-peer", "version": "1"}, "agentCapabilities": map[string]any{}}); err != nil {
				return err
			}
		case "session/new":
			var params struct {
				Cwd string `json:"cwd"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				return err
			}
			cwd = params.Cwd
			if err := reply(msg.ID, map[string]any{"sessionId": "local-session", "configOptions": options}); err != nil {
				return err
			}
		case "session/set_config_option":
			var params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				return err
			}
			if params.ConfigID != "model" || params.Value != "local-test-model" {
				return fmt.Errorf("unexpected configuration")
			}
			if err := reply(msg.ID, map[string]any{"configOptions": options}); err != nil {
				return err
			}
		case "session/prompt":
			if err := writeApp(filepath.Join(cwd, "app"), "local.personal.stdio", "1.0.0", testCode); err != nil {
				return err
			}
			if mode == "eof" {
				return nil
			}
			if mode == "max_tokens" {
				if err := reply(msg.ID, map[string]string{"stopReason": "max_tokens"}); err != nil {
					return err
				}
				continue
			}
			promptID = msg.ID
			if err := send(map[string]any{"jsonrpc": "2.0", "id": "permission:test", "method": "session/request_permission", "params": map[string]any{"sessionId": "local-session", "toolCall": map[string]string{"toolCallId": "write", "title": "Write generated code", "status": "pending"}, "options": []any{map[string]string{"optionId": "opaque allowed / once", "name": "Allow once", "kind": "allow_once"}, map[string]string{"optionId": "opaque rejected / once", "name": "Reject once", "kind": "reject_once"}}}}); err != nil {
				return err
			}
		case "":
			if string(msg.ID) != `"permission:test"` {
				return fmt.Errorf("unexpected response")
			}
			var selected struct {
				Outcome struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			if err := json.Unmarshal(msg.Result, &selected); err != nil {
				return err
			}
			if mode == "allow" {
				if selected.Outcome.Outcome != "selected" || selected.Outcome.OptionID != "opaque allowed / once" {
					return fmt.Errorf("option id changed")
				}
			} else if selected.Outcome.Outcome != "cancelled" {
				return fmt.Errorf("default permission was allowed")
			}
			if err := reply(promptID, map[string]string{"stopReason": "end_turn"}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected method")
		}
	}
	return scanner.Err()
}

func TestProductionClientUsesRealStdioWithLocalPeer(t *testing.T) {
	for _, mode := range []string{"deny", "allow", "eof", "max_tokens"} {
		t.Run(mode, func(t *testing.T) {
			s, _, m, store := harness(t)
			// 不覆盖新建 Service 的默认工厂，从真正 acp.Start 走完整 stdio 链路。
			s = New(m.manager)
			req := testRequest(t)
			req.Profile.Args = []string{"-test.run=^TestAuthoringPeerProcess$", "--", "lintel-authoring-test-peer", mode}
			req.ConfigValues = map[string]string{"model": "local-test-model"}
			if mode == "allow" {
				req.RequestPermission = func(context.Context, acp.PermissionRequest) (string, error) { return "opaque allowed / once", nil }
			}
			out, err := s.Run(context.Background(), req)
			if mode == "eof" || mode == "max_tokens" {
				if err == nil || len(m.manager.Installations()) != 0 {
					t.Fatalf("未完成仍提议: %+v %v", out, err)
				}
				return
			}
			if err != nil || out.Installation.Status != "pending" || out.Session.ID != "local-session" {
				t.Fatalf("真实 stdio 路径: %+v %v", out, err)
			}
			if installed, _ := store.List(); len(installed) != 0 {
				t.Fatal("stdio 路径跳过了原生确认")
			}
		})
	}
}
