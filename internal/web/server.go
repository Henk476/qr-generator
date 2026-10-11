// Package web serves the local, server-rendered UI. Pages are plain HTML
// forms that work without JavaScript; static/app.js only adds live preview
// and small conveniences on top.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/logo"
	"github.com/Henk476/qr-generator/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Options configures the server.
type Options struct {
	// AllowedHosts lists the Host header values to accept, e.g.
	// "127.0.0.1:8080". Requests for any other host are rejected, which
	// blocks DNS-rebinding attacks. Empty means accept any host.
	AllowedHosts []string
	Version      string
}

// Server is the web UI.
type Server struct {
	store     *store.Store
	opts      Options
	pages     map[string]*template.Template
	csrf      string
	options   *designOptions
	logoCache sync.Map // asset ID -> decoded image.Image; assets never change
	handler   http.Handler
}

const csrfSettingKey = "csrf_token"

// New builds the UI handler.
func New(ctx context.Context, st *store.Store, opts Options) (*Server, error) {
	s := &Server{store: st, opts: opts, options: newDesignOptions()}
	var err error
	if s.pages, err = parsePages(); err != nil {
		return nil, err
	}
	// The CSRF token is persisted so open tabs keep working across restarts.
	if s.csrf, err = st.GetSetting(ctx, csrfSettingKey, ""); err != nil {
		return nil, err
	}
	if s.csrf == "" {
		b := make([]byte, 32)
		rand.Read(b)
		s.csrf = hex.EncodeToString(b)
		if err := st.SetSetting(ctx, csrfSettingKey, s.csrf); err != nil {
			return nil, err
		}
	}

	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/new", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /new", s.handleNew)
	mux.HandleFunc("POST /new", s.handleEditorPost)
	mux.HandleFunc("POST /preview", s.handlePreview)
	mux.HandleFunc("GET /codes/{id}", s.handleEdit)
	mux.HandleFunc("POST /codes/{id}", s.handleEditorPost)
	mux.HandleFunc("GET /codes/{id}/image", s.handleCodeImage)
	mux.HandleFunc("POST /codes/{id}/delete", s.handleCodeDelete)
	mux.HandleFunc("POST /codes/{id}/duplicate", s.handleCodeDuplicate)
	mux.HandleFunc("GET /library", s.handleLibrary)
	mux.HandleFunc("GET /presets", s.handlePresets)
	mux.HandleFunc("GET /presets/{id}/image", s.handlePresetImage)
	mux.HandleFunc("POST /presets/{id}/delete", s.handlePresetDelete)
	mux.HandleFunc("GET /settings", s.handleSettings)
	mux.HandleFunc("POST /settings", s.handleSettingsPost)
	mux.HandleFunc("GET /bulk", s.handleBulk)
	mux.HandleFunc("POST /bulk", s.handleBulkPost)
	mux.HandleFunc("GET /backup", s.handleBackup)
	mux.HandleFunc("POST /restore", s.handleRestore)

	s.handler = s.secure(mux)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// secure applies host checking, CSRF protection and security headers.
func (s *Server) secure(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, h := range s.opts.AllowedHosts {
		allowed[strings.ToLower(h)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(allowed) > 0 && !allowed[strings.ToLower(r.Host)] {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self'; script-src 'self'; "+
			"form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			limit := int64(logo.MaxBytes + 1<<20) // room for a logo or CSV upload
			if r.URL.Path == "/restore" {
				limit = maxRestoreBytes + 1<<20
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(s.csrf)) != 1 {
				http.Error(w, "invalid or missing form token: reload the page and try again", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// page is the data every template receives.
type page struct {
	Title   string
	Nav     string // active nav item
	CSRF    string
	Theme   string // "", "light" or "dark"
	Version string
	Flash   string
	Error   string
}

func (s *Server) newPage(r *http.Request, title, nav string) page {
	theme, _ := s.store.GetSetting(r.Context(), settingTheme, "")
	return page{Title: title, Nav: nav, CSRF: s.csrf, Theme: theme, Version: s.opts.Version}
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	t, ok := s.pages[name]
	if !ok {
		panic("web: unknown template " + name)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	log.Printf("error: %v", err)
	http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
}

var funcs = template.FuncMap{
	"typeTitle": func(name string) string {
		if t, ok := encode.Lookup(name); ok {
			return t.Title
		}
		return name
	},
	"date": func(t time.Time) string { return t.Local().Format("2 Jan 2006 15:04") },
	"join": strings.Join,
	// hint returns a field's help text for forms, skipping short usages
	// such as "network name" that only repeat the label.
	"hint": func(f encode.Field) string {
		if len(strings.Fields(f.Usage)) <= 3 {
			return ""
		}
		return f.Usage
	},
	"seq": func(from, to int) []int {
		var out []int
		for i := from; i <= to; i++ {
			out = append(out, i)
		}
		return out
	},
}

// parsePages parses each page template together with the shared layout.
func parsePages() (map[string]*template.Template, error) {
	base, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/preview.html")
	if err != nil {
		return nil, err
	}
	pages := map[string]*template.Template{}
	for _, name := range []string{"editor", "library", "bulk", "presets", "settings"} {
		t, err := template.Must(base.Clone()).ParseFS(templateFS, "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		pages[name] = t
	}
	pages["preview"] = base
	return pages, nil
}
