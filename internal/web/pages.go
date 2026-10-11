package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/i18n"
	"github.com/Henk476/qr-generator/internal/matrix"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/store"
)

// Setting keys.
const (
	settingTheme    = "theme"          // "", "light" or "dark"
	settingFormat   = "default_format" // png or svg
	settingSize     = "default_size"   // px
	settingECC      = "default_ecc"    // L, M, Q or H
	settingLanguage = "language"       // "" (browser) or an i18n language code
)

// ---- Library ----

type libraryData struct {
	page
	Codes  []store.Code
	Types  []encode.Type
	Tags   []string
	Filter store.ListOptions
	Total  int // codes in the library, ignoring filters
	Size   int // default download size
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	d := libraryData{
		page:   s.newPage(r, "Library", "library"),
		Types:  encode.Types(),
		Filter: store.ListOptions{Query: q.Get("q"), Type: q.Get("type"), Tag: q.Get("tag"), Sort: q.Get("sort")},
	}
	_, e := s.defaults(r)
	d.Size = e.Size
	var err error
	if d.Codes, err = s.store.ListCodes(ctx, d.Filter); err != nil {
		s.serverError(w, err)
		return
	}
	if d.Tags, err = s.store.Tags(ctx); err != nil {
		s.serverError(w, err)
		return
	}
	all, err := s.store.ListCodes(ctx, store.ListOptions{})
	if err != nil {
		s.serverError(w, err)
		return
	}
	d.Total = len(all)
	switch {
	case q.Has("deleted"):
		d.Flash = s.tr(r, "Code deleted.")
	case q.Has("duplicated"):
		d.Flash = s.tr(r, "Code duplicated.")
	}
	s.render(w, http.StatusOK, "library", d)
}

func (s *Server) loadCode(w http.ResponseWriter, r *http.Request) (*store.Code, bool) {
	c, err := s.store.GetCode(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return nil, false
	} else if err != nil {
		s.serverError(w, err)
		return nil, false
	}
	return c, true
}

// handleCodeImage serves a saved code: ?format=png|svg&size=N&download=1.
func (s *Server) handleCodeImage(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCode(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	format := render.SVG
	if f := q.Get("format"); f != "" {
		var err error
		if format, err = render.ParseFormat(f); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	_, e := s.defaults(r)
	opts := render.Options{Format: format, Size: e.Size, Page: q.Get("page")}
	if v := q.Get("size"); v != "" {
		opts.Size, _ = strconv.Atoi(v)
	}
	if v := q.Get("print_mm"); v != "" {
		opts.PrintMM, _ = strconv.ParseFloat(v, 64)
	}
	code, err := generate.Prepare(generate.Request{Type: c.Type, Fields: c.Fields, Design: c.Design, Assets: s.assetLoader(r.Context()), Fonts: s.fontLoader(r.Context())})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, opts); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := ""
	if q.Has("download") {
		name = generate.Slug(c.Name) + "." + format.Ext()
	}
	writeImage(w, buf.Bytes(), format, name)
}

func (s *Server) handleCodeDelete(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteCode(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/library?deleted", http.StatusSeeOther)
}

func (s *Server) handleCodeDuplicate(w http.ResponseWriter, r *http.Request) {
	c, ok := s.loadCode(w, r)
	if !ok {
		return
	}
	c.Name += " (copy)"
	if err := s.store.CreateCode(r.Context(), c); err != nil {
		s.serverError(w, err)
		return
	}
	http.Redirect(w, r, "/library?duplicated", http.StatusSeeOther)
}

// ---- Presets ----

type presetsData struct {
	page
	Presets []store.Preset
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	d := presetsData{page: s.newPage(r, "Presets", "presets")}
	var err error
	if d.Presets, err = s.store.ListPresets(r.Context()); err != nil {
		s.serverError(w, err)
		return
	}
	if r.URL.Query().Has("deleted") {
		d.Flash = s.tr(r, "Preset deleted.")
	}
	s.render(w, http.StatusOK, "presets", d)
}

// presetSample is the content shown in preset thumbnails.
var presetSample = map[string]string{"url": "https://example.com/canute"}

func (s *Server) handlePresetImage(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPreset(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	code, err := generate.Prepare(generate.Request{Type: "url", Fields: presetSample, Design: p.Design, Assets: s.assetLoader(r.Context()), Fonts: s.fontLoader(r.Context())})
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, render.Options{Format: render.SVG, Size: 256}); err != nil {
		s.serverError(w, err)
		return
	}
	writeImage(w, buf.Bytes(), render.SVG, "")
}

func (s *Server) handlePresetDelete(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeletePreset(r.Context(), r.PathValue("id"))
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.NotFound(w, r)
	case errors.Is(err, store.ErrBuiltin):
		http.Error(w, err.Error(), http.StatusForbidden)
	case err != nil:
		s.serverError(w, err)
	default:
		http.Redirect(w, r, "/presets?deleted", http.StatusSeeOther)
	}
}

// ---- Settings ----

type settingsData struct {
	page
	APIToken        string
	LanguageSetting string
	Langs           []i18n.Lang
	Formats         []render.Option
	Format          string
	Size            int
	ECC             string
	Levels          []string
}

func (s *Server) settingsPage(r *http.Request) settingsData {
	d := settingsData{page: s.newPage(r, "Settings", "settings"), Levels: []string{"L", "M", "Q", "H"}, APIToken: s.APIToken(),
		Langs: i18n.Langs, Formats: s.options.Formats}
	d.LanguageSetting, _ = s.store.GetSetting(r.Context(), settingLanguage, "")
	design, e := s.defaults(r)
	d.Format, d.Size, d.ECC = e.Format, e.Size, design.ECC
	return d
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	d := s.settingsPage(r)
	q := r.URL.Query()
	switch {
	case q.Has("saved"):
		d.Flash = s.tr(r, "Settings saved.")
	case q.Has("token"):
		d.Flash = s.tr(r, "New API token created. Clients using the old token must be updated.")
	case q.Has("restored"):
		var codes, presets, logos int
		fmt.Sscanf(q.Get("restored"), "%d,%d,%d", &codes, &presets, &logos)
		d.Flash = s.trf(r, "Backup restored: %d codes, %d presets and %d logos.", codes, presets, logos)
	}
	s.render(w, http.StatusOK, "settings", d)
}

func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	theme := r.PostFormValue("theme")
	language := r.PostFormValue("language")
	format, ferr := render.ParseFormat(r.PostFormValue("format"))
	size, serr := strconv.Atoi(r.PostFormValue("size"))
	_, eerr := matrix.ParseLevel(r.PostFormValue("ecc"))

	var msg string
	switch {
	case theme != "" && theme != "light" && theme != "dark":
		msg = s.tr(r, "Unknown theme.")
	case language != "" && !i18n.Supported(language):
		msg = s.tr(r, "Unknown language.")
	case ferr != nil:
		msg = ferr.Error()
	case serr != nil || size < render.MinSize || size > render.MaxSize:
		msg = s.trf(r, "Size must be a number from %d to %d.", render.MinSize, render.MaxSize)
	case eerr != nil:
		msg = eerr.Error()
	}
	if msg != "" {
		d := s.settingsPage(r)
		d.Error = msg
		s.render(w, http.StatusUnprocessableEntity, "settings", d)
		return
	}
	ctx := r.Context()
	for k, v := range map[string]string{
		settingTheme:    theme,
		settingLanguage: language,
		settingFormat:   string(format),
		settingSize:     strconv.Itoa(size),
		settingECC:      r.PostFormValue("ecc"),
	} {
		if err := s.store.SetSetting(ctx, k, v); err != nil {
			s.serverError(w, err)
			return
		}
	}
	http.Redirect(w, r, "/settings?saved", http.StatusSeeOther)
}
