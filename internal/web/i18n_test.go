package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/i18n"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/store"
	"github.com/Henk476/qr-generator/internal/verify"
)

// translatable collects every English string the web UI can show through
// the translator.
func translatable(t *testing.T) []string {
	t.Helper()
	keys := map[string]bool{}
	add := func(s string) {
		if s != "" {
			keys[s] = true
		}
	}

	// Template literals: {{t "..."}} and {{tf "..." ...}}.
	tmplRE := regexp.MustCompile(`\{\{\s*tf? "((?:[^"\\]|\\.)*)"`)
	fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := templateFS.ReadFile(path)
		for _, m := range tmplRE.FindAllStringSubmatch(string(b), -1) {
			add(m[1])
		}
		return nil
	})

	// Handler messages: s.tr(r, "...") and s.trf(r, "...", ...).
	goRE := regexp.MustCompile(`s\.trf?\(r, "((?:[^"\\]|\\.)*)"`)
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range goRE.FindAllStringSubmatch(string(b), -1) {
			add(strings.ReplaceAll(m[1], `\"`, `"`))
		}
	}

	// Scan-check warnings and notes.
	b, err := os.ReadFile(filepath.Join("..", "verify", "verify.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`warn\("((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(string(b), -1) {
		add(m[1])
	}
	add(verify.ErrNoReader.Error())

	// Go-side labels.
	for _, ty := range encode.Types() {
		add(ty.Title)
		for _, f := range ty.Fields {
			add(f.Label)
			add(funcs["hint"].(func(encode.Field) string)(f))
		}
	}
	for _, opts := range [][]render.Option{render.ModuleShapes, render.EyeOuterShapes, render.EyeInnerShapes,
		render.Frames, render.Fonts, render.Pages, render.Icons(), newDesignOptions().Formats} {
		for _, o := range opts {
			add(o.Label)
		}
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ps, _ := st.ListPresets(context.Background())
	for _, p := range ps {
		add(p.Name)
	}
	// Page titles set by handlers.
	for _, title := range []string{"New code", "Edit code", "Library", "Bulk", "Presets", "Settings",
		"Top left", "Top right", "Bottom left"} { // page titles and per-corner labels
		add(title)
	}

	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// TestCatalogsComplete fails when a language is missing a translation.
// Run with I18N_DUMP=path to write the full key list as JSON.
func TestCatalogsComplete(t *testing.T) {
	keys := translatable(t)
	if len(keys) < 150 {
		t.Fatalf("only %d translatable strings found; extraction is broken", len(keys))
	}
	if path := os.Getenv("I18N_DUMP"); path != "" {
		b, _ := json.MarshalIndent(keys, "", "  ")
		os.WriteFile(path, b, 0o644)
	}
	for _, l := range i18n.Langs[1:] {
		cat := i18n.Catalog(l.Code)
		var missing []string
		for _, k := range keys {
			if cat[k] == "" {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s (%s) is missing %d of %d translations, e.g. %q", l.Name, l.Code, len(missing), len(keys), missing[0])
		}
		var stale []string
		for k := range cat {
			if !slices.Contains(keys, k) {
				stale = append(stale, k)
			}
		}
		if len(stale) > 0 {
			slices.Sort(stale)
			t.Errorf("%s has %d unused entries, e.g. %q", l.Code, len(stale), stale[0])
		}
	}
}

func TestPagesRenderInEveryLanguage(t *testing.T) {
	a := newTestApp(t)
	for _, l := range i18n.Langs {
		req := httptest.NewRequest("GET", "/new", nil)
		req.Header.Set("Accept-Language", l.Code)
		rec := a.do(req)
		wantStatus(t, rec, 200)
		wantBody(t, rec, `<html lang="`+l.Code+`"`, i18n.T(l.Code, "Library"), i18n.T(l.Code, "Website address"))
	}
	// The saved setting wins over the browser.
	if rec := a.post("/settings", map[string][]string{"language": {"de"}, "format": {"png"}, "size": {"512"}, "ecc": {"M"}}); rec.Code != 303 {
		t.Fatalf("save language: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/library", nil)
	req.Header.Set("Accept-Language", "fr")
	wantBody(t, a.do(req), `<html lang="de"`)
}
