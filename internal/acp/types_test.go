package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func configValues(first, count int) []any {
	values := make([]any, count)
	for i := range values {
		values[i] = map[string]any{"value": fmt.Sprintf("value-%d", first+i), "name": "Value"}
	}
	return values
}

func configGroups(count int) []any {
	groups := make([]any, count)
	for i := range groups {
		groups[i] = map[string]any{"group": fmt.Sprintf("group-%d", i), "name": "Group", "options": configValues(i, 1)}
	}
	return groups
}

func configOption() map[string]any {
	return map[string]any{"id": "model", "name": "Model", "type": "select", "category": "model", "currentValue": "value-0", "options": configValues(0, 1)}
}

func excessiveConfigOptions() []any {
	options := make([]any, 33)
	for i := range options {
		option := configOption()
		option["id"] = fmt.Sprintf("config-%d", i)
		options[i] = option
	}
	return options
}

func TestConfigResourceBoundaries(t *testing.T) {
	t.Run("32 options", func(t *testing.T) {
		data, _ := json.Marshal(excessiveConfigOptions()[:32])
		options, err := parseConfigOptions(data)
		if err != nil || len(options) != 32 {
			t.Fatalf("boundary rejected: count=%d err=%v", len(options), err)
		}
	})
	valid := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"128 values", func(o map[string]any) { o["options"] = configValues(0, 128) }},
		{"128 grouped values", func(o map[string]any) {
			o["options"] = []any{map[string]any{"group": "a", "name": "A", "options": configValues(0, 64)}, map[string]any{"group": "b", "name": "B", "options": configValues(64, 64)}}
		}},
		{"32 groups", func(o map[string]any) { o["options"] = configGroups(32) }},
		{"id bytes", func(o map[string]any) { o["id"] = strings.Repeat("i", 128) }},
		{"name runes", func(o map[string]any) { o["name"] = strings.Repeat("界", 256) }},
		{"category bytes", func(o map[string]any) { o["category"] = strings.Repeat("c", 64) }},
		{"extension type bytes", func(o map[string]any) { o["type"] = strings.Repeat("t", 32) }},
		{"value fields", func(o map[string]any) {
			o["currentValue"] = strings.Repeat("v", 128)
			o["options"] = []any{map[string]any{"value": o["currentValue"], "name": strings.Repeat("界", 256)}}
		}},
		{"group fields", func(o map[string]any) {
			o["options"] = []any{map[string]any{"group": strings.Repeat("g", 128), "name": strings.Repeat("界", 256), "options": configValues(0, 1)}}
		}},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			o := configOption()
			tc.mutate(o)
			data, _ := json.Marshal([]any{o})
			if _, err := parseConfigOptions(data); err != nil {
				t.Fatalf("exact boundary rejected: %v", err)
			}
		})
	}
}

func TestConfigResourceOverflowRejected(t *testing.T) {
	t.Run("33 options", func(t *testing.T) {
		data, _ := json.Marshal(excessiveConfigOptions())
		if _, err := parseConfigOptions(data); !errors.Is(err, ErrProtocol) {
			t.Fatalf("too many options accepted: %v", err)
		}
	})
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"129 values", func(o map[string]any) { o["options"] = configValues(0, 129) }},
		{"129 values across groups", func(o map[string]any) {
			o["options"] = []any{map[string]any{"group": "a", "name": "A", "options": configValues(0, 64)}, map[string]any{"group": "b", "name": "B", "options": configValues(64, 65)}}
		}},
		{"33 groups", func(o map[string]any) { o["options"] = configGroups(33) }},
		{"id bytes", func(o map[string]any) { o["id"] = strings.Repeat("i", 129) }},
		{"name runes", func(o map[string]any) { o["name"] = strings.Repeat("界", 257) }},
		{"category bytes", func(o map[string]any) { o["category"] = strings.Repeat("c", 65) }},
		{"extension type bytes", func(o map[string]any) { o["type"] = strings.Repeat("t", 33) }},
		{"current value bytes", func(o map[string]any) { o["currentValue"] = strings.Repeat("v", 129) }},
		{"value id bytes", func(o map[string]any) {
			o["options"] = []any{map[string]any{"value": strings.Repeat("v", 129), "name": "Value"}}
		}},
		{"value name runes", func(o map[string]any) {
			o["options"] = []any{map[string]any{"value": "v", "name": strings.Repeat("界", 257)}}
		}},
		{"group id bytes", func(o map[string]any) {
			o["options"] = []any{map[string]any{"group": strings.Repeat("g", 129), "name": "Group", "options": configValues(0, 1)}}
		}},
		{"group name runes", func(o map[string]any) {
			o["options"] = []any{map[string]any{"group": "g", "name": strings.Repeat("界", 257), "options": configValues(0, 1)}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := configOption()
			tc.mutate(o)
			data, _ := json.Marshal([]any{o})
			if _, err := parseConfigOptions(data); !errors.Is(err, ErrProtocol) {
				t.Fatalf("configuration over UI limit accepted: %v", err)
			}
		})
	}
}

func TestConfigLimitsApplyToEveryProtocolSource(t *testing.T) {
	for _, mode := range []string{"config-new-overflow", "config-set-overflow", "config-update-overflow"} {
		t.Run(mode, func(t *testing.T) {
			c := startFake(t, mode, nil)
			if _, err := c.Initialize(context.Background()); err != nil {
				t.Fatal(err)
			}
			s, err := c.NewSession(context.Background(), t.TempDir())
			if mode == "config-new-overflow" {
				if !errors.Is(err, ErrProtocol) || s.ID != "" {
					t.Fatalf("excessive new-session configuration delivered: id=%q count=%d err=%v", s.ID, len(s.ConfigOptions), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "config-set-overflow" {
				_, err = c.SetConfigOption(context.Background(), s.ID, "model", "model-b")
			} else {
				_, err = c.Prompt(context.Background(), s.ID, "update")
			}
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("excessive configuration accepted from %s: %v", mode, err)
			}
		})
	}
}
