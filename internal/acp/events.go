package acp

import (
	"encoding/json"
)

func (c *Client) updateLoop() {
	for {
		select {
		case <-c.done:
			return
		case update := <-c.updates:
			c.cfg.OnUpdate(update)
		}
	}
}

func (c *Client) handleUpdate(raw json.RawMessage) error {
	var params struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if json.Unmarshal(raw, &params) != nil || params.SessionID == "" {
		return ErrProtocol
	}
	fields, err := decodeObject(params.Update)
	if err != nil {
		return err
	}
	var kind string
	if json.Unmarshal(fields["sessionUpdate"], &kind) != nil || kind == "" {
		return ErrProtocol
	}
	update := Update{SessionID: params.SessionID, Type: kind, Raw: cloneJSON(params.Update)}
	switch kind {
	case "agent_message_chunk", "agent_thought_chunk", "user_message_chunk", "compaction_summary_chunk":
		var content struct {
			Type string  `json:"type"`
			Text *string `json:"text"`
		}
		if json.Unmarshal(fields["content"], &content) != nil || content.Type == "" {
			return ErrProtocol
		}
		if content.Type == "text" {
			if content.Text == nil {
				return ErrProtocol
			}
			update.Text = *content.Text
		}
	case "tool_call", "tool_call_update":
		var id string
		if json.Unmarshal(fields["toolCallId"], &id) != nil || id == "" || (kind == "tool_call" && fields["title"] == nil) {
			return ErrProtocol
		}
	case "config_option_update":
		if fields["configOptions"] == nil || string(fields["configOptions"]) == "null" {
			return ErrProtocol
		}
		options, err := parseConfigOptions(fields["configOptions"])
		if err != nil {
			return err
		}
		c.mu.Lock()
		if s, ok := c.sessions[params.SessionID]; ok {
			s.ConfigOptions = options
			c.sessions[s.ID] = s
		}
		c.mu.Unlock()
	}
	if c.cfg.OnUpdate != nil {
		select {
		case c.updates <- update:
		case <-c.done:
			return c.Err()
		default:
			return ErrUpdateBacklog
		}
	}
	return nil
}

func parsePermission(raw json.RawMessage) (PermissionRequest, error) {
	var request PermissionRequest
	fields, err := decodeObject(raw)
	if err != nil || json.Unmarshal(raw, &request) != nil || request.SessionID == "" || request.ToolCall.ID == "" || fields["options"] == nil || string(fields["options"]) == "null" {
		return PermissionRequest{}, ErrProtocol
	}
	request.ToolCall.Raw = cloneJSON(fields["toolCall"])
	seen := make(map[string]bool)
	var rawOptions []map[string]json.RawMessage
	if json.Unmarshal(fields["options"], &rawOptions) != nil {
		return PermissionRequest{}, ErrProtocol
	}
	for i, option := range request.Options {
		if option.OptionID == "" || seen[option.OptionID] || rawOptions[i]["name"] == nil || string(rawOptions[i]["name"]) == "null" {
			return PermissionRequest{}, ErrProtocol
		}
		seen[option.OptionID] = true
		switch option.Kind {
		case "allow_once", "allow_always", "reject_once", "reject_always":
		default:
			return PermissionRequest{}, ErrProtocol
		}
	}
	return request, nil
}

func cancelledPermission() any {
	return map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}
}

// permissionBeforeAccept 仅测试设置。它在许可选择已经返回、尚未被接受时运行。
var permissionBeforeAccept func(*Client)

func (c *Client) handleRequest(msg message) {
	c.mu.Lock()
	duplicate := c.inbound[msg.idKey]
	c.mu.Unlock()
	if duplicate {
		c.fail(ErrProtocol)
		return
	}
	if msg.method != "session/request_permission" {
		c.enqueueReply(msg.id, nil, &RPCError{Code: -32601, Message: "Method not supported"})
		return
	}
	request, err := parsePermission(msg.params)
	if err != nil {
		c.enqueueReply(msg.id, nil, &RPCError{Code: -32602, Message: "Invalid permission parameters"})
		return
	}
	c.mu.Lock()
	t := c.active
	eligible := t != nil && t.sessionID == request.SessionID && !t.cancelRequested && !c.closing && c.terminal == nil
	c.mu.Unlock()
	if !eligible || c.cfg.RequestPermission == nil {
		c.enqueueReply(msg.id, cancelledPermission(), nil)
		return
	}
	select {
	case c.permissions <- struct{}{}:
	default:
		c.enqueueReply(msg.id, cancelledPermission(), nil)
		return
	}
	// 原始允许表独立保存，宿主回调对参数的修改不能增加可选项。
	allowed := make(map[string]bool, len(request.Options))
	for _, option := range request.Options {
		allowed[option.OptionID] = true
	}
	request.Options = append([]PermissionOption(nil), request.Options...)
	c.mu.Lock()
	c.inbound[msg.idKey] = true
	c.mu.Unlock()
	type choice struct {
		id  string
		err error
	}
	selected := make(chan choice, 1)
	go func() {
		// 即使宿主忽略 ctx，也始终最多保留一个未返回的原生许可回调。
		defer func() { <-c.permissions }()
		id, err := c.cfg.RequestPermission(t.permissionCtx, request)
		selected <- choice{id: id, err: err}
	}()
	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.inbound, msg.idKey)
			c.mu.Unlock()
		}()
		result := cancelledPermission()
		var answer choice
		select {
		case answer = <-selected:
		case <-t.permissionCtx.Done():
		}
		// 生产路径为空。测试在观察到选择后、接受选择前取消本轮。
		if permissionBeforeAccept != nil {
			permissionBeforeAccept(c)
		}
		c.mu.Lock()
		canSelect := answer.err == nil && answer.id != "" && allowed[answer.id] && t.permissionCtx.Err() == nil && c.active == t && !t.cancelRequested && !c.closing && c.terminal == nil
		c.mu.Unlock()
		if canSelect {
			result = map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": answer.id}}
		}
		c.enqueueReply(msg.id, result, nil)
	}()
}
