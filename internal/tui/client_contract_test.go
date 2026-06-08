package tui

import (
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
)

// TestDaemonClientAlertContract drives the REAL DaemonClient against the REAL API handlers
// over an httptest server and a migrated DuckDB. Unlike mockClient (which can't catch a
// wrong URL, verb, or serialization), this exercises the full HTTP round-trip for the
// alert-rule CRUD + ack the TUI depends on, so client/server drift is caught.
func TestDaemonClientAlertContract(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "contract.duckdb"), "", "")
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

	addr := strings.TrimPrefix(ts.URL, "http://")
	client := NewDaemonClientTCP(addr, nil, "") // plain HTTP, no auth

	// Create (POST).
	if err := client.CreateAlertRule(api.AlertRuleMetric{
		Name: "disk_40", Type: "threshold", Severity: "warning",
		Metric: "disk.used_pct", Operator: ">", Value: 40, Duration: "1m", Mount: "/",
	}); err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}

	// List (GET) returns it, fully populated.
	rules, err := client.GetAlertRules()
	if err != nil {
		t.Fatalf("GetAlertRules: %v", err)
	}
	if len(rules) != 1 || rules[0].Name != "disk_40" || rules[0].Value != 40 || rules[0].Mount != "/" {
		t.Fatalf("unexpected rules after create: %+v", rules)
	}
	id := rules[0].ID

	// Update (PUT) — change value and severity.
	updated := rules[0]
	updated.Value = 90
	updated.Severity = "critical"
	if err := client.UpdateAlertRule(updated); err != nil {
		t.Fatalf("UpdateAlertRule: %v", err)
	}
	rules, _ = client.GetAlertRules()
	if len(rules) != 1 || rules[0].Value != 90 || rules[0].Severity != "critical" {
		t.Fatalf("update not reflected: %+v", rules)
	}

	// Acknowledge (POST /api/alerts/{id}/ack): seed a fired alert, ack it, confirm it
	// drops out of the active set.
	if _, err := database.Exec(
		"INSERT INTO alerts (ts, rule_name, severity, message) VALUES (now(), 'disk_40', 'warning', 'x')"); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	active, err := client.GetActiveAlerts()
	if err != nil || len(active) != 1 {
		t.Fatalf("GetActiveAlerts before ack: n=%d err=%v", len(active), err)
	}
	if err := client.AckAlert(active[0].ID); err != nil {
		t.Fatalf("AckAlert: %v", err)
	}
	if active, _ = client.GetActiveAlerts(); len(active) != 0 {
		t.Fatalf("expected no active alerts after ack, got %d", len(active))
	}

	// Delete (DELETE) — rule gone, and its fired alerts cleared.
	if err := client.DeleteAlertRule(id); err != nil {
		t.Fatalf("DeleteAlertRule: %v", err)
	}
	if rules, _ = client.GetAlertRules(); len(rules) != 0 {
		t.Fatalf("expected no rules after delete, got %d", len(rules))
	}
	var remaining int
	database.QueryRow("SELECT count(*) FROM alerts WHERE rule_name = 'disk_40'").Scan(&remaining)
	if remaining != 0 {
		t.Errorf("expected fired alerts cleared on delete, got %d", remaining)
	}
}

// TestDaemonClientCustomContract drives the real DaemonClient custom-source methods
// (catalog, live metrics/status, history) against the real handlers, catching
// client/server drift on the Services-tab endpoints.
func TestDaemonClientCustomContract(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "custom-contract.duckdb"), "", "")
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

	client := NewDaemonClientTCP(strings.TrimPrefix(ts.URL, "http://"), nil, "")

	// Catalog.
	srv.SetCustomCatalog([]api.CustomSourceInfo{{
		Name:    "qb",
		Metrics: []api.CustomFieldInfo{{Name: "dl", Unit: "bytes"}},
		Status:  []api.CustomFieldInfo{{Name: "Connection"}},
	}})
	sources, err := client.GetCustomSources()
	if err != nil || len(sources) != 1 || sources[0].Name != "qb" || sources[0].Metrics[0].Unit != "bytes" {
		t.Fatalf("GetCustomSources: %+v err=%v", sources, err)
	}

	// Live metrics + status.
	srv.SetCustomSnapshot("qb",
		[]api.CustomMetric{{Source: "qb", Name: "dl", Unit: "bytes", Value: 2048}},
		[]api.CustomStatus{{Source: "qb", Label: "Connection", Value: "connected", Badge: "ok"}})
	metrics, status, err := client.GetCustom()
	if err != nil || len(metrics) != 1 || metrics[0].Value != 2048 {
		t.Fatalf("GetCustom metrics: %+v err=%v", metrics, err)
	}
	if len(status) != 1 || status[0].Badge != "ok" {
		t.Fatalf("GetCustom status: %+v", status)
	}

	// History.
	now := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := database.Exec(
			`INSERT INTO custom_metrics (ts, source, metric, value) VALUES (?, 'qb', 'dl', ?)`,
			now.Add(-time.Duration(i)*time.Minute), float64(100+i)); err != nil {
			t.Fatalf("seed custom_metrics: %v", err)
		}
	}
	series, err := client.GetCustomHistory("qb", "dl", now.Add(-time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("GetCustomHistory: %v", err)
	}
	if len(series) != 1 || len(series[0].Points) == 0 {
		t.Fatalf("GetCustomHistory series = %+v", series)
	}
}
