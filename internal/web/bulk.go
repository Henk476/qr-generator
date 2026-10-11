package web

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Henk476/qr-generator/internal/batch"
	"github.com/Henk476/qr-generator/internal/encode"
	"github.com/Henk476/qr-generator/internal/generate"
	"github.com/Henk476/qr-generator/internal/render"
	"github.com/Henk476/qr-generator/internal/store"
)

// maxRestoreBytes is the upload limit for backup archives.
const maxRestoreBytes = 512 << 20

// bulkData backs templates/bulk.html.
type bulkData struct {
	page
	Opts    *designOptions
	Types   []encode.Type
	Presets []store.Preset
	Form    bulkForm
}

type bulkForm struct {
	Type    string
	Preset  string
	Format  string
	Size    int
	Map     string
	Save    bool
	Tags    string
	PrintMM float64
}

func (s *Server) bulkPage(r *http.Request, f bulkForm) (bulkData, error) {
	presets, err := s.store.ListPresets(r.Context())
	return bulkData{page: s.newPage(r, "Bulk", "bulk"), Opts: s.options, Types: encode.Types(), Presets: presets, Form: f}, err
}

func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	_, e := s.defaults(r)
	d, err := s.bulkPage(r, bulkForm{Type: "url", Preset: "classic", Format: e.Format, Size: e.Size, PrintMM: e.PrintMM})
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.render(w, http.StatusOK, "bulk", d)
}

func (s *Server) handleBulkPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f := bulkForm{
		Type:   r.PostFormValue("type"),
		Preset: r.PostFormValue("preset"),
		Format: r.PostFormValue("format"),
		Map:    r.PostFormValue("map"),
		Save:   r.PostFormValue("save") != "",
		Tags:   r.PostFormValue("tags"),
	}
	f.Size, _ = strconv.Atoi(r.PostFormValue("size"))
	f.PrintMM, _ = strconv.ParseFloat(r.PostFormValue("print_mm"), 64)
	fail := func(msg string) {
		d, err := s.bulkPage(r, f)
		if err != nil {
			s.serverError(w, err)
			return
		}
		d.Error = msg
		s.render(w, http.StatusUnprocessableEntity, "bulk", d)
	}

	format, err := render.ParseFormat(f.Format)
	if err != nil {
		fail(err.Error())
		return
	}
	design := generate.DefaultDesign()
	if f.Preset != "" {
		p, err := s.store.GetPreset(ctx, f.Preset)
		if err != nil {
			fail("Choose a preset.")
			return
		}
		design = p.Design
	}
	rename, err := batch.ParseRename(f.Map)
	if err != nil {
		fail(err.Error())
		return
	}
	file, _, err := r.FormFile("csv_file")
	if err != nil {
		fail("Choose a CSV file.")
		return
	}
	defer file.Close()
	rows, err := batch.Parse(io.LimitReader(file, 5<<20), batch.ParseOptions{DefaultType: f.Type, Rename: rename})
	if err != nil {
		fail(err.Error())
		return
	}

	var buf bytes.Buffer
	results, err := batch.Generate(&buf, rows, batch.Options{
		Design: design, Assets: s.assetLoader(ctx), Verify: true,
		Export: render.Options{Format: format, Size: f.Size, PrintMM: f.PrintMM},
	})
	if err != nil {
		fail(err.Error())
		return
	}
	if f.Save {
		tags := strings.Split(f.Tags, ",")
		for _, res := range results {
			if res.Err != nil {
				continue
			}
			name := res.Row.Name
			if name == "" {
				name = generate.DefaultName(res.Type, res.Fields)
			}
			c := &store.Code{Name: name, Type: res.Type.Name, Fields: res.Fields, Design: design, Tags: tags}
			if err := s.store.CreateCode(ctx, c); err != nil {
				s.serverError(w, err)
				return
			}
		}
	}
	ok, warned, failed := batch.Summary(results)
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="canute-qr-%s.zip"`, time.Now().Format("2006-01-02-150405")))
	w.Header().Set("X-Canute-Summary", fmt.Sprintf("ok=%d warnings=%d failed=%d", ok, warned, failed))
	w.Write(buf.Bytes())
}

// ---- Backup and restore ----

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := s.store.Backup(r.Context(), &buf, s.opts.Version); err != nil {
		s.serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="canute-qr-backup-%s.zip"`, time.Now().Format("2006-01-02")))
	w.Write(buf.Bytes())
}

func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		d := s.settingsPage(r)
		d.Error = msg
		s.render(w, http.StatusUnprocessableEntity, "settings", d)
	}
	file, hdr, err := r.FormFile("backup_file")
	if err != nil {
		fail("Choose a backup file.")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRestoreBytes+1))
	if err != nil {
		s.serverError(w, err)
		return
	}
	if len(data) > maxRestoreBytes || hdr.Size > maxRestoreBytes {
		fail("That backup is too large.")
		return
	}
	stats, err := s.store.Restore(r.Context(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		fail("Restore failed: " + err.Error())
		return
	}
	// Restored logos may reuse IDs with different content.
	s.logoCache.Range(func(k, _ any) bool { s.logoCache.Delete(k); return true })
	http.Redirect(w, r, fmt.Sprintf("/settings?restored=%d,%d,%d", stats.Codes, stats.Presets, stats.Logos), http.StatusSeeOther)
}
