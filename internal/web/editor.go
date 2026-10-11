package web

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/store"
)

// previewPx is the nominal size of the inline preview SVG; CSS scales it.
const previewPx = 512

// editorData backs templates/editor.html.
type editorData struct {
	page
	Action  string // form action URL
	CodeID  string // empty for a new, unsaved code
	Types   []encode.Type
	Type    encode.Type
	Values  map[string]string
	Name    string
	Tags    string
	Design  generate.Design
	Presets []store.Preset
	Preset  string
	Format  string
	Size    int
	Preview preview
}

// FgHex and BgHex feed <input type=color>, which only accepts #rrggbb.
func (d editorData) FgHex() string { return colorInput(d.Design.Foreground, "#000000") }
func (d editorData) BgHex() string { return colorInput(d.Design.Background, "#ffffff") }

// Transparent reports whether the background is transparent.
func (d editorData) Transparent() bool { return d.Design.Background == "transparent" }

func colorInput(c, fallback string) string {
	if len(c) >= 7 && c[0] == '#' {
		return strings.ToLower(c[:7])
	}
	return fallback
}

// preview backs the "preview" template.
type preview struct {
	Empty       bool // no content entered yet
	SVG         template.HTML
	Error       string
	Warnings    []string
	OK          bool
	Version     int
	Modules     int
	ECC         string
	Bytes       int
	Transparent bool
}

func (s *Server) defaults(r *http.Request) (generate.Design, string, int) {
	ctx := r.Context()
	d := generate.DefaultDesign()
	d.ECC, _ = s.store.GetSetting(ctx, settingECC, d.ECC)
	format, _ := s.store.GetSetting(ctx, settingFormat, string(render.PNG))
	sizeStr, _ := s.store.GetSetting(ctx, settingSize, "1024")
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		size = 1024
	}
	return d, format, size
}

func (s *Server) newEditor(r *http.Request, codeID string) (*editorData, error) {
	presets, err := s.store.ListPresets(r.Context())
	if err != nil {
		return nil, err
	}
	design, format, size := s.defaults(r)
	title, nav, action := "New code", "new", "/new"
	if codeID != "" {
		title, nav, action = "Edit code", "library", "/codes/"+codeID
	}
	t, _ := encode.Lookup("url")
	return &editorData{
		page:    s.newPage(r, title, nav),
		Action:  action,
		CodeID:  codeID,
		Types:   encode.Types(),
		Type:    t,
		Values:  map[string]string{},
		Design:  design,
		Presets: presets,
		Format:  format,
		Size:    size,
	}, nil
}

func (s *Server) handleNew(w http.ResponseWriter, r *http.Request) {
	d, err := s.newEditor(r, "")
	if err != nil {
		s.serverError(w, err)
		return
	}
	if t, ok := encode.Lookup(r.URL.Query().Get("type")); ok {
		d.Type = t
	}
	if id := r.URL.Query().Get("preset"); id != "" {
		if p, err := s.store.GetPreset(r.Context(), id); err == nil {
			d.Design, d.Preset = p.Design, p.ID
		}
	}
	d.Preview = makePreview(d)
	s.render(w, http.StatusOK, "editor", d)
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCode(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, err)
		return
	}
	d, err := s.newEditor(r, c.ID)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if t, ok := encode.Lookup(c.Type); ok {
		d.Type = t
	}
	d.Values, d.Name, d.Tags, d.Design = c.Fields, c.Name, strings.Join(c.Tags, ", "), c.Design
	if r.URL.Query().Has("saved") {
		d.Flash = "Saved to your library."
	}
	d.Preview = makePreview(d)
	s.render(w, http.StatusOK, "editor", d)
}

// readForm fills d from the posted editor form.
func readForm(r *http.Request, d *editorData) {
	if t, ok := encode.Lookup(r.PostFormValue("type")); ok {
		d.Type = t
	}
	for k, vs := range r.PostForm {
		if name, ok := strings.CutPrefix(k, "f."); ok && len(vs) > 0 {
			d.Values[name] = vs[0]
		}
	}
	d.Name = strings.TrimSpace(r.PostFormValue("name"))
	d.Tags = r.PostFormValue("tags")
	d.Preset = r.PostFormValue("preset")

	d.Design.ECC = r.PostFormValue("ecc")
	d.Design.Version, _ = strconv.Atoi(r.PostFormValue("version"))
	d.Design.QuietZone, _ = strconv.Atoi(r.PostFormValue("quiet"))
	d.Design.Foreground = r.PostFormValue("fg")
	d.Design.Background = r.PostFormValue("bg")
	if r.PostFormValue("bg_transparent") != "" {
		d.Design.Background = "transparent"
	}
	if f := r.PostFormValue("format"); f != "" {
		d.Format = f
	}
	if n, err := strconv.Atoi(r.PostFormValue("size")); err == nil {
		d.Size = n
	}
}

// request turns the editor state into a generate request. Only the current
// type's non-empty fields are sent, so values left over from switching
// types are ignored.
func (d *editorData) request() generate.Request {
	fields := map[string]string{}
	for _, f := range d.Type.Fields {
		if v := strings.TrimSpace(d.Values[f.Name]); v != "" {
			fields[f.Name] = v
		}
	}
	return generate.Request{Type: d.Type.Name, Fields: fields, Design: d.Design}
}

func makePreview(d *editorData) preview {
	req := d.request()
	p := preview{Transparent: d.Transparent()}
	if len(req.Fields) == 0 {
		p.Empty = true
		return p
	}
	code, err := generate.Prepare(req)
	if err != nil {
		p.Error = err.Error()
		return p
	}
	var buf bytes.Buffer
	if err := render.WriteSVG(&buf, code.Scene, previewPx); err != nil {
		p.Error = err.Error()
		return p
	}
	p.SVG = template.HTML(buf.String()) // generated by our renderer, not user markup
	size := d.Size
	if size < render.MinSize || size > render.MaxSize {
		size = previewPx
	}
	rep := code.Verify(size)
	p.Warnings, p.OK = rep.Warnings, rep.OK()
	p.Version, p.Modules, p.ECC, p.Bytes = code.Matrix.Version, code.Matrix.Size, code.Matrix.Level.String(), len(code.Payload)
	return p
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	d, err := s.newEditor(r, "")
	if err != nil {
		s.serverError(w, err)
		return
	}
	readForm(r, d)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pages["preview"].ExecuteTemplate(w, "preview", makePreview(d)); err != nil {
		s.serverError(w, err)
	}
}

func (s *Server) handleEditorPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if id != "" {
		if _, err := s.store.GetCode(ctx, id); errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			s.serverError(w, err)
			return
		}
	}
	d, err := s.newEditor(r, id)
	if err != nil {
		s.serverError(w, err)
		return
	}
	readForm(r, d)
	status := http.StatusOK

	action := r.PostFormValue("action")
	switch {
	case strings.HasPrefix(action, "type:"):
		if t, ok := encode.Lookup(strings.TrimPrefix(action, "type:")); ok {
			d.Type = t
		}

	case action == "apply-preset":
		if p, err := s.store.GetPreset(ctx, d.Preset); err == nil {
			d.Design = p.Design
		}

	case action == "save-preset":
		name := strings.TrimSpace(r.PostFormValue("preset_name"))
		switch err := d.Design.Validate(); {
		case name == "":
			d.Error, status = "Give the preset a name.", http.StatusUnprocessableEntity
		case err != nil:
			d.Error, status = "Cannot save preset: "+err.Error(), http.StatusUnprocessableEntity
		default:
			p := &store.Preset{Name: name, Design: d.Design}
			if err := s.store.CreatePreset(ctx, p); err != nil {
				s.serverError(w, err)
				return
			}
			d.Preset, d.Flash = p.ID, fmt.Sprintf("Preset %q saved.", name)
			if d.Presets, err = s.store.ListPresets(ctx); err != nil {
				s.serverError(w, err)
				return
			}
		}

	case action == "download":
		if err := s.download(w, d); err != nil {
			d.Error, status = err.Error(), http.StatusUnprocessableEntity
			break
		}
		return

	case action == "save", action == "save-new":
		if _, err := generate.Prepare(d.request()); err != nil {
			d.Error, status = "Cannot save: "+err.Error(), http.StatusUnprocessableEntity
			break
		}
		c := &store.Code{
			ID:     id,
			Name:   d.Name,
			Type:   d.Type.Name,
			Fields: d.request().Fields,
			Design: d.Design,
			Tags:   strings.Split(d.Tags, ","),
		}
		if c.Name == "" {
			c.Name = defaultName(d.Type, c.Fields)
		}
		if id == "" || action == "save-new" {
			err = s.store.CreateCode(ctx, c)
		} else {
			err = s.store.UpdateCode(ctx, c)
		}
		if err != nil {
			s.serverError(w, err)
			return
		}
		http.Redirect(w, r, "/codes/"+c.ID+"?saved", http.StatusSeeOther)
		return
	}

	d.Preview = makePreview(d)
	s.render(w, status, "editor", d)
}

// download writes the code as an attachment, or returns why it cannot.
func (s *Server) download(w http.ResponseWriter, d *editorData) error {
	format, err := render.ParseFormat(d.Format)
	if err != nil {
		return err
	}
	code, err := generate.Prepare(d.request())
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, format, d.Size); err != nil {
		return err
	}
	name := d.Name
	if name == "" {
		name = defaultName(d.Type, d.request().Fields)
	}
	writeImage(w, buf.Bytes(), format, slug(name)+"."+string(format))
	return nil
}

func writeImage(w http.ResponseWriter, b []byte, f render.Format, attachmentName string) {
	ct := "image/png"
	if f == render.SVG {
		ct = "image/svg+xml"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	if attachmentName != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, attachmentName))
	}
	w.Write(b)
}

// defaultName names an unnamed code after its type and main value.
func defaultName(t encode.Type, fields map[string]string) string {
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

// slug makes a safe ASCII file name stem.
func slug(s string) string {
	out := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-")
	}
	if out == "" {
		return "qr-code"
	}
	return out
}
