package web

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/store"
	"github.com/Henk476/qr-generator/internal/verify"
)

type testApp struct {
	t     *testing.T
	srv   *Server
	store *store.Store
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	// httptest requests use Host "example.com".
	srv, err := New(context.Background(), st, Options{AllowedHosts: []string{"example.com"}, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return &testApp{t: t, srv: srv, store: st}
}

func (a *testApp) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.srv.ServeHTTP(rec, req)
	return rec
}

func (a *testApp) get(path string) *httptest.ResponseRecorder {
	return a.do(httptest.NewRequest(http.MethodGet, path, nil))
}

// post submits form values with a valid CSRF token.
func (a *testApp) post(path string, v url.Values) *httptest.ResponseRecorder {
	if v == nil {
		v = url.Values{}
	}
	if !v.Has("csrf") {
		v.Set("csrf", a.srv.csrf)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return a.do(req)
}

// editorForm is a complete editor submission for a URL code.
func editorForm(action string, extra ...string) url.Values {
	v := url.Values{
		"type": {"url"}, "f.url": {"example.com"}, "ecc": {"M"}, "quiet": {"4"}, "version": {"0"},
		"fg": {"#1b2a49"}, "bg": {"#ffffff"}, "format": {"png"}, "size": {"512"}, "action": {action},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return v
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, code int) {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status %d, want %d; body: %.300s", rec.Code, code, rec.Body.String())
	}
}

func wantBody(t *testing.T, rec *httptest.ResponseRecorder, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(rec.Body.String(), s) {
			t.Errorf("body missing %q", s)
		}
	}
}

func TestEditorPages(t *testing.T) {
	a := newTestApp(t)

	rec := a.get("/")
	wantStatus(t, rec, http.StatusSeeOther)
	if loc := rec.Header().Get("Location"); loc != "/new" {
		t.Errorf("redirect to %q", loc)
	}

	rec = a.get("/new")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `name="csrf" value="`+a.srv.csrf, "Website address", "Fill in the content", `aria-pressed="true">Link<`)
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP = %q", csp)
	}

	rec = a.get("/new?type=wifi&preset=midnight")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Network name (SSID)", `name="f.hidden" value="true"`, `value="#1b2a49"`)

	wantStatus(t, a.get("/static/app.css"), http.StatusOK)
	wantStatus(t, a.get("/codes/nope"), http.StatusNotFound)
}

func TestPreview(t *testing.T) {
	a := newTestApp(t)

	rec := a.post("/preview", editorForm("preview"))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<svg", `fill="#1b2a49"`, "Scan check passed", "ECC M")
	if strings.Contains(rec.Body.String(), "<html") {
		t.Error("preview should be a fragment")
	}

	rec = a.post("/preview", editorForm("preview", "f.url", ""))
	wantBody(t, rec, "Fill in the content")

	rec = a.post("/preview", editorForm("preview", "type", "call", "f.phone", "not a number"))
	wantBody(t, rec, "invalid phone number")

	rec = a.post("/preview", editorForm("preview", "fg", "#eeeeee"))
	wantBody(t, rec, "low contrast")

	rec = a.post("/preview", editorForm("preview", "bg_transparent", "1"))
	wantBody(t, rec, "qr checker")
}

func TestTypeSwitchKeepsDesign(t *testing.T) {
	a := newTestApp(t)
	rec := a.post("/new", editorForm("type:sms", "f.phone", "+31612345678", "ecc", "H"))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `aria-pressed="true">SMS<`, `value="&#43;31612345678"`, `value="#1b2a49"`, `value="H" selected`)
}

func TestDownload(t *testing.T) {
	a := newTestApp(t)
	rec := a.post("/new", editorForm("download", "name", "My Site!"))
	wantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="my-site.png"` {
		t.Errorf("Content-Disposition %q", cd)
	}
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := verify.Decode(img); err != nil || got != "https://example.com" {
		t.Errorf("decoded %q, %v", got, err)
	}

	rec = a.post("/new", editorForm("download", "format", "svg"))
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="link-example-com.svg"`) {
		t.Errorf("default name Content-Disposition %q", cd)
	}

	rec = a.post("/new", editorForm("download", "size", "20"))
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "size must be")
}

func TestSaveEditLibrary(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	// Invalid content is not saved.
	rec := a.post("/new", editorForm("save", "f.url", "javascript://x"))
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "Cannot save")

	rec = a.post("/new", editorForm("save", "name", "<b>Shop</b>", "tags", "work, promo"))
	wantStatus(t, rec, http.StatusSeeOther)
	loc := rec.Header().Get("Location")
	id := strings.TrimSuffix(strings.TrimPrefix(loc, "/codes/"), "?saved")
	c, err := a.store.GetCode(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "<b>Shop</b>" || c.Fields["url"] != "example.com" || c.Design.Foreground != "#1b2a49" || len(c.Tags) != 2 {
		t.Fatalf("saved code %+v", c)
	}

	rec = a.get(loc)
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Saved to your library", "Save changes", "&lt;b&gt;Shop&lt;/b&gt;")

	rec = a.get("/library")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "&lt;b&gt;Shop&lt;/b&gt;", "/codes/"+id+"/image", `href="/library?tag=work"`)
	if strings.Contains(rec.Body.String(), "<b>Shop</b>") {
		t.Error("name not escaped")
	}
	wantBody(t, a.get("/library?q=nomatch"), "No codes match")
	wantBody(t, a.get("/library?tag=promo"), "/codes/"+id)

	rec = a.get("/codes/" + id + "/image")
	wantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("image Content-Type %q", ct)
	}
	rec = a.get("/codes/" + id + "/image?format=png&size=256&download")
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="b-shop-b.png"` {
		t.Errorf("Content-Disposition %q", cd)
	}

	// Update in place, then save a copy.
	rec = a.post("/codes/"+id, editorForm("save", "name", "Shop", "f.url", "shop.example.com"))
	wantStatus(t, rec, http.StatusSeeOther)
	c, _ = a.store.GetCode(ctx, id)
	if c.Name != "Shop" || c.Fields["url"] != "shop.example.com" {
		t.Errorf("after update %+v", c)
	}
	rec = a.post("/codes/"+id, editorForm("save-new", "name", "Shop 2"))
	wantStatus(t, rec, http.StatusSeeOther)
	if strings.Contains(rec.Header().Get("Location"), id) {
		t.Error("save-new should create a new code")
	}

	wantStatus(t, a.post("/codes/"+id+"/duplicate", nil), http.StatusSeeOther)
	all, _ := a.store.ListCodes(ctx, store.ListOptions{})
	if len(all) != 3 {
		t.Fatalf("%d codes, want 3", len(all))
	}
	wantStatus(t, a.post("/codes/"+id+"/delete", nil), http.StatusSeeOther)
	wantStatus(t, a.get("/codes/"+id), http.StatusNotFound)
	wantStatus(t, a.post("/codes/"+id+"/delete", nil), http.StatusNotFound)
	wantStatus(t, a.post("/codes/"+id, editorForm("save")), http.StatusNotFound)
}

func TestPresets(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()

	rec := a.post("/new", editorForm("save-preset", "preset_name", ""))
	wantStatus(t, rec, http.StatusUnprocessableEntity)

	rec = a.post("/new", editorForm("save-preset", "preset_name", "Navy", "ecc", "Q"))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `Preset &#34;Navy&#34; saved`, "Navy (yours)")
	ps, _ := a.store.ListPresets(ctx)
	mine := ps[len(ps)-1]
	if mine.Name != "Navy" || mine.Design.ECC != "Q" || mine.Design.Foreground != "#1b2a49" {
		t.Fatalf("saved preset %+v", mine)
	}

	rec = a.post("/new", editorForm("apply-preset", "preset", "ocean"))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `value="#0b5d7a"`, `value="example.com"`)

	rec = a.get("/presets")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Classic", "Navy", "/presets/"+mine.ID+"/delete")
	if strings.Contains(rec.Body.String(), "/presets/classic/delete") {
		t.Error("built-in preset offers delete")
	}
	rec = a.get("/presets/classic/image")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<svg")

	wantStatus(t, a.post("/presets/classic/delete", nil), http.StatusForbidden)
	wantStatus(t, a.post("/presets/"+mine.ID+"/delete", nil), http.StatusSeeOther)
	wantStatus(t, a.post("/presets/"+mine.ID+"/delete", nil), http.StatusNotFound)
}

func TestSettings(t *testing.T) {
	a := newTestApp(t)
	rec := a.post("/settings", url.Values{"theme": {"dark"}, "format": {"svg"}, "size": {"2048"}, "ecc": {"Q"}})
	wantStatus(t, rec, http.StatusSeeOther)

	rec = a.get("/new")
	wantBody(t, rec, `data-theme="dark"`, `value="svg" selected`, `value="2048"`, `value="Q" selected`)

	rec = a.post("/settings", url.Values{"theme": {""}, "format": {"svg"}, "size": {"99999"}, "ecc": {"Q"}})
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "Size must be")
	rec = a.post("/settings", url.Values{"theme": {"neon"}, "format": {"png"}, "size": {"512"}, "ecc": {"M"}})
	wantStatus(t, rec, http.StatusUnprocessableEntity)
}

func TestSecurity(t *testing.T) {
	a := newTestApp(t)

	rec := a.post("/new", url.Values{"csrf": {"wrong"}, "action": {"save"}})
	wantStatus(t, rec, http.StatusForbidden)
	rec = a.post("/preview", url.Values{"csrf": {""}})
	wantStatus(t, rec, http.StatusForbidden)

	req := httptest.NewRequest(http.MethodPost, "/codes/x/delete", strings.NewReader("csrf="+a.srv.csrf))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	wantStatus(t, a.do(req), http.StatusForbidden)

	req = httptest.NewRequest(http.MethodGet, "/new", nil)
	req.Host = "evil.example:8080"
	wantStatus(t, a.do(req), http.StatusMisdirectedRequest)

	// The token survives a restart.
	srv2, err := New(context.Background(), a.store, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if srv2.csrf != a.srv.csrf {
		t.Error("CSRF token changed across restarts")
	}
}

func TestDefaultNameAndSlug(t *testing.T) {
	tests := []struct{ typ, want string }{
		{"url", "Link: example.com"},
		{"vcard", "vCard: Canute Knutsson"},
	}
	fields := map[string]string{"url": "example.com", "first": "Canute", "last": "Knutsson"}
	for _, tt := range tests {
		ty, _ := encode.Lookup(tt.typ)
		if got := generate.DefaultName(ty, fields); got != tt.want {
			t.Errorf("defaultName(%s) = %q, want %q", tt.typ, got, tt.want)
		}
	}
	for in, want := range map[string]string{"Hello, World!": "hello-world", "日本": "qr-code", "--a--": "a"} {
		if got := generate.Slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// multipartPost submits fields plus an optional file as multipart/form-data.
func (a *testApp) multipartPost(path string, v url.Values, fileField, fileName string, file []byte) *httptest.ResponseRecorder {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if !v.Has("csrf") {
		v.Set("csrf", a.srv.csrf)
	}
	for k, vs := range v {
		for _, x := range vs {
			mw.WriteField(k, x)
		}
	}
	if fileField != "" {
		fw, _ := mw.CreateFormFile(fileField, fileName)
		fw.Write(file)
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return a.do(req)
}

func TestDesignOptionsInPreview(t *testing.T) {
	a := newTestApp(t)
	rec := a.post("/preview", editorForm("preview",
		"module", "dots", "eye_outer", "circle", "eye_inner", "star",
		"gradient", "linear", "fg2", "#0b5d7a", "gradient_angle", "45",
		"eye_inner_custom", "1", "eye_inner_color", "#c02020",
		"logo", "icon:wifi", "logo_size", "25", "logo_circle", "1",
		"frame", "badge", "frame_text", "SCAN THE TIDE", "frame_font", "mono"))
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "<linearGradient", `fill="#c02020"`, "Scan check passed", "ECC H")
}

func TestEditorRendersPickers(t *testing.T) {
	a := newTestApp(t)
	rec := a.get("/new?preset=tide")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, `name="module" value="rounded" checked`, `name="gradient"`, `value="linear" selected`,
		`name="logo" value="icon:wifi"`, `data-when="frame"`, `value="pdf"`, `enctype="multipart/form-data"`)
}

func TestLogoUpload(t *testing.T) {
	a := newTestApp(t)
	logoImg := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := range logoImg.Pix {
		logoImg.Pix[i] = 0xc0
	}
	var pngBytes bytes.Buffer
	png.Encode(&pngBytes, logoImg)

	rec := a.multipartPost("/new", editorForm("upload-logo"), "logo_file", "logo.png", pngBytes.Bytes())
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Logo uploaded", "Uploaded image", "<image ")
	m := regexp.MustCompile(`name="logo" value="(asset:[0-9a-f]+)" checked`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("uploaded logo not selected")
	}

	// Saving keeps the logo reference, and the library image renders it.
	rec = a.post("/new", editorForm("save", "logo", m[1]))
	wantStatus(t, rec, http.StatusSeeOther)
	id := strings.TrimSuffix(strings.TrimPrefix(rec.Header().Get("Location"), "/codes/"), "?saved")
	c, _ := a.store.GetCode(context.Background(), id)
	if c.Design.Logo != m[1] || c.Design.LogoSize != 0 {
		t.Errorf("saved design %+v", c.Design)
	}
	rec = a.get("/codes/" + id + "/image?format=svg")
	wantBody(t, rec, "data:image/png;base64,")

	// Rejected uploads explain why.
	rec = a.multipartPost("/new", editorForm("upload-logo"), "logo_file", "x.gif", []byte("GIF89a....."))
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "unsupported logo type")
	rec = a.multipartPost("/new", editorForm("upload-logo"), "", "", nil)
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "choose a PNG")

	// An SVG upload is rasterized: no SVG markup comes back.
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><script>alert(1)</script><rect width="10" height="10"/></svg>`
	rec = a.multipartPost("/new", editorForm("upload-logo"), "logo_file", "x.svg", []byte(svg))
	wantStatus(t, rec, http.StatusOK)
	if strings.Contains(rec.Body.String(), "alert(1)") {
		t.Error("uploaded SVG markup leaked into the page")
	}
}

func TestDownloadFormats(t *testing.T) {
	a := newTestApp(t)
	tests := []struct {
		format, ctype, ext, magic string
	}{
		{"jpeg", "image/jpeg", "jpg", "\xff\xd8"},
		{"pdf", "application/pdf", "pdf", "%PDF-1.4"},
		{"eps", "application/postscript", "eps", "%!PS-Adobe-3.0 EPSF-3.0"},
	}
	for _, tt := range tests {
		rec := a.post("/new", editorForm("download", "format", tt.format, "frame", "border", "name", "Shop"))
		wantStatus(t, rec, http.StatusOK)
		if ct := rec.Header().Get("Content-Type"); ct != tt.ctype {
			t.Errorf("%s: Content-Type %q", tt.format, ct)
		}
		if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="shop.`+tt.ext+`"` {
			t.Errorf("%s: Content-Disposition %q", tt.format, cd)
		}
		if !strings.HasPrefix(rec.Body.String(), tt.magic) {
			t.Errorf("%s: body starts %q", tt.format, rec.Body.String()[:10])
		}
	}
	rec := a.post("/new", editorForm("download", "format", "pdf", "page", "a4", "print_mm", "400"))
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "does not fit")
}

func TestBarcodeEditor(t *testing.T) {
	a := newTestApp(t)
	rec := a.get("/new?type=barcode")
	wantStatus(t, rec, http.StatusOK)
	wantBody(t, rec, "Barcode format", "shapes, logos and frames are QR features")
	if strings.Contains(rec.Body.String(), `name="module"`) {
		t.Error("QR shape pickers shown for a barcode")
	}
	rec = a.post("/preview", editorForm("preview", "type", "barcode", "f.symbology", "upca", "f.value", "03600029145"))
	wantBody(t, rec, "<svg", "UPC-A, 95 modules wide", "Scan check passed")
	rec = a.post("/preview", editorForm("preview", "type", "barcode", "f.symbology", "pdf417", "f.value", "HELLO"))
	wantBody(t, rec, "not decode-checked")
}

func TestBulk(t *testing.T) {
	a := newTestApp(t)
	wantBody(t, a.get("/bulk"), "Create ZIP", "<strong>wifi</strong>: ssid, password")

	csvData := []byte("type,name,url,ssid,password\nurl,Shop,example.com,,\nwifi,Guest,,Guest,secret\nurl,Bad,javascript://x,,\n")
	form := url.Values{"type": {"url"}, "preset": {"tide"}, "format": {"svg"}, "size": {"256"}, "print_mm": {"50"}, "save": {"1"}, "tags": {"bulk, test"}}
	rec := a.multipartPost("/bulk", form, "csv_file", "codes.csv", csvData)
	wantStatus(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type %q", ct)
	}
	if s := rec.Header().Get("X-Canute-Summary"); s != "ok=2 warnings=0 failed=1" {
		t.Errorf("summary %q", s)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 3 || zr.File[0].Name != "shop.svg" {
		t.Errorf("zip files: %v", zr.File)
	}
	codes, _ := a.store.ListCodes(context.Background(), store.ListOptions{Tag: "bulk"})
	if len(codes) != 2 || codes[0].Design.Gradient != "linear" {
		t.Errorf("saved %d codes: %+v", len(codes), codes)
	}

	rec = a.multipartPost("/bulk", url.Values{"type": {"url"}, "preset": {"classic"}, "format": {"png"}, "size": {"256"}},
		"csv_file", "x.csv", []byte("colour\nred\n"))
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "unknown CSV columns: colour")
}

func TestBackupAndRestore(t *testing.T) {
	a := newTestApp(t)
	wantStatus(t, a.post("/new", editorForm("save", "name", "Keep me")), http.StatusSeeOther)
	rec := a.get("/backup")
	wantStatus(t, rec, http.StatusOK)
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "canute-qr-backup-") {
		t.Errorf("Content-Disposition %q", cd)
	}
	archive := rec.Body.Bytes()

	b := newTestApp(t)
	wantStatus(t, b.post("/new", editorForm("save", "name", "Replaced")), http.StatusSeeOther)
	rec = b.multipartPost("/restore", url.Values{}, "backup_file", "backup.zip", archive)
	wantStatus(t, rec, http.StatusSeeOther)
	rec = b.get(rec.Header().Get("Location"))
	wantBody(t, rec, "Backup restored: 1 codes")
	wantBody(t, b.get("/library"), "Keep me")
	if strings.Contains(b.get("/library").Body.String(), "Replaced") {
		t.Error("old code survived the restore")
	}

	rec = b.multipartPost("/restore", url.Values{}, "backup_file", "x.zip", []byte("not a zip"))
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	wantBody(t, rec, "Restore failed: not a backup file")
}
