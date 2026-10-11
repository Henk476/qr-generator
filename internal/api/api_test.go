package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/store"
	"github.com/Henk476/qr-generator/internal/verify"
)

const token = "test-token"

type client struct {
	t   *testing.T
	api *API
	st  *store.Store
}

func newClient(t *testing.T) *client {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fonts := func(ctx context.Context) generate.FontLoader {
		return func(id string) (*render.Font, error) {
			a, err := st.GetAsset(ctx, id)
			if err != nil {
				return nil, err
			}
			return render.ParseFont(a.Data)
		}
	}
	assets := func(ctx context.Context) generate.AssetLoader {
		return func(id string) (image.Image, error) {
			a, err := st.GetAsset(ctx, id)
			if err != nil {
				return nil, err
			}
			return png.Decode(bytes.NewReader(a.Data))
		}
	}
	return &client{t: t, st: st, api: New(st, Options{Token: func() string { return token }, Assets: assets, Fonts: fonts})}
}

func (c *client) do(method, path, contentType string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, Prefix+path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	c.api.ServeHTTP(rec, req)
	return rec
}

func (c *client) json(method, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	return c.do(method, path, "application/json", bytes.NewReader(b))
}

func want(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("bad JSON %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestAuth(t *testing.T) {
	c := newClient(t)
	for _, auth := range []string{"", "Bearer wrong", token} {
		req := httptest.NewRequest(http.MethodGet, Prefix+"/codes", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		c.api.ServeHTTP(rec, req)
		want(t, rec, http.StatusUnauthorized)
		if !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("error body not JSON: %s", rec.Body.String())
		}
	}
	// The OpenAPI document is public and valid JSON.
	rec := httptest.NewRecorder()
	c.api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, Prefix+"/openapi.json", nil))
	want(t, rec, http.StatusOK)
	doc := decode[map[string]any](t, rec)
	if doc["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v", doc["openapi"])
	}
	want(t, c.do(http.MethodGet, "/nope", "", nil), http.StatusNotFound)
}

func TestReferenceData(t *testing.T) {
	c := newClient(t)
	rec := c.do(http.MethodGet, "/types", "", nil)
	want(t, rec, http.StatusOK)
	types := decode[[]typeJSON](t, rec)
	if len(types) != 13 || types[0].Name != "url" || types[0].Fields[0].Name != "url" {
		t.Errorf("types = %+v", types[0])
	}
	rec = c.do(http.MethodGet, "/styles", "", nil)
	want(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), `"icon:wifi"`) {
		t.Error("styles missing icons")
	}
}

func TestRenderAndCheck(t *testing.T) {
	c := newClient(t)
	rec := c.json(http.MethodPost, "/render", map[string]any{
		"type": "wifi", "fields": map[string]string{"ssid": "Home", "password": "secret"},
		"design": map[string]any{"module": "dots", "eyeOuter": "circle"},
		"export": map[string]any{"format": "png", "size": 400},
	})
	want(t, rec, http.StatusOK)
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type %q", ct)
	}
	img, err := png.Decode(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := verify.Decode(img); err != nil || got != "WIFI:T:WPA;S:Home;P:secret;;" {
		t.Errorf("decoded %q, %v", got, err)
	}

	rec = c.json(http.MethodPost, "/check", map[string]any{"type": "url", "fields": map[string]string{"url": "example.com"},
		"design": map[string]any{"fg": "#dddddd"}})
	want(t, rec, http.StatusOK)
	res := decode[checkResponse](t, rec)
	if res.Payload != "https://example.com" || res.OK || len(res.Warnings) == 0 || !strings.Contains(res.Summary, "QR version") {
		t.Errorf("check = %+v", res)
	}

	tests := []struct {
		name, ctype, body string
		status            int
		msg               string
	}{
		{"not JSON content type", "text/plain", `{}`, http.StatusUnsupportedMediaType, "application/json"},
		{"bad JSON", "application/json", `{"type":`, http.StatusBadRequest, "invalid JSON"},
		{"unknown field", "application/json", `{"type":"url","fields":{},"colour":"red"}`, http.StatusBadRequest, "unknown field"},
		{"bad content", "application/json", `{"type":"url","fields":{"url":"javascript://x"}}`, http.StatusUnprocessableEntity, "scheme"},
		{"bad format", "application/json", `{"type":"url","fields":{"url":"a.com"},"export":{"format":"gif"}}`, http.StatusUnprocessableEntity, "unsupported format"},
	}
	for _, tt := range tests {
		rec := c.do(http.MethodPost, "/render", tt.ctype, strings.NewReader(tt.body))
		if rec.Code != tt.status || !strings.Contains(rec.Body.String(), tt.msg) {
			t.Errorf("%s: %d %s", tt.name, rec.Code, rec.Body.String())
		}
	}
}

func TestCodesCRUD(t *testing.T) {
	c := newClient(t)
	rec := c.json(http.MethodPost, "/codes", map[string]any{"type": "url", "fields": map[string]string{"url": "a.com"}, "tags": []string{"api"}})
	want(t, rec, http.StatusCreated)
	created := decode[codeJSON](t, rec)
	if created.Name != "Link: a.com" || created.Design.ECC != "M" || rec.Header().Get("Location") != Prefix+"/codes/"+created.ID {
		t.Errorf("created %+v", created)
	}

	want(t, c.json(http.MethodPost, "/codes", map[string]any{"fields": map[string]string{"url": "a.com"}}), http.StatusUnprocessableEntity)
	want(t, c.json(http.MethodPost, "/codes", map[string]any{"type": "url", "fields": map[string]string{"url": "javascript://x"}}), http.StatusUnprocessableEntity)

	rec = c.json(http.MethodPatch, "/codes/"+created.ID, map[string]any{"name": "Renamed", "design": map[string]any{"ecc": "H", "fg": "#123456", "bg": "#ffffff", "quietZone": 4}})
	want(t, rec, http.StatusOK)
	updated := decode[codeJSON](t, rec)
	if updated.Name != "Renamed" || updated.Fields["url"] != "a.com" || updated.Design.Foreground != "#123456" {
		t.Errorf("updated %+v", updated)
	}

	rec = c.do(http.MethodGet, "/codes?tag=api", "", nil)
	want(t, rec, http.StatusOK)
	if list := decode[[]codeJSON](t, rec); len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("list = %+v", list)
	}
	rec = c.do(http.MethodGet, "/codes/"+created.ID+"/image?format=svg&size=256&download", "", nil)
	want(t, rec, http.StatusOK)
	if !strings.HasPrefix(rec.Body.String(), "<svg") || !strings.Contains(rec.Header().Get("Content-Disposition"), "renamed.svg") {
		t.Errorf("image: %s %.40s", rec.Header().Get("Content-Disposition"), rec.Body.String())
	}

	want(t, c.do(http.MethodDelete, "/codes/"+created.ID, "", nil), http.StatusNoContent)
	want(t, c.do(http.MethodGet, "/codes/"+created.ID, "", nil), http.StatusNotFound)
	want(t, c.json(http.MethodPatch, "/codes/"+created.ID, map[string]any{}), http.StatusNotFound)
}

func TestPresetsAndAssets(t *testing.T) {
	c := newClient(t)
	rec := c.json(http.MethodPost, "/presets", map[string]any{"name": "API look", "design": map[string]any{"ecc": "Q", "fg": "#0b5d7a", "bg": "#ffffff", "quietZone": 4, "module": "dots"}})
	want(t, rec, http.StatusCreated)
	p := decode[presetJSON](t, rec)
	want(t, c.do(http.MethodDelete, "/presets/classic", "", nil), http.StatusForbidden)
	want(t, c.do(http.MethodDelete, "/presets/"+p.ID, "", nil), http.StatusNoContent)
	want(t, c.json(http.MethodPost, "/presets", map[string]any{"name": "", "design": map[string]any{}}), http.StatusUnprocessableEntity)

	logoImg := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	var buf bytes.Buffer
	png.Encode(&buf, logoImg)
	rec = c.do(http.MethodPost, "/assets", "image/png", bytes.NewReader(buf.Bytes()))
	want(t, rec, http.StatusCreated)
	asset := decode[map[string]string](t, rec)
	rec = c.json(http.MethodPost, "/render", map[string]any{"type": "url", "fields": map[string]string{"url": "a.com"},
		"design": map[string]any{"logo": asset["logo"]}, "export": map[string]any{"format": "svg", "size": 256}})
	want(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "data:image/png;base64,") {
		t.Error("uploaded logo not embedded")
	}
	want(t, c.do(http.MethodPost, "/assets", "text/plain", strings.NewReader("nope")), http.StatusUnprocessableEntity)
}

func TestBatch(t *testing.T) {
	c := newClient(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "codes.csv")
	fw.Write([]byte("url,name\nexample.com/a,A\nexample.com/b,B\n"))
	mw.WriteField("type", "url")
	mw.WriteField("preset", "soft-dots")
	mw.WriteField("export", `{"format":"svg","size":256}`)
	mw.Close()
	rec := c.do(http.MethodPost, "/batch", mw.FormDataContentType(), &body)
	want(t, rec, http.StatusOK)
	if s := rec.Header().Get("X-Canute-Summary"); s != "ok=2 warnings=0 failed=0" {
		t.Errorf("summary %q", s)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil || len(zr.File) != 3 || zr.File[0].Name != "a.svg" {
		t.Errorf("zip %v, %v", zr, err)
	}
}

func TestUploadFont(t *testing.T) {
	c := newClient(t)
	data, err := os.ReadFile(filepath.Join("..", "render", "fonts", "LibreBaskerville-Bold.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	rec := c.do(http.MethodPost, "/fonts", "font/ttf", bytes.NewReader(data))
	want(t, rec, http.StatusCreated)
	got := decode[map[string]string](t, rec)
	if !strings.HasPrefix(got["font"], "asset:") || !strings.Contains(got["name"], "Baskerville") {
		t.Fatalf("upload = %v", got)
	}
	rec = c.json(http.MethodPost, "/render", map[string]any{"type": "url", "fields": map[string]string{"url": "a.com"},
		"design": map[string]any{"frame": "label", "frameFont": got["font"], "frameText": "Hello"}, "export": map[string]any{"format": "svg", "size": 256}})
	want(t, rec, http.StatusOK)
	want(t, c.do(http.MethodPost, "/fonts", "font/ttf", strings.NewReader("not a font")), http.StatusUnprocessableEntity)
}
