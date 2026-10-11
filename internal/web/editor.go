package web

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/logo"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/store"
)

// previewPx is the nominal size of the inline preview SVG; CSS scales it.
const previewPx = 512

// editorData backs templates/editor.html.
type editorData struct {
	page
	Opts    *designOptions
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
	Export  exportForm
	Preview preview
}

// exportForm is the Download step's settings.
type exportForm struct {
	Format  string
	Size    int
	Quality int
	Page    string
	PrintMM float64
}

func (e exportForm) options() (render.Options, error) {
	f, err := render.ParseFormat(e.Format)
	return render.Options{Format: f, Size: e.Size, Quality: e.Quality, Page: e.Page, PrintMM: e.PrintMM}, err
}

// checkPx is the output size the scan check should assume.
func (e exportForm) checkPx() int {
	if f, err := render.ParseFormat(e.Format); err == nil && (f == render.PDF || f == render.EPS) {
		return render.MaxSize // printed: resolution is not the bottleneck
	}
	if e.Size < render.MinSize || e.Size > render.MaxSize {
		return previewPx
	}
	return e.Size
}

// Values for <input type=color>, which only accepts #rrggbb.
func (d editorData) FgHex() string  { return colorInput(d.Design.Foreground, "#000000") }
func (d editorData) BgHex() string  { return colorInput(d.Design.Background, "#ffffff") }
func (d editorData) Fg2Hex() string { return colorInput(d.Design.Foreground2, "#0b5d7a") }
func (d editorData) EyeOuterHex() string {
	return colorInput(d.Design.EyeOuterColor, d.FgHex())
}
func (d editorData) EyeInnerHex() string {
	return colorInput(d.Design.EyeInnerColor, d.FgHex())
}
func (d editorData) FrameColorHex() string { return colorInput(d.Design.FrameColor, d.FgHex()) }
func (d editorData) FrameTextHex() string  { return colorInput(d.Design.FrameTextColor, "#ffffff") }

// Transparent reports whether the background is transparent.
func (d editorData) Transparent() bool { return d.Design.Background == "transparent" }

// LogoSize is the logo size slider value.
func (d editorData) LogoSize() int {
	if d.Design.LogoSize == 0 {
		return generate.DefaultLogoSize
	}
	return d.Design.LogoSize
}

// FrameText is the frame phrase shown in the form.
func (d editorData) FrameText() string {
	if d.Design.FrameText == "" {
		return generate.DefaultFrameText
	}
	return d.Design.FrameText
}

// UploadedLogo reports whether the logo is an uploaded image.
func (d editorData) UploadedLogo() bool { return strings.HasPrefix(d.Design.Logo, "asset:") }

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
	Summary     string
	Notes       []string
	Bytes       int
	Transparent bool
}

func (s *Server) defaults(r *http.Request) (generate.Design, exportForm) {
	ctx := r.Context()
	d := generate.DefaultDesign()
	d.ECC, _ = s.store.GetSetting(ctx, settingECC, d.ECC)
	format, _ := s.store.GetSetting(ctx, settingFormat, string(render.PNG))
	sizeStr, _ := s.store.GetSetting(ctx, settingSize, "1024")
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		size = 1024
	}
	return d, exportForm{Format: format, Size: size, Quality: 90, Page: "fit", PrintMM: 50}
}

func (s *Server) newEditor(r *http.Request, codeID string) (*editorData, error) {
	presets, err := s.store.ListPresets(r.Context())
	if err != nil {
		return nil, err
	}
	design, export := s.defaults(r)
	title, nav, action := "New code", "new", "/new"
	if codeID != "" {
		title, nav, action = "Edit code", "library", "/codes/"+codeID
	}
	t, _ := encode.Lookup("url")
	return &editorData{
		page:    s.newPage(r, title, nav),
		Opts:    s.options,
		Action:  action,
		CodeID:  codeID,
		Types:   encode.Types(),
		Type:    t,
		Values:  map[string]string{},
		Design:  design,
		Presets: presets,
		Export:  export,
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
	d.Preview = s.makePreview(r, d)
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
	d.Preview = s.makePreview(r, d)
	s.render(w, http.StatusOK, "editor", d)
}

// readForm fills d from the posted editor form.
func readForm(r *http.Request, d *editorData) {
	v := func(k string) string { return strings.TrimSpace(r.PostFormValue(k)) }
	on := func(k string) bool { return r.PostFormValue(k) != "" }
	num := func(k string) int { n, _ := strconv.Atoi(v(k)); return n }

	if t, ok := encode.Lookup(v("type")); ok {
		d.Type = t
	}
	for k, vs := range r.PostForm {
		if name, ok := strings.CutPrefix(k, "f."); ok && len(vs) > 0 {
			d.Values[name] = vs[0]
		}
	}
	d.Name, d.Tags, d.Preset = v("name"), r.PostFormValue("tags"), v("preset")

	g := &d.Design
	g.ECC, g.Version, g.QuietZone = v("ecc"), num("version"), num("quiet")
	g.Foreground, g.Background = v("fg"), v("bg")
	if on("bg_transparent") {
		g.Background = "transparent"
	}
	g.Gradient, g.Foreground2, g.GradientAngle = v("gradient"), v("fg2"), num("gradient_angle")
	g.Module, g.EyeOuter, g.EyeInner = v("module"), v("eye_outer"), v("eye_inner")
	g.EyeOuterColor, g.EyeInnerColor = "", ""
	if on("eye_outer_custom") {
		g.EyeOuterColor = v("eye_outer_color")
	}
	if on("eye_inner_custom") {
		g.EyeInnerColor = v("eye_inner_color")
	}
	g.Logo, g.LogoSize = v("logo"), num("logo_size")
	g.LogoCircle, g.LogoKeepModules = on("logo_circle"), on("logo_keep")
	g.Frame, g.FrameText, g.FrameFont = v("frame"), v("frame_text"), v("frame_font")
	g.FrameColor, g.FrameTextColor = v("frame_color"), v("frame_text_color")
	*g = g.Normalize()

	e := &d.Export
	if f := v("format"); f != "" {
		e.Format = f
	}
	if n := num("size"); n != 0 {
		e.Size = n
	}
	if n := num("quality"); n != 0 {
		e.Quality = n
	}
	if p := v("page"); p != "" {
		e.Page = p
	}
	if mm, err := strconv.ParseFloat(v("print_mm"), 64); err == nil {
		e.PrintMM = mm
	}
}

// request turns the editor state into a generate request. Only the current
// type's non-empty fields are sent, so values left over from switching
// types are ignored.
func (s *Server) request(r *http.Request, d *editorData) generate.Request {
	fields := map[string]string{}
	for _, f := range d.Type.Fields {
		if v := strings.TrimSpace(d.Values[f.Name]); v != "" {
			fields[f.Name] = v
		}
	}
	return generate.Request{Type: d.Type.Name, Fields: fields, Design: d.Design, Assets: s.assetLoader(r.Context())}
}

func (s *Server) makePreview(r *http.Request, d *editorData) preview {
	req := s.request(r, d)
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
	// Generated by our renderer from validated values; logos are embedded
	// as re-encoded PNG data, never as user markup.
	p.SVG = template.HTML(buf.String())
	rep := code.Verify(d.Export.checkPx())
	p.Warnings, p.OK = rep.Warnings, rep.OK()
	p.Summary, p.Bytes, p.Notes = code.Summary(), len(code.Payload), rep.Notes
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
	if err := s.pages["preview"].ExecuteTemplate(w, "preview", s.makePreview(r, d)); err != nil {
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
	invalid := func(msg string) { d.Error, status = msg, http.StatusUnprocessableEntity }

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

	case action == "upload-logo":
		id, err := s.uploadLogo(r)
		if err != nil {
			invalid(err.Error())
			break
		}
		d.Design.Logo, d.Flash = "asset:"+id, "Logo uploaded."

	case action == "save-preset":
		name := strings.TrimSpace(r.PostFormValue("preset_name"))
		switch err := d.Design.Validate(); {
		case name == "":
			invalid("Give the preset a name.")
		case err != nil:
			invalid("Cannot save preset: " + err.Error())
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
		if err := s.download(w, r, d); err != nil {
			invalid(err.Error())
			break
		}
		return

	case action == "save", action == "save-new":
		req := s.request(r, d)
		if _, err := generate.Prepare(req); err != nil {
			invalid("Cannot save: " + err.Error())
			break
		}
		c := &store.Code{
			ID:     id,
			Name:   d.Name,
			Type:   d.Type.Name,
			Fields: req.Fields,
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

	d.Preview = s.makePreview(r, d)
	s.render(w, status, "editor", d)
}

// uploadLogo stores the posted logo file as a normalized PNG asset.
func (s *Server) uploadLogo(r *http.Request) (string, error) {
	f, _, err := r.FormFile("logo_file")
	if err != nil {
		return "", errors.New("choose a PNG, JPEG or SVG file to upload")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, logo.MaxBytes+1))
	if err != nil {
		return "", err
	}
	img, err := logo.Decode(data)
	if err != nil {
		return "", err
	}
	normalized, err := logo.EncodePNG(img)
	if err != nil {
		return "", err
	}
	return s.store.SaveAsset(r.Context(), "logo", "image/png", normalized)
}

// download writes the code as an attachment, or returns why it cannot.
func (s *Server) download(w http.ResponseWriter, r *http.Request, d *editorData) error {
	opts, err := d.Export.options()
	if err != nil {
		return err
	}
	req := s.request(r, d)
	code, err := generate.Prepare(req)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, opts); err != nil {
		return err
	}
	name := d.Name
	if name == "" {
		name = defaultName(d.Type, req.Fields)
	}
	writeImage(w, buf.Bytes(), opts.Format, slug(name)+"."+opts.Format.Ext())
	return nil
}

func writeImage(w http.ResponseWriter, b []byte, f render.Format, attachmentName string) {
	w.Header().Set("Content-Type", f.ContentType())
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

// Radio values for shape pickers; the design stores "" for square.
func (d editorData) ModuleValue() string   { return orSquare(d.Design.Module) }
func (d editorData) EyeOuterValue() string { return orSquare(d.Design.EyeOuter) }
func (d editorData) EyeInnerValue() string { return orSquare(d.Design.EyeInner) }

func orSquare(v string) string {
	if v == "" {
		return "square"
	}
	return v
}
