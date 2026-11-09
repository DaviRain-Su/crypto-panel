package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type timeouts struct {
	handshake time.Duration
	prompt    time.Duration
	cancel    time.Duration
}

type turn struct {
	sessionID        string
	request          *pendingRequest
	sent             chan struct{}
	permissionCtx    context.Context
	permissionCancel context.CancelFunc
	cancelRequested  bool
	cancelOnce       sync.Once
	cancelDone       chan struct{}
	cancelErr        error
}

// Client 拥有自己启动的进程；不同方法可并发调用，生成始终串行。
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cfg    Config
	limits timeouts

	ctx         context.Context
	stopContext context.CancelFunc
	done        chan struct{}
	readDone    chan struct{}
	processDone chan struct{}
	closeDone   chan struct{}
	writes      chan outbound
	updates     chan Update
	initGate    chan struct{}
	promptGate  chan struct{}
	permissions chan struct{}

	mu             sync.Mutex
	terminal       error
	closing        bool
	initialized    bool
	initialization Initialization
	nextID         int64
	pending        map[string]*pendingRequest
	sessions       map[string]Session
	inbound        map[string]bool
	active         *turn
	failOnce       sync.Once
	closeOnce      sync.Once
}

// Start 只启动显式配置的本机程序；ctx 管理整个连接生命周期。
func Start(ctx context.Context, cfg Config) (*Client, error) {
	return startClient(ctx, cfg, timeouts{handshake: HandshakeTimeout, prompt: PromptTimeout, cancel: CancelGracePeriod})
}

func startClient(ctx context.Context, cfg Config, limits timeouts) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Executable == "" {
		return nil, errors.New("acp: executable is required")
	}
	if cfg.Cwd != "" && !filepath.IsAbs(cfg.Cwd) {
		return nil, errors.New("acp: process cwd must be absolute")
	}
	cfg.Args = append([]string(nil), cfg.Args...)
	// 非 nil 的空环境防止 os/exec 隐式继承宿主秘密。
	cfg.Env = append([]string{}, cfg.Env...)
	cmd := exec.Command(cfg.Executable, cfg.Args...)
	cmd.Env, cmd.Dir = cfg.Env, cfg.Cwd
	configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("acp: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("acp: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("acp: start process: %w", err)
	}
	life, stop := context.WithCancel(context.Background())
	c := &Client{
		cmd: cmd, stdin: stdin, stdout: stdout, cfg: cfg, limits: limits, ctx: life, stopContext: stop,
		done: make(chan struct{}), readDone: make(chan struct{}), processDone: make(chan struct{}), closeDone: make(chan struct{}),
		writes: make(chan outbound, maxWriteQueue), updates: make(chan Update, maxUpdateQueue),
		initGate: make(chan struct{}, 1), promptGate: make(chan struct{}, 1), permissions: make(chan struct{}, 1),
		pending: make(map[string]*pendingRequest), sessions: make(map[string]Session), inbound: make(map[string]bool),
	}
	go c.readLoop()
	go c.writeLoop()
	if cfg.OnUpdate != nil {
		go c.updateLoop()
	}
	go func() {
		// 先读完 stdout，避免 Wait 提前关闭最后一条响应所在的管道。
		<-c.readDone
		_ = cmd.Wait()
		close(c.processDone)
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.closeWithReason(ctx.Err())
		case <-c.done:
		}
	}()
	return c, nil
}

func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminal
}

func (c *Client) stateErrorLocked(requireReady bool) error {
	if c.terminal != nil {
		return c.terminal
	}
	if c.closing {
		return ErrClosed
	}
	if requireReady && !c.initialized {
		return ErrNotInitialized
	}
	return nil
}

func (c *Client) checkReady() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateErrorLocked(true)
}

func (c *Client) acquire(ctx context.Context, gate chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-gate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
}

func (c *Client) prepare(method string, params map[string]any) (*pendingRequest, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.stateErrorLocked(false); err != nil {
		return nil, nil, err
	}
	c.nextID++
	id := c.nextID
	key := fmt.Sprintf("n:%d", id)
	data, err := encodeMessage(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, nil, err
	}
	sessionID, _ := params["sessionId"].(string)
	p := &pendingRequest{idKey: key, method: method, sessionID: sessionID, reply: make(chan reply, 1), done: make(chan struct{})}
	c.pending[key] = p
	return p, data, nil
}

func (c *Client) forget(p *pendingRequest) {
	c.mu.Lock()
	delete(c.pending, p.idKey)
	c.mu.Unlock()
}

func (c *Client) await(ctx context.Context, p *pendingRequest) (json.RawMessage, error) {
	// 进程紧随最后响应退出时，优先交付已经收到的响应。
	select {
	case r := <-p.reply:
		return r.result, r.err
	default:
	}
	select {
	case r := <-p.reply:
		return r.result, r.err
	case <-ctx.Done():
		select {
		case r := <-p.reply:
			return r.result, r.err
		default:
			return nil, ctx.Err()
		}
	case <-c.done:
		select {
		case r := <-p.reply:
			return r.result, r.err
		default:
			return nil, c.Err()
		}
	}
}

func (c *Client) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	p, data, err := c.prepare(method, params)
	if err != nil {
		return nil, err
	}
	defer c.forget(p)
	if err := c.send(ctx, data); err != nil {
		select {
		case r := <-p.reply:
			return r.result, r.err
		default:
			return nil, err
		}
	}
	return c.await(ctx, p)
}

func decodeObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, ErrProtocol
	}
	return fields, nil
}

// applyResponse 在交付响应之前按线序更新状态，避免调用方覆盖后来的配置通知。
func (c *Client) applyResponse(p *pendingRequest, raw json.RawMessage) error {
	if p.method != "session/new" && p.method != "session/set_config_option" {
		return nil
	}
	fields, err := decodeObject(raw)
	if err != nil {
		return err
	}
	id := p.sessionID
	if p.method == "session/new" {
		if json.Unmarshal(fields["sessionId"], &id) != nil || id == "" {
			return ErrProtocol
		}
	} else if fields["configOptions"] == nil || string(fields["configOptions"]) == "null" {
		return ErrProtocol
	}
	options, err := parseConfigOptions(fields["configOptions"])
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal != nil {
		return c.terminal
	}
	_, exists := c.sessions[id]
	if (p.method == "session/new" && exists) || (p.method != "session/new" && !exists) {
		return ErrProtocol
	}
	c.sessions[id] = Session{ID: id, ConfigOptions: options}
	return nil
}

// Initialize 只公布实际实现的能力；不请求文件、终端或 MCP 宿主权限。
func (c *Client) Initialize(ctx context.Context) (Initialization, error) {
	ctx, cancel := context.WithTimeout(ctx, c.limits.handshake)
	defer cancel()
	if err := c.acquire(ctx, c.initGate); err != nil {
		return Initialization{}, err
	}
	defer func() { <-c.initGate }()
	c.mu.Lock()
	err := c.stateErrorLocked(false)
	cached, ready := cloneInitialization(c.initialization), c.initialized
	c.mu.Unlock()
	if err != nil {
		return Initialization{}, err
	}
	if ready {
		return cached, nil
	}
	raw, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion":    ProtocolVersion,
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
		"clientInfo":         Implementation{Name: "lintel", Version: "1"},
	})
	if err != nil {
		c.fail(err)
		return Initialization{}, err
	}
	fields, err := decodeObject(raw)
	var init Initialization
	if err != nil || fields["protocolVersion"] == nil || string(fields["protocolVersion"]) == "null" || json.Unmarshal(raw, &init) != nil {
		c.fail(ErrProtocol)
		return Initialization{}, ErrProtocol
	}
	if init.ProtocolVersion != ProtocolVersion {
		c.fail(ErrIncompatibleProtocol)
		return Initialization{}, ErrIncompatibleProtocol
	}
	if init.AgentInfo != nil && (init.AgentInfo.Name == "" || init.AgentInfo.Version == "") {
		c.fail(ErrProtocol)
		return Initialization{}, ErrProtocol
	}
	init.AgentCapabilities.Raw = cloneJSON(fields["agentCapabilities"])
	if len(init.AgentCapabilities.Raw) == 0 {
		init.AgentCapabilities.Raw = json.RawMessage(`{}`)
	}
	c.mu.Lock()
	if err := c.stateErrorLocked(false); err != nil {
		c.mu.Unlock()
		return Initialization{}, err
	}
	c.initialized, c.initialization = true, cloneInitialization(init)
	c.mu.Unlock()
	return init, nil
}

func (c *Client) NewSession(ctx context.Context, cwd string) (Session, error) {
	if err := c.checkReady(); err != nil {
		return Session{}, err
	}
	if !filepath.IsAbs(cwd) {
		return Session{}, errors.New("acp: session cwd must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, c.limits.handshake)
	defer cancel()
	raw, err := c.call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		if ctx.Err() != nil {
			c.fail(ctx.Err())
		}
		return Session{}, err
	}
	var response struct {
		ID string `json:"sessionId"`
	}
	if json.Unmarshal(raw, &response) != nil {
		c.fail(ErrProtocol)
		return Session{}, ErrProtocol
	}
	c.mu.Lock()
	s := cloneSession(c.sessions[response.ID])
	c.mu.Unlock()
	return s, nil
}

func (c *Client) sessionLocked(id string) (Session, error) {
	if err := c.stateErrorLocked(true); err != nil {
		return Session{}, err
	}
	s, exists := c.sessions[id]
	if !exists {
		return Session{}, ErrUnknownSession
	}
	return s, nil
}

func (c *Client) SetConfigOption(ctx context.Context, sessionID, configID, value string) (Session, error) {
	if err := c.checkReady(); err != nil {
		return Session{}, err
	}
	if err := c.acquire(ctx, c.promptGate); err != nil {
		return Session{}, err
	}
	defer func() { <-c.promptGate }()
	c.mu.Lock()
	s, err := c.sessionLocked(sessionID)
	c.mu.Unlock()
	if err != nil {
		return Session{}, err
	}
	allowed := false
	for _, option := range s.ConfigOptions {
		if option.ID == configID && option.Type == "select" {
			for _, v := range option.Options {
				allowed = allowed || v.Value == value
			}
		}
	}
	if !allowed {
		return Session{}, ErrUnsupportedConfig
	}
	ctx, cancel := context.WithTimeout(ctx, c.limits.handshake)
	defer cancel()
	_, err = c.call(ctx, "session/set_config_option", map[string]any{"sessionId": sessionID, "configId": configID, "value": value})
	if err != nil {
		if ctx.Err() != nil {
			c.fail(ctx.Err())
		}
		return Session{}, err
	}
	c.mu.Lock()
	s = cloneSession(c.sessions[sessionID])
	c.mu.Unlock()
	return s, nil
}

// Prompt 的静默或工具更新不代表完成，只有匹配的标准响应可结束任务。
func (c *Client) Prompt(ctx context.Context, sessionID, text string) (PromptResult, error) {
	if err := c.checkReady(); err != nil {
		return PromptResult{}, err
	}
	if err := c.acquire(ctx, c.promptGate); err != nil {
		return PromptResult{}, err
	}
	defer func() { <-c.promptGate }()
	c.mu.Lock()
	_, err := c.sessionLocked(sessionID)
	c.mu.Unlock()
	if err != nil {
		return PromptResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.limits.prompt)
	defer cancel()
	p, data, err := c.prepare("session/prompt", map[string]any{"sessionId": sessionID, "prompt": []any{map[string]string{"type": "text", "text": text}}})
	if err != nil {
		return PromptResult{}, err
	}
	defer c.forget(p)
	permissionCtx, permissionCancel := context.WithCancel(ctx)
	t := &turn{sessionID: sessionID, request: p, sent: make(chan struct{}), permissionCtx: permissionCtx, permissionCancel: permissionCancel, cancelDone: make(chan struct{})}
	c.mu.Lock()
	if err := c.stateErrorLocked(true); err != nil {
		c.mu.Unlock()
		permissionCancel()
		return PromptResult{}, err
	}
	c.active = t
	c.mu.Unlock()
	defer func() {
		permissionCancel()
		c.mu.Lock()
		if c.active == t {
			c.active = nil
		}
		c.mu.Unlock()
	}()
	err = c.send(ctx, data)
	close(t.sent)
	var raw json.RawMessage
	if err == nil {
		raw, err = c.await(ctx, p)
	} else {
		select {
		case r := <-p.reply:
			raw, err = r.result, r.err
		default:
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			c.beginCancel(t)
			<-t.cancelDone
			return PromptResult{}, ctx.Err()
		}
		return PromptResult{}, err
	}
	var result PromptResult
	if json.Unmarshal(raw, &result) != nil {
		c.fail(ErrProtocol)
		return PromptResult{}, ErrProtocol
	}
	switch result.StopReason {
	case "end_turn", "max_tokens", "max_turn_requests", "refusal", "cancelled":
		return result, nil
	default:
		c.fail(ErrProtocol)
		return PromptResult{}, ErrProtocol
	}
}

func (c *Client) beginCancel(t *turn) {
	t.cancelOnce.Do(func() {
		c.mu.Lock()
		t.cancelRequested = true
		t.permissionCancel()
		c.mu.Unlock()
		go func() {
			t.cancelErr = c.cancelTurn(t)
			close(t.cancelDone)
		}()
	})
}

func (c *Client) cancelTurn(t *turn) error {
	ctx, cancel := context.WithTimeout(c.ctx, c.limits.cancel)
	defer cancel()
	select {
	case <-t.request.done:
		return nil
	case <-t.sent:
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		return c.cancelExpired()
	}
	select {
	case <-t.request.done:
		return nil
	default:
	}
	data, _ := encodeMessage(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]string{"sessionId": t.sessionID}})
	if err := c.send(ctx, data); err != nil {
		if ctx.Err() != nil {
			return c.cancelExpired()
		}
		return err
	}
	select {
	case <-t.request.done:
		return nil
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		select {
		case <-t.request.done:
			return nil
		default:
			return c.cancelExpired()
		}
	}
}

func (c *Client) cancelExpired() error {
	if err := c.Err(); err != nil {
		return err
	}
	c.fail(ErrCancelTimeout)
	return ErrCancelTimeout
}

// Cancel 的调用方期限只限制等待，后台仍保留十秒内清理的责任。
func (c *Client) Cancel(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	_, err := c.sessionLocked(sessionID)
	t := c.active
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if t == nil || t.sessionID != sessionID {
		return nil
	}
	c.beginCancel(t)
	select {
	case <-t.cancelDone:
		return t.cancelErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Client) fail(err error) {
	if err == nil {
		err = ErrClosed
	}
	c.failOnce.Do(func() {
		c.mu.Lock()
		c.terminal = err
		if c.active != nil {
			c.active.permissionCancel()
		}
		c.mu.Unlock()
		c.stopContext()
		close(c.done)
		terminateProcess(c.cmd)
		_ = c.stdin.Close()
		_ = c.stdout.Close()
	})
}

func (c *Client) closeWithReason(reason error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closing = true
		t := c.active
		c.mu.Unlock()
		if t != nil {
			c.beginCancel(t)
			<-t.cancelDone
		}
		c.fail(reason)
		<-c.processDone
		close(c.closeDone)
	})
	<-c.closeDone
}

// Close 幂等，先取消活跃生成，再终止和回收拥有的进程。
func (c *Client) Close() error {
	c.closeWithReason(ErrClosed)
	return nil
}
