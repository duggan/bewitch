package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/duggan/bewitch/internal/db"
)

func exportTestServer(t *testing.T) *Server {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "export.duckdb"), "", "")
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return &Server{dbFn: func() *sql.DB { return database }}
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
	s := exportTestServer(t)
	path := filepath.Join(t.TempDir(), "out.dat")
	body := `{"sql":"SELECT 1","path":"` + path + `","format":"csv) ; DROP TABLE cpu_metrics; --"}`
	w, resp := postExport(t, s, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (invalid format rejected); resp=%+v", w.Code, resp)
	}
}

// TestExportPathInjectionCannotDropTable proves the COPY target path is a properly
// escaped string literal: a path full of SQL must be treated as a filename, not run.
func TestExportPathInjectionCannotDropTable(t *testing.T) {
	s := exportTestServer(t)
	// A path crafted to break out of the single-quoted literal and drop a table.
	malicious := filepath.Join(t.TempDir(), "x'); DROP TABLE cpu_metrics; --.csv")
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
