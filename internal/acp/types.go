// Package acp 提供本机编码 agent 的 ACP v1 stdio 客户端，不提供安装或钱包权限。
package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

const (
	ProtocolVersion   = 1
	HandshakeTimeout  = 120 * time.Second
	PromptTimeout     = 15 * time.Minute
	CancelGracePeriod = 10 * time.Second

	// 原生 UI 只会收到这些上限以内的配置字段，不能仅依赖总消息大小。
	MaxConfigOptions       = 32
	MaxConfigValues        = 128
	MaxConfigWireValues    = 1024
	MaxConfigGroups        = 32
	MaxConfigIDBytes       = 128
	MaxConfigNameRunes     = 256
	MaxConfigCategoryBytes = 64
	MaxConfigTypeBytes     = 32

	maxMessageBytes = 4 << 20
	maxJSONDepth    = 64
	maxUpdateQueue  = 128
	maxWriteQueue   = 32
)

var (
	ErrClosed               = errors.New("acp: client closed")
	ErrNotInitialized       = errors.New("acp: client not initialized")
	ErrUnknownSession       = errors.New("acp: unknown session")
	ErrProtocol             = errors.New("acp: invalid protocol message")
	ErrIncompatibleProtocol = errors.New("acp: incompatible protocol version")
	ErrUnsupportedConfig    = errors.New("acp: configuration was not advertised")
	ErrCancelTimeout        = errors.New("acp: agent did not finish after cancellation")
	ErrUpdateBacklog        = errors.New("acp: update callback queue full")
)

// Config 只接受显式环境；Env 为空时不继承宿主环境或凭据。
type Config struct {
	Executable        string
	Args              []string
	Env               []string
	Cwd               string
	OnUpdate          func(Update)
	RequestPermission func(context.Context, PermissionRequest) (string, error)
}

// Implementation 是 agent 自报的身份信息，不表示可信身份。
type Implementation struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type PromptCapabilities struct {
	Audio           bool `json:"audio"`
	Image           bool `json:"image"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// Capabilities 保留 agent 公布的完整能力；本模块只使用文本基础会话。
type Capabilities struct {
	LoadSession         bool               `json:"loadSession"`
	PromptCapabilities  PromptCapabilities `json:"promptCapabilities"`
	SessionCapabilities json.RawMessage    `json:"sessionCapabilities,omitempty"`
	Raw                 json.RawMessage    `json:"-"`
}

type Initialization struct {
	ProtocolVersion   uint16          `json:"protocolVersion"`
	AgentInfo         *Implementation `json:"agentInfo,omitempty"`
	AgentCapabilities Capabilities    `json:"agentCapabilities"`
	AuthMethods       json.RawMessage `json:"authMethods,omitempty"`
}

type Session struct {
	ID            string         `json:"sessionId"`
	ConfigOptions []ConfigOption `json:"configOptions,omitempty"`
}

// ConfigOption 只为 select 提供可设置的值；其他类型保留 Raw 供宿主说明。
type ConfigOption struct {
	ID           string
	Name         string
	Category     string
	Type         string
	CurrentValue string
	Options      []ConfigValue
	Groups       []ConfigGroup
	Raw          json.RawMessage
}

type ConfigValue struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type ConfigGroup struct {
	Group   string        `json:"group"`
	Name    string        `json:"name"`
	Options []ConfigValue `json:"options"`
}

type PromptResult struct {
	StopReason string `json:"stopReason"`
}

// Update.Raw 是独立的、不可信 JSON 对象，不得解释为安装或交易批准。
type Update struct {
	SessionID string
	Type      string
	Text      string
	Raw       json.RawMessage
}

type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type ToolCall struct {
	ID     string          `json:"toolCallId"`
	Title  string          `json:"title"`
	Kind   string          `json:"kind"`
	Status string          `json:"status"`
	Raw    json.RawMessage `json:"-"`
}

type PermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCall           `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// RPCError 的消息和数据来自不可信 peer；普通错误字符串仅包含错误码。
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp: remote error (code %d)", e.Code) }

func cloneJSON(v json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), v...) }

func cloneInitialization(v Initialization) Initialization {
	if v.AgentInfo != nil {
		info := *v.AgentInfo
		v.AgentInfo = &info
	}
	v.AgentCapabilities.Raw = cloneJSON(v.AgentCapabilities.Raw)
	v.AgentCapabilities.SessionCapabilities = cloneJSON(v.AgentCapabilities.SessionCapabilities)
	v.AuthMethods = cloneJSON(v.AuthMethods)
	return v
}

func cloneSession(s Session) Session {
	s.ConfigOptions = append([]ConfigOption(nil), s.ConfigOptions...)
	for i := range s.ConfigOptions {
		o := &s.ConfigOptions[i]
		o.Raw = cloneJSON(o.Raw)
		o.Options = append([]ConfigValue(nil), o.Options...)
		o.Groups = append([]ConfigGroup(nil), o.Groups...)
		for j := range o.Groups {
			o.Groups[j].Options = append([]ConfigValue(nil), o.Groups[j].Options...)
		}
	}
	return s
}

func parseConfigOptions(raw json.RawMessage) ([]ConfigOption, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || len(items) > MaxConfigOptions {
		return nil, ErrProtocol
	}
	options := make([]ConfigOption, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		var wire struct {
			ID           string          `json:"id"`
			Name         *string         `json:"name"`
			Category     string          `json:"category"`
			Type         string          `json:"type"`
			CurrentValue json.RawMessage `json:"currentValue"`
			Options      json.RawMessage `json:"options"`
		}
		if err := json.Unmarshal(item, &wire); err != nil || wire.ID == "" || wire.Name == nil || wire.Type == "" || seen[wire.ID] {
			return nil, ErrProtocol
		}
		if len(wire.ID) > MaxConfigIDBytes || utf8.RuneCountInString(*wire.Name) > MaxConfigNameRunes || len(wire.Category) > MaxConfigCategoryBytes || len(wire.Type) > MaxConfigTypeBytes {
			return nil, ErrProtocol
		}
		seen[wire.ID] = true
		o := ConfigOption{ID: wire.ID, Name: *wire.Name, Category: wire.Category, Type: wire.Type}
		if wire.Type == "select" {
			if len(wire.CurrentValue) == 0 || string(wire.CurrentValue) == "null" || json.Unmarshal(wire.CurrentValue, &o.CurrentValue) != nil || len(o.CurrentValue) > MaxConfigIDBytes {
				return nil, ErrProtocol
			}
			var values []json.RawMessage
			if len(wire.Options) == 0 || string(wire.Options) == "null" || json.Unmarshal(wire.Options, &values) != nil || len(values) > MaxConfigWireValues {
				return nil, ErrProtocol
			}
			seenValues := make(map[string]bool)
			grouped := false
			for i, value := range values {
				var fields map[string]json.RawMessage
				if json.Unmarshal(value, &fields) != nil || fields == nil {
					return nil, ErrProtocol
				}
				_, isGroup := fields["group"]
				if i == 0 {
					grouped = isGroup
				} else if grouped != isGroup {
					return nil, ErrProtocol
				}
				entries := []json.RawMessage{value}
				var group ConfigGroup
				if isGroup {
					var header struct {
						Group string  `json:"group"`
						Name  *string `json:"name"`
					}
					if len(values) > MaxConfigGroups || json.Unmarshal(value, &header) != nil || header.Group == "" || header.Name == nil {
						return nil, ErrProtocol
					}
					if len(header.Group) > MaxConfigIDBytes || utf8.RuneCountInString(*header.Name) > MaxConfigNameRunes {
						return nil, ErrProtocol
					}
					if fields["options"] == nil || string(fields["options"]) == "null" || json.Unmarshal(fields["options"], &entries) != nil {
						return nil, ErrProtocol
					}
					group.Group, group.Name = header.Group, *header.Name
				}
				// 以整项累计选值，分组不能绕过协议上限。
				if len(entries) > MaxConfigWireValues-len(o.Options) {
					return nil, ErrProtocol
				}
				for _, entry := range entries {
					var v struct {
						Value *string `json:"value"`
						Name  *string `json:"name"`
					}
					if json.Unmarshal(entry, &v) != nil || v.Value == nil || v.Name == nil || len(*v.Value) > MaxConfigIDBytes || utf8.RuneCountInString(*v.Name) > MaxConfigNameRunes || seenValues[*v.Value] {
						return nil, ErrProtocol
					}
					seenValues[*v.Value] = true
					cv := ConfigValue{Value: *v.Value, Name: *v.Name}
					o.Options = append(o.Options, cv)
					group.Options = append(group.Options, cv)
				}
				if isGroup {
					o.Groups = append(o.Groups, group)
				}
			}
		}
		o.Raw = cloneJSON(item)
		options = append(options, o)
	}
	return options, nil
}
