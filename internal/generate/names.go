package generate

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Henk476/qr-generator/internal/encode"
)

// DefaultName names an unnamed code after its type and main value, e.g.
// "Link: example.com".
func DefaultName(t encode.Type, fields map[string]string) string {
	v := fields[t.Primary]
	if t.Name == "vcard" {
		v = strings.TrimSpace(fields["first"] + " " + fields["last"])
		if v == "" {
			v = fields["org"]
		}
	}
	v = strings.Join(strings.Fields(v), " ")
	if utf8.RuneCountInString(v) > 40 {
		v = string([]rune(v)[:39]) + "…"
	}
	if v == "" {
		return t.Title
	}
	return t.Title + ": " + v
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug makes a safe ASCII file name stem.
func Slug(s string) string {
	out := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-")
	}
	if out == "" {
		return "qr-code"
	}
	return out
}
