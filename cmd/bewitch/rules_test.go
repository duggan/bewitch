package main

import (
	"bytes"
	"context"
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/duggan/bewitch/internal/api"
	"github.com/duggan/bewitch/internal/config"
	"github.com/duggan/bewitch/internal/db"
	"github.com/duggan/bewitch/internal/tui"
)

// newRulesDaemon serves the real API handlers over httptest and returns the real
// client, so export/import exercise the same HTTP round-trips as the CLI.
func newRulesDaemon(t *testing.T) *tui.DaemonClient {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "rules.duckdb"), db.Options{})
	if err != nil {
		t.Fatalf("opening migrated db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	srv := api.NewServer(&config.Config{}, func() *sql.DB { return database })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return tui.NewDaemonClientTCP(strings.TrimPrefix(ts.URL, "http://"), nil, "")
}

// TestRulesExportImport: rules exported from one host import onto another
// (created, enabled flag preserved), a re-import is a no-op, and an edited file
// updates rules in place without touching rules that aren't in it.
func TestRulesExportImport(t *testing.T) {
	src := newRulesDaemon(t)
	for _, r := range []api.AlertRuleMetric{
		{Name: "root-full", Type: "threshold", Severity: "warning", Metric: "disk.used_pct", Operator: ">", Value: 90, Duration: "5m", Mount: "/", Aggregate: "max"},
		{Name: "realloc", Type: "threshold", Severity: "critical", Metric: "smart.reallocated", Operator: ">", Value: 0, Duration: "24h", Aggregate: "increase"},
		{Name: "nginx", Type: "process_down", Severity: "critical", ProcessName: "nginx", MinInstances: 2, CheckDuration: "1m"},
	} {
		if err := src.CreateAlertRule(r); err != nil {
			t.Fatalf("seed %s: %v", r.Name, err)
		}
	}
	srcRules, _ := src.GetAlertRules()
	for _, r := range srcRules {
		if r.Name == "nginx" {
			if err := src.ToggleAlertRule(r.ID); err != nil { // disabled on the source
				t.Fatal(err)
			}
		}
	}

	var exported bytes.Buffer
	if err := exportRules(src, &exported); err != nil {
		t.Fatalf("export: %v", err)
	}
	if strings.Contains(exported.String(), `"id"`) {
		t.Errorf("export should not carry ids:\n%s", exported.String())
	}

	dst := newRulesDaemon(t)
	if err := dst.CreateAlertRule(api.AlertRuleMetric{Name: "local-only", Type: "threshold", Severity: "warning",
		Metric: "cpu.aggregate", Operator: ">", Value: 95, Duration: "5m"}); err != nil {
		t.Fatal(err)
	}
	rules, err := readRulesFile(bytes.NewReader(exported.Bytes()))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out bytes.Buffer
	if err := importRules(dst, rules, false, &out); err != nil {
		t.Fatalf("import: %v\n%s", err, out.String())
	}
	got := map[string]api.AlertRuleMetric{}
	dstRules, _ := dst.GetAlertRules()
	for _, r := range dstRules {
		got[r.Name] = r
	}
	if len(got) != 4 {
		t.Fatalf("destination has %d rules, want 3 imported + local-only", len(got))
	}
	for _, want := range srcRules {
		have := got[want.Name]
		if want.Name == "nginx" {
			want.Enabled = false
		}
		if !sameRule(have, want) || have.Enabled != want.Enabled {
			t.Errorf("%s imported as %+v, want %+v", want.Name, have, want)
		}
	}

	// Re-import: nothing to do.
	out.Reset()
	if err := importRules(dst, rules, false, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "created") || strings.Contains(out.String(), "updated") || strings.Contains(out.String(), "abled") {
		t.Errorf("re-import should be a no-op, got:\n%s", out.String())
	}

	// Edit the file: an update in place keeps the rule's id.
	rules[0].Value = 80
	out.Reset()
	if err := importRules(dst, rules, true, &out); err != nil || !strings.Contains(out.String(), "would update root-full") {
		t.Fatalf("dry run = %v:\n%s", err, out.String())
	}
	if err := importRules(dst, rules, false, &out); err != nil {
		t.Fatal(err)
	}
	dstRules, _ = dst.GetAlertRules()
	for _, r := range dstRules {
		if r.Name == "root-full" && (r.Value != 80 || r.ID != got["root-full"].ID) {
			t.Errorf("root-full after update = %+v, want value 80 and id %d", r, got["root-full"].ID)
		}
	}
}

// TestRulesImportTypeMismatch: a file rule whose name belongs to a rule of a
// different type on the daemon is reported and skipped, not applied.
func TestRulesImportTypeMismatch(t *testing.T) {
	dst := newRulesDaemon(t)
	if err := dst.CreateAlertRule(api.AlertRuleMetric{Name: "x", Type: "process_down", Severity: "warning", ProcessName: "a", MinInstances: 1}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := importRules(dst, []api.AlertRuleMetric{{Name: "x", Type: "threshold", Severity: "warning", Metric: "cpu.aggregate", Operator: ">", Value: 1, Duration: "1m", Enabled: true}}, false, &out)
	if err == nil || !strings.Contains(out.String(), "skip    x") {
		t.Errorf("err = %v, out = %q; want a skip and an error", err, out.String())
	}
}

func TestReadRulesFileRejects(t *testing.T) {
	for name, body := range map[string]string{
		"version":   `{"version": 2, "rules": []}`,
		"unknown":   `{"version": 1, "rules": [{"name": "a", "type": "threshold", "treshold": 1}]}`,
		"duplicate": `{"version": 1, "rules": [{"name": "a"}, {"name": "a"}]}`,
		"unnamed":   `{"version": 1, "rules": [{"type": "threshold"}]}`,
	} {
		if _, err := readRulesFile(strings.NewReader(body)); err == nil {
			t.Errorf("%s: accepted %s", name, body)
		}
	}
}
