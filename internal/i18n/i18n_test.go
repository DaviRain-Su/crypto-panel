package i18n

import "testing"

func TestEveryMessageIsTranslated(t *testing.T) {
	for key, byLang := range messages {
		for _, lang := range []string{English, Chinese} {
			if byLang[lang] == "" {
				t.Errorf("%s has no %s message", key, lang)
			}
		}
	}
}

func TestT(t *testing.T) {
	if got := T(English, AppTitle); got != "Lintel" {
		t.Errorf("English title = %q", got)
	}
	if got := T(Chinese, AppTitle); got != "门厅" {
		t.Errorf("Chinese title = %q", got)
	}
	if got := T("de", AppTitle); got != "Lintel" {
		t.Errorf("unknown language = %q, want the English fallback", got)
	}
	if got := T(English, AccountMany, 3); got != "3 accounts" {
		t.Errorf("AccountMany = %q", got)
	}
	if got := T(English, Key("missing")); got != "missing" {
		t.Errorf("missing key = %q", got)
	}
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		setting string
		system  string
		want    string
	}{
		{English, "zh-Hans", English},
		{Chinese, "en-US", Chinese},
		{System, "zh-Hans-CN", Chinese},
		{System, "zh-Hant", Chinese},
		{System, "en-US", English},
		{System, "", English},
		{"fr", "zh-Hans", Chinese},
	} {
		if got := Resolve(tc.setting, tc.system); got != tc.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", tc.setting, tc.system, got, tc.want)
		}
	}
}
