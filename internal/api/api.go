// Package api is the local JSON REST API, served under /api/v1.
//
// Every request needs "Authorization: Bearer <token>", with the token shown
// in the web UI's Settings page. The OpenAPI description is served at
// /api/v1/openapi.json without a token.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Henk476/qr-generator/internal/batch"
	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/logo"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/store"
)

//go:embed openapi.json
var openapiJSON []byte

// Prefix is where the API is mounted.
const Prefix = "/api/v1"

// Options configures the API.
type Options struct {
	Token  func() string                              // the current API token
	Assets func(context.Context) generate.AssetLoader // resolves uploaded logos
	Fonts  func(context.Context) generate.FontLoader  // resolves uploaded fonts
}

// API serves the REST endpoints.
type API struct {
	store *store.Store
	opts  Options
	mux   *http.ServeMux
}

// New builds the API handler; mount it at Prefix.
func New(st *store.Store, opts Options) *API {
	a := &API{store: st, opts: opts, mux: http.NewServeMux()}
	routes := map[string]http.HandlerFunc{
		"GET /openapi.json":     a.openapi,
		"GET /types":            a.types,
		"GET /styles":           a.styles,
		"POST /render":          a.render,
		"POST /check":           a.check,
		"GET /codes":            a.listCodes,
		"POST /codes":           a.createCode,
		"GET /codes/{id}":       a.getCode,
		"PATCH /codes/{id}":     a.updateCode,
		"DELETE /codes/{id}":    a.deleteCode,
		"GET /codes/{id}/image": a.codeImage,
		"GET /presets":          a.listPresets,
		"POST /presets":         a.createPreset,
		"DELETE /presets/{id}":  a.deletePreset,
		"POST /assets":          a.uploadAsset,
		"POST /fonts":           a.uploadFont,
		"POST /batch":           a.batch,
	}
	for pattern, h := range routes {
		method, path, _ := strings.Cut(pattern, " ")
		a.mux.HandleFunc(method+" "+Prefix+path, h)
	}
	a.mux.HandleFunc(Prefix+"/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint; see "+Prefix+"/openapi.json")
	})
	return a
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != Prefix+"/openapi.json" && !a.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="canute-qr"`)
		writeError(w, http.StatusUnauthorized, "missing or wrong API token: send Authorization: Bearer <token> (see Settings in the web UI)")
		return
	}
	a.mux.ServeHTTP(w, r)
}

func (a *API) authorized(r *http.Request) bool {
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	want := a.opts.Token()
	return ok && want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// readJSON decodes a JSON body strictly: unknown fields are errors.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "send a JSON body with Content-Type: application/json")
		return false
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func (a *API) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrBuiltin):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// exportSpec is the JSON form of render.Options.
type exportSpec struct {
	Format  string  `json:"format"`
	Size    int     `json:"size,omitempty"`
	Quality int     `json:"quality,omitempty"`
	Page    string  `json:"page,omitempty"`
	PrintMM float64 `json:"printMM,omitempty"`
}

func (e exportSpec) options() (render.Options, error) {
	if e.Format == "" {
		e.Format = "png"
	}
	f, err := render.ParseFormat(e.Format)
	if err != nil {
		return render.Options{}, err
	}
	if e.Size == 0 {
		e.Size = 1024
	}
	return render.Options{Format: f, Size: e.Size, Quality: e.Quality, Page: e.Page, PrintMM: e.PrintMM}, nil
}

// exportFromQuery reads export options from URL parameters.
func exportFromQuery(r *http.Request) exportSpec {
	q := r.URL.Query()
	e := exportSpec{Format: q.Get("format"), Page: q.Get("page")}
	e.Size, _ = strconv.Atoi(q.Get("size"))
	e.Quality, _ = strconv.Atoi(q.Get("quality"))
	e.PrintMM, _ = strconv.ParseFloat(q.Get("printMM"), 64)
	return e
}

// mergeDesign decodes a partial design JSON onto base: omitted fields keep
// base's values (the defaults when creating, the current design for PATCH).
func mergeDesign(base generate.Design, raw json.RawMessage) (generate.Design, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return base, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&base); err != nil {
		return base, fmt.Errorf("invalid design: %w", err)
	}
	return base.Normalize(), nil
}

func writeImage(w http.ResponseWriter, b []byte, f render.Format) {
	w.Header().Set("Content-Type", f.ContentType())
	w.Write(b)
}

// ---- reference data ----

func (a *API) openapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(openapiJSON)
}

type fieldJSON struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Help     string   `json:"help"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

type typeJSON struct {
	Name    string      `json:"name"`
	Title   string      `json:"title"`
	Primary string      `json:"primary,omitempty"`
	Fields  []fieldJSON `json:"fields"`
}

func (a *API) types(w http.ResponseWriter, r *http.Request) {
	var out []typeJSON
	for _, t := range encode.Types() {
		tj := typeJSON{Name: t.Name, Title: t.Title, Primary: t.Primary}
		for _, f := range t.Fields {
			tj.Fields = append(tj.Fields, fieldJSON{f.Name, f.Label, f.Usage, f.Required, f.Options})
		}
		out = append(out, tj)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) styles(w http.ResponseWriter, r *http.Request) {
	icons := render.Icons()
	for i := range icons {
		icons[i].Value = "icon:" + icons[i].Value
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modules": render.ModuleShapes, "eyeOuter": render.EyeOuterShapes, "eyeInner": render.EyeInnerShapes,
		"frames": render.Frames, "fonts": render.Fonts, "icons": icons, "pages": render.Pages,
		"formats": render.Formats(),
	})
}

// ---- stateless rendering ----

type renderRequest struct {
	Type   string            `json:"type"`
	Fields map[string]string `json:"fields"`
	Design json.RawMessage   `json:"design,omitempty"`
	Export exportSpec        `json:"export"`
}

func (a *API) prepare(w http.ResponseWriter, r *http.Request, req renderRequest) (*generate.Code, bool) {
	design, err := mergeDesign(generate.DefaultDesign(), req.Design)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return a.prepareDesign(w, r, req.Type, req.Fields, design)
}

func (a *API) prepareDesign(w http.ResponseWriter, r *http.Request, typ string, fields map[string]string, design generate.Design) (*generate.Code, bool) {
	code, err := generate.Prepare(generate.Request{
		Type: typ, Fields: fields, Design: design, Assets: a.opts.Assets(r.Context()), Fonts: a.opts.Fonts(r.Context()),
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return nil, false
	}
	return code, true
}

func (a *API) render(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if !readJSON(w, r, &req) {
		return
	}
	opts, err := req.Export.options()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	code, ok := a.prepare(w, r, req)
	if !ok {
		return
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, opts); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeImage(w, buf.Bytes(), opts.Format)
}

type checkResponse struct {
	Payload  string   `json:"payload"`
	Summary  string   `json:"summary"`
	Decoded  bool     `json:"decoded"`
	OK       bool     `json:"ok"`
	Contrast float64  `json:"contrast"`
	Warnings []string `json:"warnings"`
	Notes    []string `json:"notes"`
}

// check validates content and design and runs the scannability check.
func (a *API) check(w http.ResponseWriter, r *http.Request) {
	var req renderRequest
	if !readJSON(w, r, &req) {
		return
	}
	code, ok := a.prepare(w, r, req)
	if !ok {
		return
	}
	size := req.Export.Size
	if size == 0 {
		size = 1024
	}
	rep := code.Verify(size)
	writeJSON(w, http.StatusOK, checkResponse{
		Payload: code.Payload, Summary: code.Summary(), Decoded: rep.Decoded, OK: rep.OK(),
		Contrast: rep.Contrast, Warnings: nonNil(rep.Warnings), Notes: nonNil(rep.Notes),
	})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---- saved codes ----

type codeJSON struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Fields    map[string]string `json:"fields"`
	Design    generate.Design   `json:"design"`
	Tags      []string          `json:"tags"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

func toJSON(c *store.Code) codeJSON {
	return codeJSON{c.ID, c.Name, c.Type, c.Fields, c.Design, nonNil(c.Tags), c.CreatedAt, c.UpdatedAt}
}

func (a *API) listCodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	codes, err := a.store.ListCodes(r.Context(), store.ListOptions{Query: q.Get("q"), Type: q.Get("type"), Tag: q.Get("tag"), Sort: q.Get("sort")})
	if err != nil {
		a.storeError(w, err)
		return
	}
	out := make([]codeJSON, len(codes))
	for i := range codes {
		out[i] = toJSON(&codes[i])
	}
	writeJSON(w, http.StatusOK, out)
}

type codeInput struct {
	Name   *string            `json:"name,omitempty"`
	Type   *string            `json:"type,omitempty"`
	Fields *map[string]string `json:"fields,omitempty"`
	Design json.RawMessage    `json:"design,omitempty"`
	Tags   *[]string          `json:"tags,omitempty"`
}

// apply merges in into c and validates the result by preparing it.
func (a *API) apply(ctx context.Context, c *store.Code, in codeInput) error {
	if in.Type != nil {
		c.Type = *in.Type
	}
	if in.Fields != nil {
		c.Fields = *in.Fields
	}
	var err error
	if c.Design, err = mergeDesign(c.Design, in.Design); err != nil {
		return err
	}
	if in.Tags != nil {
		c.Tags = *in.Tags
	}
	if in.Name != nil {
		c.Name = strings.TrimSpace(*in.Name)
	}
	if c.Fields == nil {
		c.Fields = map[string]string{}
	}
	if _, err := generate.Prepare(generate.Request{Type: c.Type, Fields: c.Fields, Design: c.Design, Assets: a.opts.Assets(ctx), Fonts: a.opts.Fonts(ctx)}); err != nil {
		return err
	}
	if c.Name == "" {
		t, _ := encode.Lookup(c.Type)
		c.Name = generate.DefaultName(t, c.Fields)
	}
	return nil
}

func (a *API) createCode(w http.ResponseWriter, r *http.Request) {
	var in codeInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Type == nil {
		writeError(w, http.StatusUnprocessableEntity, `"type" is required`)
		return
	}
	c := &store.Code{Design: generate.DefaultDesign()}
	if err := a.apply(r.Context(), c, in); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := a.store.CreateCode(r.Context(), c); err != nil {
		a.storeError(w, err)
		return
	}
	w.Header().Set("Location", Prefix+"/codes/"+c.ID)
	writeJSON(w, http.StatusCreated, toJSON(c))
}

func (a *API) getCode(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.GetCode(r.Context(), r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toJSON(c))
}

func (a *API) updateCode(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.GetCode(r.Context(), r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	var in codeInput
	if !readJSON(w, r, &in) {
		return
	}
	if err := a.apply(r.Context(), c, in); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := a.store.UpdateCode(r.Context(), c); err != nil {
		a.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toJSON(c))
}

func (a *API) deleteCode(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteCode(r.Context(), r.PathValue("id")); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) codeImage(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.GetCode(r.Context(), r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	opts, err := exportFromQuery(r).options()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	code, ok := a.prepareDesign(w, r, c.Type, c.Fields, c.Design)
	if !ok {
		return
	}
	var buf bytes.Buffer
	if err := code.Export(&buf, opts); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if r.URL.Query().Has("download") {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, generate.Slug(c.Name), opts.Format.Ext()))
	}
	writeImage(w, buf.Bytes(), opts.Format)
}

// ---- presets ----

type presetJSON struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Design  generate.Design `json:"design"`
	Builtin bool            `json:"builtin"`
}

func (a *API) listPresets(w http.ResponseWriter, r *http.Request) {
	ps, err := a.store.ListPresets(r.Context())
	if err != nil {
		a.storeError(w, err)
		return
	}
	out := make([]presetJSON, len(ps))
	for i, p := range ps {
		out[i] = presetJSON{p.ID, p.Name, p.Design, p.Builtin}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) createPreset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string          `json:"name"`
		Design json.RawMessage `json:"design"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, `"name" is required`)
		return
	}
	design, err := mergeDesign(generate.DefaultDesign(), in.Design)
	if err == nil {
		err = design.Validate()
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	p := &store.Preset{Name: in.Name, Design: design}
	if err := a.store.CreatePreset(r.Context(), p); err != nil {
		a.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, presetJSON{p.ID, p.Name, p.Design, false})
}

func (a *API) deletePreset(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeletePreset(r.Context(), r.PathValue("id")); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- uploads and batch ----

// uploadAsset accepts a logo as multipart field "file", or as the raw body.
func (a *API) uploadAsset(w http.ResponseWriter, r *http.Request) {
	var src io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		f, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, `send the logo as multipart field "file" or as the request body`)
			return
		}
		defer f.Close()
		src = f
	}
	data, err := io.ReadAll(io.LimitReader(src, logo.MaxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	img, err := logo.Decode(data)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	normalized, err := logo.EncodePNG(img)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := a.store.SaveAsset(r.Context(), "logo", "image/png", normalized)
	if err != nil {
		a.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "logo": "asset:" + id})
}

// batch takes multipart fields: file (CSV), type, map, preset or design
// (JSON), and export options as in /render's "export" (JSON field
// "export"). It returns a ZIP with the images and report.csv.
func (a *API) batch(w http.ResponseWriter, r *http.Request) {
	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `send the CSV as multipart field "file"`)
		return
	}
	defer f.Close()
	design := generate.DefaultDesign()
	if id := r.FormValue("preset"); id != "" {
		p, err := a.store.GetPreset(r.Context(), id)
		if err != nil {
			a.storeError(w, err)
			return
		}
		design = p.Design
	}
	if d := r.FormValue("design"); d != "" {
		if design, err = mergeDesign(design, json.RawMessage(d)); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var spec exportSpec
	if e := r.FormValue("export"); e != "" {
		if err := json.Unmarshal([]byte(e), &spec); err != nil {
			writeError(w, http.StatusBadRequest, "invalid export JSON: "+err.Error())
			return
		}
	}
	opts, err := spec.options()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	rename, err := batch.ParseRename(r.FormValue("map"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	rows, err := batch.Parse(io.LimitReader(f, 5<<20), batch.ParseOptions{DefaultType: r.FormValue("type"), Rename: rename})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var buf bytes.Buffer
	results, err := batch.Generate(&buf, rows, batch.Options{Design: design, Assets: a.opts.Assets(r.Context()), Fonts: a.opts.Fonts(r.Context()), Export: opts, Verify: true})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ok, warned, failed := batch.Summary(results)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Canute-Summary", fmt.Sprintf("ok=%d warnings=%d failed=%d", ok, warned, failed))
	w.Write(buf.Bytes())
}

// uploadFont accepts a TTF or OTF file as multipart field "file", or as the
// raw body, and returns the value to use as a design's frameFont.
func (a *API) uploadFont(w http.ResponseWriter, r *http.Request) {
	var src io.Reader = r.Body
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		f, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, `send the font as multipart field "file" or as the request body`)
			return
		}
		defer f.Close()
		src = f
	}
	data, err := io.ReadAll(io.LimitReader(src, render.MaxFontBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	font, err := render.ParseFont(data)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	id, err := a.store.SaveAsset(r.Context(), "font", "font/sfnt", data)
	if err != nil {
		a.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "font": "asset:" + id, "name": font.Name()})
}
