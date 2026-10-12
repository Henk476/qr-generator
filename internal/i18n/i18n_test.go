package i18n

import (
	"strings"
	"testing"
)

func TestMatch(t *testing.T) {
	tests := map[string]string{
		"":                        "en",
		"de-DE,de;q=0.9,en;q=0.8": "de",
		"nl-NL, fr-BE;q=0.8":      "fr",
		"pt-BR":                   "pt",
		"ja, zh":                  "en",
		"EN-gb":                   "en",
	}
	for in, want := range tests {
		if got := Match(in); got != want {
			t.Errorf("Match(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCatalogsKeepFormatVerbs makes sure a translation never drops or
// adds a formatting verb, which would garble messages at run time.
func TestCatalogsKeepFormatVerbs(t *testing.T) {
	verbs := func(s string) string {
		var out []string
		for i := 0; i < len(s)-1; i++ {
			if s[i] == '%' {
				j := i + 1
				for j < len(s) && strings.ContainsRune("0123456789.+-# ", rune(s[j])) {
					j++
				}
				if j < len(s) {
					out = append(out, string(s[j]))
				}
				i = j
			}
		}
		return strings.Join(out, ",")
	}
	for _, l := range Langs[1:] {
		for en, tr := range Catalog(l.Code) {
			if verbs(en) != verbs(tr) {
				t.Errorf("%s: %q has verbs %q, translation %q has %q", l.Code, en, verbs(en), tr, verbs(tr))
			}
		}
	}
}

func TestFallback(t *testing.T) {
	if got := T("de", "no such string"); got != "no such string" {
		t.Errorf("fallback = %q", got)
	}
	if got := T("xx", "Library"); got != "Library" {
		t.Errorf("unknown language = %q", got)
	}
}
