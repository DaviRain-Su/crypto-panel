package acp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// 假 peer 复用测试二进制，绝不启动真实 agent 或访问模型服务。
func TestACPHelperProcess(t *testing.T) {
	if os.Getenv("ACP_TEST_PEER") != "1" {
		return
	}
	if os.Getenv("ACP_TEST_MODE") == "descendant" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if err := runPeer(os.Getenv("ACP_TEST_MODE")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

type peerMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func peerWrite(mode string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if mode == "fragmented" {
		for len(data) > 0 {
			n := min(7, len(data))
			if _, err := os.Stdout.Write(data[:n]); err != nil {
				return err
			}
			data = data[n:]
		}
		return nil
	}
	_, err = os.Stdout.Write(data)
	return err
}

func peerReply(mode string, id json.RawMessage, result any) error {
	return peerWrite(mode, map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func peerError(mode string, id json.RawMessage, code int) error {
	return peerWrite(mode, map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": "fake error"}})
}

func peerUpdate(mode, sessionID, kind, text string) error {
	return peerWrite(mode, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
		"sessionId": sessionID, "update": map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": text}},
	}})
}

func peerReplyWithConfigUpdate(mode string, id json.RawMessage, sessionID string, result any) error {
	response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		return err
	}
	update, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": sessionID, "update": map[string]any{"sessionUpdate": "config_option_update", "configOptions": []any{}}}})
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(append(append(response, '\n'), update...), '\n'))
	return err
}

func peerConfig(current string) []any {
	return []any{
		map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": current, "options": []any{
			map[string]any{"value": "model-a", "name": "A"}, map[string]any{"value": "model-b", "name": "B"},
		}},
		map[string]any{"id": "mode", "name": "Mode", "type": "select", "currentValue": "safe", "options": []any{
			map[string]any{"group": "modes", "name": "Modes", "options": []any{map[string]any{"value": "safe", "name": "Safe"}}},
		}},
	}
}

func runPeer(mode string) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	var active *peerMessage
	var activeSession string
	var reordered []peerMessage
	var permissionCount int
	var sessions int
	complete := func(reason string) error {
		if active == nil {
			return fmt.Errorf("no active prompt")
		}
		err := peerReply(mode, active.ID, map[string]any{"stopReason": reason})
		active = nil
		return err
	}
	for scanner.Scan() {
		var msg peerMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.JSONRPC != "2.0" {
			return fmt.Errorf("invalid client message")
		}
		if msg.Method == "" {
			if active == nil {
				continue
			}
			if mode == "unknown-request" || mode == "bad-permission" {
				want := -32601
				if mode == "bad-permission" {
					want = -32602
				}
				if msg.Error == nil || msg.Error.Code != want || string(msg.ID) != `"peer-request"` {
					return fmt.Errorf("invalid rejection response")
				}
				if err := complete("end_turn"); err != nil {
					return err
				}
				continue
			}
			var result struct {
				Outcome struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			if err := json.Unmarshal(msg.Result, &result); err != nil {
				return err
			}
			wantID := `"permission-1"`
			if mode == "permission-number" {
				wantID = "-7"
			} else if mode == "permission-null" {
				wantID = "null"
			}
			if mode != "permission-concurrent" && string(msg.ID) != wantID {
				return fmt.Errorf("permission request id changed")
			}
			text := result.Outcome.Outcome
			if text == "selected" {
				text += ":" + result.Outcome.OptionID
			}
			if err := peerUpdate(mode, activeSession, "agent_message_chunk", text); err != nil {
				return err
			}
			permissionCount--
			if permissionCount == 0 {
				if err := complete("end_turn"); err != nil {
					return err
				}
			}
			continue
		}
		switch msg.Method {
		case "initialize":
			if mode == "no-handshake" {
				return fmt.Errorf("Start sent initialize")
			}
			if mode == "init-hang" {
				continue
			}
			if mode == "init-eof" {
				return nil
			}
			if strings.HasPrefix(mode, "bad:") {
				var raw []byte
				switch strings.TrimPrefix(mode, "bad:") {
				case "json":
					raw = []byte("not-json\n")
				case "version":
					raw = []byte("{\"id\":1,\"result\":{}}\n")
				case "both":
					raw = []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{},\"error\":{\"code\":-1,\"message\":\"bad\"}}\n")
				case "array":
					raw = []byte("[]\n")
				case "id":
					raw = []byte("{\"jsonrpc\":\"2.0\",\"id\":1.5,\"result\":{}}\n")
				case "duplicate":
					raw = []byte("{\"jsonrpc\":\"2.0\",\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n")
				case "utf8":
					raw = append([]byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"x\":\""), 0xff)
					raw = append(raw, []byte("\"}}\n")...)
				case "oversize":
					raw = []byte(strings.Repeat(" ", maxMessageBytes+1) + "\n")
				case "truncated":
					raw = []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
				case "depth":
					raw = []byte(`{"jsonrpc":"2.0","id":1,"result":` + strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70) + "}\n")
				}
				_, err := os.Stdout.Write(raw)
				return err
			}
			var params struct {
				ProtocolVersion    int `json:"protocolVersion"`
				ClientCapabilities struct {
					Terminal bool `json:"terminal"`
					FS       struct {
						Read  bool `json:"readTextFile"`
						Write bool `json:"writeTextFile"`
					} `json:"fs"`
				} `json:"clientCapabilities"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil || params.ProtocolVersion != 1 || params.ClientCapabilities.Terminal || params.ClientCapabilities.FS.Read || params.ClientCapabilities.FS.Write {
				return fmt.Errorf("invented client capabilities")
			}
			if os.Getenv("ACP_PARENT_SECRET") != "" || os.Getenv("ACP_TEST_EXPLICIT") == "wrong" {
				return fmt.Errorf("parent environment leaked")
			}
			if want := os.Getenv("ACP_EXPECT_CWD"); want != "" {
				cwd, err := os.Getwd()
				if err != nil || cwd != want {
					return fmt.Errorf("wrong process cwd")
				}
			}
			version, infoVersion := 1, "test"
			if mode == "incompatible" {
				version = 2
			}
			if mode == "descendant-parent" {
				child := exec.Command(os.Args[0], "-test.run=^TestACPHelperProcess$")
				child.Env = []string{"ACP_TEST_PEER=1", "ACP_TEST_MODE=descendant", "GORACE=atexit_sleep_ms=0"}
				child.Stdout = os.Stdout
				if err := child.Start(); err != nil {
					return err
				}
				infoVersion = strconv.Itoa(child.Process.Pid)
				go child.Wait()
			}
			if mode == "id-type" {
				if err := peerReply(mode, json.RawMessage(strconv.Quote(string(msg.ID))), map[string]any{"protocolVersion": 9}); err != nil {
					return err
				}
			}
			if err := peerReply(mode, msg.ID, map[string]any{
				"protocolVersion": version, "agentInfo": map[string]any{"name": "fake", "version": infoVersion},
				"agentCapabilities": map[string]any{"loadSession": true, "promptCapabilities": map[string]any{"image": true}, "sessionCapabilities": map[string]any{"list": map[string]any{}}},
				"authMethods":       []any{map[string]any{"id": "external", "name": "External login"}},
			}); err != nil {
				return err
			}
		case "session/new":
			var params struct {
				Cwd        string `json:"cwd"`
				MCPServers []any  `json:"mcpServers"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil || !filepath.IsAbs(params.Cwd) || params.MCPServers == nil || len(params.MCPServers) != 0 {
				return fmt.Errorf("invalid new session")
			}
			if mode == "auth" {
				if err := peerError(mode, msg.ID, -32000); err != nil {
					return err
				}
				continue
			}
			if mode == "reordered" {
				reordered = append(reordered, msg)
				if len(reordered) == 2 {
					for i := 1; i >= 0; i-- {
						var p struct {
							Cwd string `json:"cwd"`
						}
						json.Unmarshal(reordered[i].Params, &p)
						if err := peerReply(mode, reordered[i].ID, map[string]any{"sessionId": p.Cwd}); err != nil {
							return err
						}
					}
					reordered = nil
				}
				continue
			}
			sessions++
			sessionID := fmt.Sprintf("s-%d", sessions)
			if mode == "config-new-overflow" {
				if err := peerReply(mode, msg.ID, map[string]any{"sessionId": sessionID, "configOptions": excessiveConfigOptions()}); err != nil {
					return err
				}
				continue
			}
			if mode == "new-config-race" {
				options := peerConfig("model-a")
				options[0].(map[string]any)["_meta"] = map[string]any{"padding": strings.Repeat("m", 1<<20)}
				if err := peerReplyWithConfigUpdate(mode, msg.ID, sessionID, map[string]any{"sessionId": sessionID, "configOptions": options}); err != nil {
					return err
				}
				continue
			}
			if err := peerReply(mode, msg.ID, map[string]any{"sessionId": sessionID, "configOptions": peerConfig("model-a")}); err != nil {
				return err
			}
			if mode == "stop-reading" {
				for {
					time.Sleep(time.Hour)
				}
			}
		case "session/set_config_option":
			var p struct {
				SessionID string `json:"sessionId"`
				ConfigID  string `json:"configId"`
				Value     string `json:"value"`
			}
			json.Unmarshal(msg.Params, &p)
			if active != nil || (p.ConfigID == "model" && p.Value != "model-a" && p.Value != "model-b") || (p.ConfigID == "mode" && p.Value != "safe") {
				return fmt.Errorf("unadvertised or overlapping config")
			}
			if mode == "config-set-overflow" {
				if err := peerReply(mode, msg.ID, map[string]any{"configOptions": excessiveConfigOptions()}); err != nil {
					return err
				}
				continue
			}
			if mode == "set-config-race" {
				options := peerConfig(p.Value)
				options[0].(map[string]any)["_meta"] = map[string]any{"padding": strings.Repeat("m", 1<<20)}
				if err := peerReplyWithConfigUpdate(mode, msg.ID, p.SessionID, map[string]any{"configOptions": options}); err != nil {
					return err
				}
				continue
			}
			if err := peerReply(mode, msg.ID, map[string]any{"configOptions": peerConfig(p.Value)}); err != nil {
				return err
			}
		case "session/prompt":
			if active != nil {
				return fmt.Errorf("overlapping prompt")
			}
			var p struct {
				SessionID string `json:"sessionId"`
				Prompt    []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"prompt"`
			}
			if err := json.Unmarshal(msg.Params, &p); err != nil || len(p.Prompt) != 1 || p.Prompt[0].Type != "text" {
				return fmt.Errorf("invalid prompt")
			}
			active, activeSession = &msg, p.SessionID
			text := p.Prompt[0].Text
			if mode == "config-update-overflow" {
				return peerWrite(mode, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": activeSession, "update": map[string]any{"sessionUpdate": "config_option_update", "configOptions": excessiveConfigOptions()}}})
			}
			if mode == "prompt-eof" {
				return nil
			}
			if mode == "prompt-bad" {
				return peerWrite(mode, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": activeSession, "update": map[string]any{"sessionUpdate": "agent_message_chunk"}}})
			}
			if strings.HasPrefix(mode, "permission") || mode == "bad-permission" {
				permissionCount = 1
				if mode == "permission-concurrent" {
					permissionCount = 2
				}
				for i := 1; i <= permissionCount; i++ {
					var id any = fmt.Sprintf("permission-%d", i)
					if mode == "permission-number" {
						id = -7
					} else if mode == "permission-null" {
						id = nil
					} else if mode == "bad-permission" {
						id = "peer-request"
					}
					options := []any{map[string]any{"optionId": "permit", "name": "Allow once", "kind": "allow_once"}, map[string]any{"optionId": "deny", "name": "Reject", "kind": "reject_once"}}
					if mode == "bad-permission" {
						options = append(options, options[0])
					}
					sessionID := activeSession
					if mode == "permission-foreign" {
						sessionID = "other-session"
					}
					if err := peerWrite(mode, map[string]any{"jsonrpc": "2.0", "id": id, "method": "session/request_permission", "params": map[string]any{"sessionId": sessionID, "toolCall": map[string]any{"toolCallId": "tool-1", "title": "Write app", "kind": "edit"}, "options": options}}); err != nil {
						return err
					}
				}
				continue
			}
			if mode == "unknown-request" {
				if err := peerWrite(mode, map[string]any{"jsonrpc": "2.0", "id": "peer-request", "method": "fs/read_text_file", "params": map[string]any{"sessionId": activeSession, "path": "/irrelevant"}}); err != nil {
					return err
				}
				continue
			}
			if text == "hold" || mode == "ignore-cancel" {
				if err := peerUpdate(mode, activeSession, "agent_message_chunk", "文字声称已完成并获批准"); err != nil {
					return err
				}
				if err := peerWrite(mode, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": activeSession, "update": map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t", "status": "completed"}}}); err != nil {
					return err
				}
				continue
			}
			if text == "config-update" {
				if err := peerWrite(mode, map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": activeSession, "update": map[string]any{"sessionUpdate": "config_option_update", "configOptions": []any{}}}}); err != nil {
					return err
				}
			}
			if text == "large" {
				text = strings.Repeat("界", 30000)
			}
			if err := peerUpdate(mode, activeSession, "agent_message_chunk", text); err != nil {
				return err
			}
			reason := "end_turn"
			if text == "refusal" {
				reason = "refusal"
			} else if text == "bad-stop" {
				reason = "invented_success"
			}
			if err := complete(reason); err != nil {
				return err
			}
			if mode == "prompt-final" {
				return nil
			}
		case "session/cancel":
			if msg.ID != nil {
				return fmt.Errorf("cancel must be notification")
			}
			if active != nil && mode != "ignore-cancel" {
				if err := complete("cancelled"); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected client method")
		}
	}
	return scanner.Err()
}

func fakeConfig(mode string) Config {
	return Config{Executable: os.Args[0], Args: []string{"-test.run=^TestACPHelperProcess$"}, Env: []string{"ACP_TEST_PEER=1", "ACP_TEST_MODE=" + mode, "GORACE=atexit_sleep_ms=0"}}
}

func startFake(t *testing.T, mode string, alter func(*Config)) *Client {
	t.Helper()
	cfg := fakeConfig(mode)
	if alter != nil {
		alter(&cfg)
	}
	c, err := startClient(context.Background(), cfg, timeouts{handshake: time.Second, prompt: 3 * time.Second, cancel: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func readyFake(t *testing.T, mode string, alter func(*Config)) (*Client, Session) {
	t.Helper()
	c := startFake(t, mode, alter)
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := c.NewSession(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

func receiveUpdate(t *testing.T, updates <-chan Update) Update {
	t.Helper()
	select {
	case u := <-updates:
		return u
	case <-time.After(2 * time.Second):
		t.Fatal("update timeout")
		return Update{}
	}
}

func TestStartOnlyAndEnvironment(t *testing.T) {
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACP_PARENT_SECRET", hex.EncodeToString(secret))
	c := startFake(t, "no-handshake", nil)
	select {
	case <-c.Done():
		t.Fatal("Start unexpectedly handshook")
	case <-time.After(40 * time.Millisecond):
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c = startFake(t, "normal", func(cfg *Config) {
		cfg.Cwd = cwd
		cfg.Env = append(cfg.Env, "ACP_TEST_EXPLICIT=set", "ACP_EXPECT_CWD="+cwd)
	})
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), Config{Executable: filepath.Join(t.TempDir(), "missing")}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing executable: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, fakeConfig("normal")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled Start: %v", err)
	}
}

func TestInitializeSessionAndFragments(t *testing.T) {
	for _, mode := range []string{"normal", "fragmented", "id-type"} {
		t.Run(mode, func(t *testing.T) {
			updates := make(chan Update, 8)
			c, s := readyFake(t, mode, func(cfg *Config) { cfg.OnUpdate = func(u Update) { updates <- u } })
			init, err := c.Initialize(context.Background())
			if err != nil || init.ProtocolVersion != 1 || init.AgentInfo == nil || init.AgentInfo.Name != "fake" || !init.AgentCapabilities.LoadSession || !init.AgentCapabilities.PromptCapabilities.Image || !json.Valid(init.AgentCapabilities.Raw) || !json.Valid(init.AuthMethods) {
				t.Fatalf("initialization: %+v %v", init, err)
			}
			init.AgentInfo.Name = "mutated"
			again, _ := c.Initialize(context.Background())
			if again.AgentInfo.Name != "fake" || s.ID == "" || len(s.ConfigOptions) != 2 {
				t.Fatalf("cache isolation/session: %+v %+v", again, s)
			}
			result, err := c.Prompt(context.Background(), s.ID, "你好\n第二行")
			if err != nil || result.StopReason != "end_turn" {
				t.Fatalf("prompt: %+v %v", result, err)
			}
			u := receiveUpdate(t, updates)
			if u.Type != "agent_message_chunk" || u.Text != "你好\n第二行" || u.SessionID != s.ID || !json.Valid(u.Raw) {
				t.Fatalf("update: %+v", u)
			}
			if _, err := c.Prompt(context.Background(), s.ID, "large"); err != nil {
				t.Fatal(err)
			}
			if got := receiveUpdate(t, updates).Text; len(got) != 90000 {
				t.Fatalf("large text bytes: %d", len(got))
			}
			if result, err := c.Prompt(context.Background(), s.ID, "refusal"); err != nil || result.StopReason != "refusal" {
				t.Fatalf("refusal: %+v %v", result, err)
			}
		})
	}
}

func TestSessionPreconditionsAndAuth(t *testing.T) {
	c := startFake(t, "normal", nil)
	if _, err := c.NewSession(context.Background(), t.TempDir()); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("uninitialized: %v", err)
	}
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{"", "relative"} {
		if _, err := c.NewSession(context.Background(), cwd); err == nil {
			t.Fatal("relative/empty cwd accepted")
		}
	}
	if _, err := c.Prompt(context.Background(), "unknown", "x"); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("unknown session: %v", err)
	}
	if err := c.Cancel(context.Background(), "unknown"); !errors.Is(err, ErrUnknownSession) {
		t.Fatalf("unknown cancel: %v", err)
	}
	c = startFake(t, "auth", nil)
	c.Initialize(context.Background())
	_, err := c.NewSession(context.Background(), t.TempDir())
	var remote *RPCError
	if !errors.As(err, &remote) || remote.Code != -32000 || strings.Contains(err.Error(), "fake error") {
		t.Fatalf("auth RPC error: %#v %v", remote, err)
	}
}

func TestReorderedResponses(t *testing.T) {
	c := startFake(t, "reordered", nil)
	if _, err := c.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, name := range []string{"alpha", "beta"} {
		cwd := filepath.Join(t.TempDir(), name)
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := c.NewSession(context.Background(), cwd)
			if err != nil || s.ID != cwd {
				t.Errorf("response mismatch: %q %v", s.ID, err)
			}
		}()
	}
	wg.Wait()
}

func TestQuietPromptSerializationAndQueuedCancellation(t *testing.T) {
	updates := make(chan Update, 16)
	c, s := readyFake(t, "normal", func(cfg *Config) { cfg.OnUpdate = func(u Update) { updates <- u } })
	s2, err := c.NewSession(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result PromptResult
		err    error
	}
	first := make(chan outcome, 1)
	go func() { r, err := c.Prompt(context.Background(), s.ID, "hold"); first <- outcome{r, err} }()
	receiveUpdate(t, updates)
	receiveUpdate(t, updates)
	select {
	case got := <-first:
		t.Fatalf("quiet/tool completion finished prompt: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
	queuedCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Prompt(queuedCtx, s2.ID, "must-not-send"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued cancellation: %v", err)
	}
	second := make(chan outcome, 1)
	go func() { r, err := c.Prompt(context.Background(), s2.ID, "second"); second <- outcome{r, err} }()
	if err := c.Cancel(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	if got := <-first; got.err != nil || got.result.StopReason != "cancelled" {
		t.Fatalf("first: %+v", got)
	}
	if got := <-second; got.err != nil || got.result.StopReason != "end_turn" {
		t.Fatalf("second: %+v", got)
	}
	if got := receiveUpdate(t, updates); got.Text != "second" {
		t.Fatalf("queued request was sent: %+v", got)
	}
}

func TestPermissionDefaultsAndExactOptions(t *testing.T) {
	cases := []struct {
		name string
		mode string
		cb   func(context.Context, PermissionRequest) (string, error)
		want string
	}{
		{"default", "permission", nil, "cancelled"},
		{"integer id", "permission-number", nil, "cancelled"},
		{"null id", "permission-null", nil, "cancelled"},
		{"allow", "permission", func(_ context.Context, p PermissionRequest) (string, error) { return p.Options[0].OptionID, nil }, "selected:permit"},
		{"deny", "permission", func(_ context.Context, p PermissionRequest) (string, error) { return p.Options[1].OptionID, nil }, "selected:deny"},
		{"invented", "permission", func(context.Context, PermissionRequest) (string, error) { return "invented", nil }, "cancelled"},
		{"closed", "permission", func(context.Context, PermissionRequest) (string, error) { return "", nil }, "cancelled"},
		{"error", "permission", func(context.Context, PermissionRequest) (string, error) { return "permit", errors.New("closed UI") }, "cancelled"},
		{"mutated", "permission", func(_ context.Context, p PermissionRequest) (string, error) {
			p.Options[0].OptionID = "invented"
			return "invented", nil
		}, "cancelled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updates := make(chan Update, 4)
			c, s := readyFake(t, tc.mode, func(cfg *Config) { cfg.RequestPermission = tc.cb; cfg.OnUpdate = func(u Update) { updates <- u } })
			if _, err := c.Prompt(context.Background(), s.ID, "permission"); err != nil {
				t.Fatal(err)
			}
			if got := receiveUpdate(t, updates).Text; got != tc.want {
				t.Fatalf("permission result: %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCancelledTurnDoesNotSelectPermission(t *testing.T) {
	updates := make(chan Update, 4)
	c, s := readyFake(t, "permission", func(cfg *Config) {
		cfg.OnUpdate = func(u Update) { updates <- u }
		cfg.RequestPermission = func(context.Context, PermissionRequest) (string, error) {
			return "permit", nil
		}
	})
	permissionBeforeAccept = func(c *Client) {
		c.mu.Lock()
		turn := c.active
		c.mu.Unlock()
		if turn != nil {
			turn.permissionCancel()
		}
	}
	t.Cleanup(func() { permissionBeforeAccept = nil })
	if _, err := c.Prompt(context.Background(), s.ID, "permission"); err != nil {
		t.Fatal(err)
	}
	if got := receiveUpdate(t, updates).Text; got != "cancelled" {
		t.Fatalf("cancelled turn returned %s", got)
	}
}

func TestPermissionFromInactiveTurnIsRejected(t *testing.T) {
	updates := make(chan Update, 4)
	called := make(chan struct{}, 1)
	c, s := readyFake(t, "permission-foreign", func(cfg *Config) {
		cfg.OnUpdate = func(u Update) { updates <- u }
		cfg.RequestPermission = func(context.Context, PermissionRequest) (string, error) {
			called <- struct{}{}
			return "permit", nil
		}
	})
	if _, err := c.Prompt(context.Background(), s.ID, "permission"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
		t.Fatal("permission prompt from a non-active turn was offered")
	default:
	}
	if got := receiveUpdate(t, updates).Text; got != "cancelled" {
		t.Fatalf("non-active turn returned %s", got)
	}
}

func TestPermissionCancelAndConcurrency(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	c, s := readyFake(t, "permission", func(cfg *Config) {
		cfg.RequestPermission = func(ctx context.Context, p PermissionRequest) (string, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			return "permit", nil
		}
	})
	prompt := make(chan error, 1)
	go func() { _, err := c.Prompt(context.Background(), s.ID, "permission"); prompt <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("permission callback did not start")
	}
	if err := c.Cancel(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	if err := <-prompt; err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("permission callback context not cancelled")
	}
	updates := make(chan Update, 4)
	release := make(chan struct{})
	c, s = readyFake(t, "permission-concurrent", func(cfg *Config) {
		cfg.OnUpdate = func(u Update) { updates <- u }
		cfg.RequestPermission = func(ctx context.Context, p PermissionRequest) (string, error) {
			select {
			case <-release:
				return "deny", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	})
	go func() { _, err := c.Prompt(context.Background(), s.ID, "permission"); prompt <- err }()
	if got := receiveUpdate(t, updates).Text; got != "cancelled" {
		t.Fatalf("concurrent permission not bounded: %s", got)
	}
	close(release)
	if err := <-prompt; err != nil {
		t.Fatal(err)
	}
	if got := receiveUpdate(t, updates).Text; got != "selected:deny" {
		t.Fatalf("first permission result: %s", got)
	}
}

func TestUnsupportedRequestsAndMalformedPermissions(t *testing.T) {
	for _, mode := range []string{"unknown-request", "bad-permission"} {
		t.Run(mode, func(t *testing.T) {
			c, s := readyFake(t, mode, func(cfg *Config) {
				cfg.RequestPermission = func(context.Context, PermissionRequest) (string, error) {
					t.Error("callback called for invalid request")
					return "permit", nil
				}
			})
			if _, err := c.Prompt(context.Background(), s.ID, "x"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfigDiscoveryAndChanges(t *testing.T) {
	c, s := readyFake(t, "normal", nil)
	if len(s.ConfigOptions[1].Groups) != 1 || len(s.ConfigOptions[1].Options) != 1 {
		t.Fatalf("grouped config not exposed: %+v", s.ConfigOptions)
	}
	s.ConfigOptions[0].Options[0].Value = "invented"
	for _, args := range [][2]string{{"invented", "model-a"}, {"model", "invented"}} {
		if _, err := c.SetConfigOption(context.Background(), s.ID, args[0], args[1]); !errors.Is(err, ErrUnsupportedConfig) {
			t.Fatalf("unsupported config: %v", err)
		}
	}
	changed, err := c.SetConfigOption(context.Background(), s.ID, "model", "model-b")
	if err != nil || changed.ConfigOptions[0].CurrentValue != "model-b" {
		t.Fatalf("config change: %+v %v", changed, err)
	}
	if _, err := c.SetConfigOption(context.Background(), s.ID, "mode", "safe"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prompt(context.Background(), s.ID, "config-update"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetConfigOption(context.Background(), s.ID, "model", "model-a"); !errors.Is(err, ErrUnsupportedConfig) {
		t.Fatalf("stale config accepted: %v", err)
	}
}

func TestConfigNotificationsAfterResponseRemainAuthoritative(t *testing.T) {
	for _, mode := range []string{"new-config-race", "set-config-race"} {
		t.Run(mode, func(t *testing.T) {
			updates := make(chan Update, 4)
			c, s := readyFake(t, mode, func(cfg *Config) { cfg.OnUpdate = func(u Update) { updates <- u } })
			if mode == "set-config-race" {
				if _, err := c.SetConfigOption(context.Background(), s.ID, "model", "model-b"); err != nil {
					t.Fatal(err)
				}
			}
			if u := receiveUpdate(t, updates); u.Type != "config_option_update" {
				t.Fatalf("unexpected update: %+v", u)
			}
			if _, err := c.SetConfigOption(context.Background(), s.ID, "model", "model-a"); !errors.Is(err, ErrUnsupportedConfig) {
				t.Fatalf("old response overwrote newer config notification: %v", err)
			}
		})
	}
}

func TestBlockedStdinTimeoutStillReapsOwnedProcess(t *testing.T) {
	c, s := readyFake(t, "stop-reading", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := c.Prompt(ctx, s.ID, strings.Repeat("x", 2<<20))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked pipe did not respect timeout: %v", err)
	}
	if err := c.Close(); err != nil || c.cmd.ProcessState == nil {
		t.Fatalf("blocked writer prevented process cleanup: %v", err)
	}
}

func TestEOFMalformedAndVersion(t *testing.T) {
	for _, kind := range []string{"json", "version", "both", "array", "id", "duplicate", "utf8", "oversize", "truncated", "depth"} {
		t.Run(kind, func(t *testing.T) {
			c := startFake(t, "bad:"+kind, nil)
			if _, err := c.Initialize(context.Background()); !errors.Is(err, ErrProtocol) {
				t.Fatalf("malformed message: %v", err)
			}
		})
	}
	c := startFake(t, "init-eof", nil)
	if _, err := c.Initialize(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF: %v", err)
	}
	c = startFake(t, "incompatible", nil)
	if _, err := c.Initialize(context.Background()); !errors.Is(err, ErrIncompatibleProtocol) {
		t.Fatalf("version: %v", err)
	}
	for _, mode := range []string{"prompt-eof", "prompt-bad"} {
		c, s := readyFake(t, mode, nil)
		_, err := c.Prompt(context.Background(), s.ID, "x")
		want := io.EOF
		if mode == "prompt-bad" {
			want = ErrProtocol
		}
		if !errors.Is(err, want) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	c, s := readyFake(t, "normal", nil)
	if _, err := c.Prompt(context.Background(), s.ID, "bad-stop"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("invalid stop reason: %v", err)
	}
}

func TestFinalResponseBeforeEOFIsDelivered(t *testing.T) {
	c, s := readyFake(t, "prompt-final", nil)
	result, err := c.Prompt(context.Background(), s.ID, "last response")
	if err != nil || result.StopReason != "end_turn" {
		t.Fatalf("final response lost to EOF: %+v %v", result, err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("EOF did not terminate connection")
	}
	c.Close()
}

func TestTimeoutAndUnresponsiveCancellation(t *testing.T) {
	c := startFake(t, "init-hang", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := c.Initialize(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handshake timeout: %v", err)
	}
	if err := c.Close(); err != nil || c.cmd.ProcessState == nil {
		t.Fatalf("handshake process not reaped: %v", err)
	}
	c, s := readyFake(t, "normal", nil)
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Prompt(ctx, s.ID, "hold"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("prompt timeout: %v", err)
	}
	if _, err := c.Prompt(context.Background(), s.ID, "still usable"); err != nil {
		t.Fatalf("graceful cancellation lost client: %v", err)
	}
	updates := make(chan Update, 4)
	c, s = readyFake(t, "ignore-cancel", func(cfg *Config) { cfg.OnUpdate = func(u Update) { updates <- u } })
	finished := make(chan error, 1)
	go func() { _, err := c.Prompt(context.Background(), s.ID, "hold"); finished <- err }()
	receiveUpdate(t, updates)
	if err := c.Cancel(context.Background(), s.ID); !errors.Is(err, ErrCancelTimeout) {
		t.Fatalf("cancel timeout: %v", err)
	}
	if err := <-finished; !errors.Is(err, ErrCancelTimeout) {
		t.Fatalf("pending prompt: %v", err)
	}
	c.Close()
	if c.cmd.ProcessState == nil {
		t.Fatal("unresponsive child not reaped")
	}
}

func TestCloseAndLifetimeContext(t *testing.T) {
	updates := make(chan Update, 8)
	c, s := readyFake(t, "normal", func(cfg *Config) { cfg.OnUpdate = func(u Update) { updates <- u } })
	finished := make(chan PromptResult, 1)
	go func() { r, _ := c.Prompt(context.Background(), s.ID, "hold"); finished <- r }()
	receiveUpdate(t, updates)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if r := <-finished; r.StopReason != "cancelled" {
		t.Fatalf("Close skipped graceful cancellation: %+v", r)
	}
	if err := c.Close(); err != nil || c.cmd.ProcessState == nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if _, err := c.Initialize(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed client accepted initialize: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c, err := startClient(ctx, fakeConfig("normal"), timeouts{handshake: time.Second, prompt: time.Second, cancel: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Start context did not close process")
	}
	c.Close()
	if !errors.Is(c.Err(), context.Canceled) || c.cmd.ProcessState == nil {
		t.Fatalf("lifetime context: %v", c.Err())
	}
}
