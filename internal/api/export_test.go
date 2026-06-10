package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/duggan/bewitch/internal/config"
	"github.com/duggan/bewitch/internal/db"
)

// exportTestServer returns a server whose export/snapshot output is confined to
// the returned base directory (set via [daemon] export_dir).
func exportTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	base := t.TempDir()
	database, err := db.Open(filepath.Join(t.TempDir(), "export.duckdb"), "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := &Server{
		dbFn: func() *sql.DB { return database },
		cfg:  &config.Config{Daemon: config.DaemonConfig{ExportDir: base}},
	}
	return s, base
}

func postExport(t *testing.T, s *Server, body string) (*httptest.ResponseRecorder, ExportResponse) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/export", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	s.handleExport(w, req)
	var resp ExportResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

// TestExportRejectsInvalidFormat covers the FORMAT injection seam: req.Format is
// interpolated into the COPY options, so anything outside the allowlist is refused.
func TestExportRejectsInvalidFormat(t *testing.T) {
	s, base := exportTestServer(t)
	path := filepath.Join(base, "out.dat")
	body := `{"sql":"SELECT 1","path":"` + path + `","format":"csv) ; DROP TABLE cpu_metrics; --"}`
	w, resp := postExport(t, s, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (invalid format rejected); resp=%+v", w.Code, resp)
	}
}

// TestExportPathInjectionCannotDropTable proves the COPY target path is a properly
// escaped string literal: a path full of SQL must be treated as a filename, not run.
func TestExportPathInjectionCannotDropTable(t *testing.T) {
	s, base := exportTestServer(t)
	// A path crafted to break out of the single-quoted literal and drop a table.
	malicious := filepath.Join(base, "x'); DROP TABLE cpu_metrics; --.csv")
	body, _ := json.Marshal(map[string]string{"sql": "SELECT 1 AS n", "path": malicious})
	w, resp := postExport(t, s, string(body))
	if resp.Error != "" {
		// A filesystem error writing the odd filename is acceptable; an injection is not.
		t.Logf("export returned error (acceptable if filesystem): %s (status %d)", resp.Error, w.Code)
	}

	// The table must still exist — if the injection had executed, this would error.
	var n int
	if err := s.dbFn().QueryRow("SELECT COUNT(*) FROM cpu_metrics").Scan(&n); err != nil {
		t.Fatalf("cpu_metrics missing after export — path injection executed: %v", err)
	}
}

// TestExportRejectsPathOutsideBaseDir proves the export destination is confined to
// export_dir: a traversal or an absolute path elsewhere is refused before any write.
func TestExportRejectsPathOutsideBaseDir(t *testing.T) {
	s, base := exportTestServer(t)
	outside := filepath.Join(t.TempDir(), "loot.csv") // a different temp dir, not under base
	for _, p := range []string{outside, filepath.Join(base, "..", "escape.csv"), "/etc/bewitch-pwned.csv"} {
		body, _ := json.Marshal(map[string]string{"sql": "SELECT 1 AS n", "path": p, "format": "csv"})
		w, resp := postExport(t, s, string(body))
		if w.Code != http.StatusBadRequest {
			t.Errorf("export to %q: status = %d, want 400 (outside export_dir); resp=%+v", p, w.Code, resp)
		}
		if _, err := os.Stat(p); err == nil {
			t.Errorf("export wrote outside export_dir: %q exists", p)
		}
	}
}

// TestExportRefusesOverwrite proves an existing destination is not clobbered.
func TestExportRefusesOverwrite(t *testing.T) {
	s, base := exportTestServer(t)
	victim := filepath.Join(base, "victim.csv")
	if err := os.WriteFile(victim, []byte("precious"), 0644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"sql": "SELECT 1 AS n", "path": victim, "format": "csv"})
	w, resp := postExport(t, s, string(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (refuse overwrite); resp=%+v", w.Code, resp)
	}
	if b, _ := os.ReadFile(victim); string(b) != "precious" {
		t.Fatalf("victim file was overwritten: %q", b)
	}
}

// TestExportWritesInsideBaseDir is the happy path: a destination within export_dir
// succeeds and produces a file.
func TestExportWritesInsideBaseDir(t *testing.T) {
	s, base := exportTestServer(t)
	dest := filepath.Join(base, "ok.csv")
	body, _ := json.Marshal(map[string]string{"sql": "SELECT 1 AS n", "path": dest, "format": "csv"})
	w, resp := postExport(t, s, string(body))
	if w.Code != http.StatusOK || resp.Error != "" {
		t.Fatalf("status = %d, resp = %+v, want success", w.Code, resp)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("export did not write %q: %v", dest, err)
	}
}
