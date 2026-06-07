package api

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/alert"
	"github.com/duggan/bewitch/internal/config"
	"github.com/duggan/bewitch/internal/db"
)

// TestECCAlertFires exercises the full ecc.uncorrectable path end to end against the
// real migrated schema — guarding the ecc_metrics column names the buildQuery SQL
// references — and confirms MAX(uncorrected) > 0 fires.
func TestECCAlertFires(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ecc.duckdb")
	database, err := db.Open(dbPath, "", "")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	dbFn := func() *sql.DB { return database }
	s := &Server{dbFn: dbFn}

	rec := httptest.NewRecorder()
	s.handleCreateAlertRule(rec, httptest.NewRequest("POST", "/api/alert-rules",
		strings.NewReader(`{"name":"ecc_ue","type":"threshold","severity":"critical",
			"metric":"ecc.uncorrectable","operator":">","value":0,"duration":"5m"}`)))
	if rec.Code != 201 {
		t.Fatalf("create rule: %d %s", rec.Code, rec.Body.String())
	}

	now := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := database.Exec(
			`INSERT INTO ecc_metrics (ts, corrected, uncorrected) VALUES (?, 10, 2)`,
			now.Add(-time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("seed ecc_metrics: %v", err)
		}
	}

	engine := alert.NewEngine(dbFn, &config.AlertsConfig{EvaluationInterval: "1s"})
	engine.EvaluateOnce()

	var fired int
	database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'ecc_ue'").Scan(&fired)
	if fired == 0 {
		t.Fatal("ecc.uncorrectable > 0 rule did not fire on 2 uncorrectable errors")
	}
}
