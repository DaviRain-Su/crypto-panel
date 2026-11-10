package pages

import (
	"strings"
	"testing"
)

const validPage = `{
  "title": "Portfolio",
  "subtitle": "Base and Ethereum",
  "sections": [
    { "title": "Total", "stats": [{ "label": "Value", "value": "$12,480.20" }] },
    { "title": "Tokens", "table": {
        "columns": [{ "title": "Token" }, { "title": "Balance", "align": "end" }],
        "rows": [["ETH", "3.204"], ["USDC", "1,200.00"]]
    } },
    { "title": "Pools", "cards": [{ "title": "ETH/USDC", "subtitle": "0.05%", "body": "APR 12.4%" }] },
    { "title": "More", "links": [{ "title": "History", "page": "history" }] },
    { "text": "Plain text", "divider": true },
    { "chart": { "kind": "line", "points": [{ "label": "Mon", "value": 1 }, { "label": "Tue", "value": 2 }] } },
    { "gauge": { "label": "Health", "value": 62, "text": "62%" } }
  ]
}`

func TestDecodeValid(t *testing.T) {
	p, err := Decode([]byte(validPage))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.Title != "Portfolio" || len(p.Sections) != 7 {
		t.Fatalf("page = %+v", p)
	}
	if p.Sections[1].Table.Rows[0][1] != "3.204" {
		t.Fatalf("table = %+v", p.Sections[1].Table)
	}
	if p.Sections[6].Gauge == nil || p.Sections[6].Gauge.Value != 62 {
		t.Fatalf("gauge = %+v", p.Sections[6].Gauge)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := []struct {
		name string
		page string
		want string
	}{
		{
			name: "unknown field",
			page: `{"title": "x", "dialog": {}}`,
			want: "unknown field",
		},
		{
			name: "row width",
			page: `{"sections": [{"table": {"columns": [{"title": "A"}], "rows": [["a", "b"]]}}]}`,
			want: "want 1",
		},
		{
			name: "no columns",
			page: `{"sections": [{"table": {"columns": [], "rows": []}}]}`,
			want: "1 to 8 columns",
		},
		{
			name: "bad align",
			page: `{"sections": [{"table": {"columns": [{"title": "A", "align": "middle"}], "rows": []}}]}`,
			want: "align must be start or end",
		},
		{
			name: "bad link",
			page: `{"sections": [{"links": [{"title": "x", "page": "Bad Page"}]}]}`,
			want: "lowercase letters",
		},
		{
			name: "bad chart kind",
			page: `{"sections": [{"chart": {"kind": "pie", "points": [{"label": "a", "value": 1}]}}]}`,
			want: "must be line or bar",
		},
		{
			name: "empty chart",
			page: `{"sections": [{"chart": {"kind": "line", "points": []}}]}`,
			want: "1 to 60 points",
		},
		{
			name: "control character",
			page: `{"title": "a\u0007b"}`,
			want: "control character",
		},
		{
			name: "bidi override",
			page: `{"title": "a‮b"}`,
			want: "bidirectional",
		},
		{
			name: "bad trend",
			page: `{"sections": [{"stats": [{"label": "a", "value": "1", "trend": "sideways"}]}]}`,
			want: "neither up nor down",
		},
		{
			name: "sparkline too long",
			page: `{"sections": [{"stats": [{"label": "a", "value": "1", "sparkline": [` +
				strings.TrimSuffix(strings.Repeat("1,", maxSparkline+1), ",") + `]}]}]}`,
			want: "sparkline points",
		},
		{
			name: "gauge out of range",
			page: `{"sections": [{"gauge": {"value": 120}}]}`,
			want: "outside 0 to 100",
		},
		{
			name: "trailing data",
			page: `{"title": "x"}{}`,
			want: "trailing data",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode([]byte(tc.page))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestDecodeRejectsHugePage is the §13 fixture: a page over the size the
// shell draws is refused before it is parsed.
func TestDecodeRejectsHugePage(t *testing.T) {
	page := `{"title":"` + strings.Repeat("a", maxPageBytes) + `"}`
	if _, err := Decode([]byte(page)); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeValue(t *testing.T) {
	p, err := DecodeValue(map[string]any{
		"title":    "From a map",
		"sections": []any{map[string]any{"text": "hi"}},
	})
	if err != nil {
		t.Fatalf("DecodeValue: %v", err)
	}
	if p.Title != "From a map" || p.Sections[0].Text != "hi" {
		t.Fatalf("page = %+v", p)
	}
}

func TestTrim(t *testing.T) {
	if got := Trim("  hello  ", 10); got != "hello" {
		t.Fatalf("Trim = %q", got)
	}
	if got := Trim("abcdef", 3); got != "abc…" {
		t.Fatalf("Trim = %q", got)
	}
}
