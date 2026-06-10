package api

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/db"
)

// TestHandleDeleteAlert covers the bounded fired-alert delete used by the TUI
// multi-select clear: it removes exactly the one row, 404s when it's already gone,
// leaves other rows untouched, and 400s on a non-numeric id.
func TestHandleDeleteAlert(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "del.duckdb"), "", "")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	s := &Server{dbFn: func() *sql.DB { return database }}

	for _, id := range []int{1, 2} {
		if _, err := database.Exec(
			"INSERT INTO alerts (id, ts, rule_name, severity, message, acknowledged) VALUES (?, ?, 'r', 'warning', 'm', false)",
			id, time.Now()); err != nil {
			t.Fatalf("seed alert %d: %v", id, err)
		}
	}

	del := func(id string) int {
		req := httptest.NewRequest("DELETE", "/api/alerts/"+id, nil)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleDeleteAlert(w, req)
		return w.Code
	}

	if code := del("1"); code != 200 {
		t.Fatalf("delete id=1: status %d, want 200", code)
	}
	if code := del("1"); code != 404 {
		t.Errorf("re-delete id=1: status %d, want 404 (already gone)", code)
	}
	if code := del("notanint"); code != 400 {
		t.Errorf("bad id: status %d, want 400", code)
	}

	var n int
	if err := database.QueryRow("SELECT COUNT(*) FROM alerts").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("alerts count = %d, want 1 (only id=2 should remain)", n)
	}
}
