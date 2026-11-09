package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

type message struct {
	id     json.RawMessage
	idKey  string
	hasID  bool
	method string
	params json.RawMessage
	result json.RawMessage
	rpcErr *RPCError
}

type reply struct {
	result json.RawMessage
	err    error
}

type pendingRequest struct {
	idKey     string
	method    string
	sessionID string
	reply     chan reply
	done      chan struct{}
}

type outbound struct {
	ctx  context.Context
	data []byte
	ack  chan error
}

// readFrame 严格要求 LF；最后一个没有分隔符的片段不能冒充完整响应。
func readFrame(r *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(frame)+len(part) > maxMessageBytes {
			return nil, ErrProtocol
		}
		frame = append(frame, part...)
		switch {
		case err == nil:
			return frame[:len(frame)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(frame) != 0:
			return nil, ErrProtocol
		default:
			return nil, err
		}
	}
}

// consumeJSON 拒绝所有层级的重复键，并限制嵌套深度。
func consumeJSON(d *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return ErrProtocol
	}
	token, err := d.Token()
	if err != nil {
		return ErrProtocol
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for d.More() {
			key, err := d.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return ErrProtocol
			}
			seen[name] = true
			if err := consumeJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := consumeJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrProtocol
	}
	end, err := d.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return ErrProtocol
	}
	return nil
}

func parseID(raw json.RawMessage) (string, error) {
	if string(raw) == "null" {
		return "null", nil
	}
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", ErrProtocol
		}
		return "s:" + s, nil
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return "", ErrProtocol
	}
	return "n:" + strconv.FormatInt(n, 10), nil
}

func parseMessage(frame []byte) (message, error) {
	var msg message
	if !utf8.Valid(frame) {
		return msg, ErrProtocol
	}
	d := json.NewDecoder(bytes.NewReader(frame))
	d.UseNumber()
	if err := consumeJSON(d, 0); err != nil {
		return msg, err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return msg, ErrProtocol
	}
	var fields map[string]json.RawMessage
	var version string
	if json.Unmarshal(frame, &fields) != nil || fields == nil || json.Unmarshal(fields["jsonrpc"], &version) != nil || version != "2.0" {
		return msg, ErrProtocol
	}
	var err error
	msg.id, msg.hasID = fields["id"]
	if msg.hasID {
		if msg.idKey, err = parseID(msg.id); err != nil {
			return msg, err
		}
	}
	method, isMethod := fields["method"]
	result, isResult := fields["result"]
	rpcErr, isError := fields["error"]
	if isMethod {
		if isResult || isError || json.Unmarshal(method, &msg.method) != nil || msg.method == "" {
			return msg, ErrProtocol
		}
		msg.params = fields["params"]
		if len(msg.params) > 0 && string(msg.params) != "null" && msg.params[0] != '{' && msg.params[0] != '[' {
			return msg, ErrProtocol
		}
		return msg, nil
	}
	if !msg.hasID || isResult == isError || fields["params"] != nil {
		return msg, ErrProtocol
	}
	if isResult {
		msg.result = result
		return msg, nil
	}
	var remote struct {
		Code    *int32          `json:"code"`
		Message *string         `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(rpcErr, &remote) != nil || remote.Code == nil || remote.Message == nil {
		return msg, ErrProtocol
	}
	msg.rpcErr = &RPCError{Code: int(*remote.Code), Message: *remote.Message, Data: cloneJSON(remote.Data)}
	return msg, nil
}

func encodeMessage(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot encode message", ErrProtocol)
	}
	if len(data)+1 > maxMessageBytes {
		return nil, fmt.Errorf("%w: message exceeds limit", ErrProtocol)
	}
	return append(data, '\n'), nil
}

func (c *Client) readLoop() {
	defer close(c.readDone)
	r := bufio.NewReaderSize(c.stdout, 64<<10)
	for {
		frame, err := readFrame(r)
		if err != nil {
			c.fail(err)
			return
		}
		msg, err := parseMessage(frame)
		if err != nil {
			c.fail(err)
			return
		}
		if msg.method != "" {
			if msg.hasID {
				c.handleRequest(msg)
			} else if msg.method == "session/update" {
				if err := c.handleUpdate(msg.params); err != nil {
					c.fail(err)
					return
				}
			}
			continue
		}
		c.mu.Lock()
		p := c.pending[msg.idKey]
		if p != nil {
			delete(c.pending, msg.idKey)
			if c.active != nil && c.active.request == p {
				c.active.permissionCancel()
			}
		}
		c.mu.Unlock()
		if p != nil {
			var remoteErr error
			if msg.rpcErr != nil {
				remoteErr = msg.rpcErr
			} else if err := c.applyResponse(p, msg.result); err != nil {
				c.fail(err)
				return
			}
			p.reply <- reply{result: msg.result, err: remoteErr}
			close(p.done)
		}
	}
}

func (c *Client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case out := <-c.writes:
			err := out.ctx.Err()
			if err == nil {
				_, err = c.stdin.Write(out.data)
				if err != nil {
					c.fail(fmt.Errorf("acp: write: %w", err))
				}
			}
			if out.ack != nil {
				out.ack <- err
			}
		}
	}
}

func (c *Client) send(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ack := make(chan error, 1)
	select {
	case c.writes <- outbound{ctx: ctx, data: data, ack: ack}:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.Err()
	}
}

// enqueueReply 不阻塞读取协程；对恶意请求洪泛采用有界失败。
func (c *Client) enqueueReply(id json.RawMessage, result any, remote *RPCError) {
	v := map[string]any{"jsonrpc": "2.0", "id": id}
	if remote != nil {
		v["error"] = remote
	} else {
		v["result"] = result
	}
	data, err := encodeMessage(v)
	if err != nil {
		c.fail(err)
		return
	}
	select {
	case <-c.done:
	case c.writes <- outbound{ctx: c.ctx, data: data}:
	default:
		c.fail(fmt.Errorf("%w: reply queue full", ErrProtocol))
	}
}
