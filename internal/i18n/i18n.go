// Package i18n translates the web UI. English strings are the keys: a
// catalog maps each English string to its translation, and anything
// missing falls back to English.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed locales/*.json
var files embed.FS

// Lang is a supported UI language.
type Lang struct {
	Code string // BCP 47 primary tag, e.g. "de"
	Name string // native name, e.g. "Deutsch"
}

// Langs lists the supported languages; English first.
var Langs = []Lang{
	{"en", "English"},
	{"de", "Deutsch"},
	{"es", "Español"},
	{"fr", "Français"},
	{"it", "Italiano"},
	{"pt", "Português"},
}

var catalogs = map[string]map[string]string{}

func init() {
	for _, l := range Langs[1:] {
		b, err := files.ReadFile("locales/" + l.Code + ".json")
		if err != nil {
			panic("i18n: missing catalog " + l.Code)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			panic(fmt.Sprintf("i18n: catalog %s: %v", l.Code, err))
		}
		catalogs[l.Code] = m
	}
}

// Supported reports whether code is a supported language.
func Supported(code string) bool {
	for _, l := range Langs {
		if l.Code == code {
			return true
		}
	}
	return false
}

// T translates s into lang, falling back to s.
func T(lang, s string) string {
	if t, ok := catalogs[lang][s]; ok && t != "" {
		return t
	}
	return s
}

// Tf translates a format string, then formats it.
func Tf(lang, format string, args ...any) string {
	return fmt.Sprintf(T(lang, format), args...)
}

// Match picks the best supported language for an Accept-Language header,
// in the order the browser lists them (quality values are not needed in
// practice: browsers list languages in preference order).
func Match(acceptLanguage string) string {
	for _, part := range strings.Split(acceptLanguage, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		primary, _, _ := strings.Cut(strings.ToLower(tag), "-")
		if Supported(primary) {
			return primary
		}
	}
	return "en"
}

// Catalog returns a copy of a language's catalog, for tests and tools.
func Catalog(lang string) map[string]string {
	out := map[string]string{}
	for k, v := range catalogs[lang] {
		out[k] = v
	}
	return out
}
