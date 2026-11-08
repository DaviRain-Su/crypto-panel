package plugin

import (
	"strings"
	"testing"
)

const validManifest = `{
  "id": "com.example.portfolio",
  "name": "Portfolio",
  "version": "1.2.0",
  "main": "index.js",
  "description": "Example",
  "permissions": {
    "network": ["api.example.com", "*.example.org"],
    "chains": ["eip155:1"],
    "read": ["balances"],
    "storageKb": 64
  },
  "settings": [
    { "key": "apiKey", "label": "API key", "secret": true }
  ],
  "navigation": [
    { "title": "Portfolio", "page": "overview" }
  ]
}`

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.ID != "com.example.portfolio" || m.Permissions.StorageKb != 64 {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestParseManifestAllowsSigningWithAChain(t *testing.T) {
	s := strings.Replace(validManifest, `"storageKb": 64`, `"storageKb": 64, "sign": true`, 1)
	m, err := ParseManifest([]byte(s))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if !m.Permissions.Sign {
		t.Fatal("sign was not kept")
	}
}

func TestParseManifestRejects(t *testing.T) {
	cases := []struct {
		name string
		edit func(string) string
		want string
	}{
		{
			name: "unknown key",
			edit: func(s string) string { return strings.Replace(s, `"description"`, `"descripton"`, 1) },
			want: "unknown field",
		},
		{
			name: "unknown permission",
			edit: func(s string) string {
				return strings.Replace(s, `"storageKb": 64`, `"storageKb": 64, "process": true`, 1)
			},
			want: "unknown field",
		},
		{
			name: "bad id",
			edit: func(s string) string { return strings.Replace(s, "com.example.portfolio", "Portfolio", 1) },
			want: "reverse-DNS",
		},
		{
			name: "bad version",
			edit: func(s string) string { return strings.Replace(s, `"1.2.0"`, `"1.2"`, 1) },
			want: "semver",
		},
		{
			name: "main escapes",
			edit: func(s string) string { return strings.Replace(s, `"index.js"`, `"../index.js"`, 1) },
			want: "clean path inside the plugin",
		},
		{
			name: "main not js",
			edit: func(s string) string { return strings.Replace(s, `"index.js"`, `"index.wasm"`, 1) },
			want: ".js or .mjs",
		},
		{
			name: "unknown chain",
			edit: func(s string) string { return strings.Replace(s, `"eip155:1"`, `"eip155:999999"`, 1) },
			want: "not one this app knows",
		},
		{
			name: "unknown read scope",
			edit: func(s string) string { return strings.Replace(s, `"balances"`, `"keys"`, 1) },
			want: "read scope",
		},
		{
			name: "signing without a chain",
			edit: func(s string) string {
				s = strings.Replace(s, `"chains": ["eip155:1"],`, ``, 1)
				return strings.Replace(s, `"storageKb": 64`, `"storageKb": 64, "sign": true`, 1)
			},
			want: "at least one chain",
		},
		{
			name: "storage too large",
			edit: func(s string) string { return strings.Replace(s, `"storageKb": 64`, `"storageKb": 4096`, 1) },
			want: "storageKb",
		},
		{
			name: "bad network entry",
			edit: func(s string) string { return strings.Replace(s, `"api.example.com"`, `"https://api.example.com"`, 1) },
			want: "hostname",
		},
		{
			name: "bad navigation page",
			edit: func(s string) string { return strings.Replace(s, `"overview"`, `"Overview!"`, 1) },
			want: "lowercase letters",
		},
		{
			name: "newer app",
			edit: func(s string) string { return strings.Replace(s, `"main":`, `"minAppVersion": "99.0.0", "main":`, 1) },
			want: "needs app",
		},
		{
			name: "trailing data",
			edit: func(s string) string { return s + `{}` },
			want: "trailing data",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(tc.edit(validManifest)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.10.0", -1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0", "1.0.0-beta", 1},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
