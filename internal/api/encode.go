package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"

	"github.com/duggan/bewitch/internal/alert"
)

func isJSONBody(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Response types.

type GenericResponse struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

type CPUResponse struct {
	Cores []CPUCoreMetric `json:"cores"`
}

type DiskResponse struct {
	Disks []DiskMetric `json:"disks"`
}

type NetworkResponse struct {
	Interfaces []NetworkMetric `json:"interfaces"`
}

type TemperatureResponse struct {
	Sensors []TemperatureMetric `json:"sensors"`
}

type PowerResponse struct {
	Zones []PowerMetric `json:"zones"`
}

type ECCResponse struct {
	ECC *ECCMetric `json:"ecc"`
}

type GPUResponse struct {
	GPUs  []GPUMetric `json:"gpus"`
	Hints []string    `json:"hints,omitempty"`
}

type AlertsResponse struct {
	Alerts []AlertMetric `json:"alerts"`
}

type AlertRulesResponse struct {
	Rules []AlertRuleMetric `json:"rules"`
}

type PreferencesResponse struct {
	Items map[string]string `json:"items"`
}

type StatusResponse struct {
	Status             string            `json:"status"`
	Version            string            `json:"version,omitempty"`
	UptimeSec          float64           `json:"uptime_sec"`
	DefaultInterval    string            `json:"default_interval"`
	CollectorIntervals map[string]string `json:"collector_intervals,omitempty"`
}

// StatsResponse is the response for GET /api/stats.
type StatsResponse struct {
	Version    string            `json:"version"`
	UptimeSec  float64           `json:"uptime_sec"`
	Database   DatabaseStats     `json:"database"`
	Archive    *ArchiveStats     `json:"archive,omitempty"`
	Coverage   CoverageStats     `json:"coverage"`
	Tables     []TableStats      `json:"tables"`
	Dimensions map[string]int64  `json:"dimensions"`
	Processes  int64             `json:"processes"`
	Alerts     AlertCountStats   `json:"alerts"`
	Collectors map[string]string `json:"collectors,omitempty"`
}

type DatabaseStats struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	WALBytes  int64  `json:"wal_bytes"`
}

type ArchiveStats struct {
	Path       string `json:"path"`
	FileCount  int64  `json:"file_count"`
	TotalBytes int64  `json:"total_bytes"`
}

type CoverageStats struct {
	OldestTs    int64   `json:"oldest_ts"`
	NewestTs    int64   `json:"newest_ts"`
	SpanSeconds float64 `json:"span_seconds"`
}

type TableStats struct {
	Name     string `json:"name"`
	Rows     int64  `json:"rows"`
	OldestTs int64  `json:"oldest_ts"`
	NewestTs int64  `json:"newest_ts"`
}

type AlertCountStats struct {
	RulesEnabled  int `json:"rules_enabled"`
	RulesDisabled int `json:"rules_disabled"`
	FiredTotal    int `json:"fired_total"`
	FiredUnacked  int `json:"fired_unacked"`
}

// StatsCore holds the store-sourced bits of the stats response. The handler
// composes the full StatsResponse by adding file sizes, archive dir stats,
// version, uptime, and collector intervals.
type StatsCore struct {
	Tables     []TableStats
	Dimensions map[string]int64
	Processes  int64
	Alerts     AlertCountStats
}

type HistoryResponse struct {
	Series []TimeSeries `json:"series"`
}

// QueryResponse is the response for POST /api/query.
type QueryResponse struct {
	Columns []string `json:"columns,omitempty"`
	Rows    [][]any  `json:"rows,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// ExportRequest is the request body for POST /api/export.
type ExportRequest struct {
	SQL    string `json:"sql"`
	Path   string `json:"path"`
	Format string `json:"format,omitempty"` // "csv", "parquet", or "json"; inferred from extension if empty
}

// ExportResponse is the response for POST /api/export.
type ExportResponse struct {
	RowCount int64  `json:"row_count"`
	Path     string `json:"path"`
	Error    string `json:"error,omitempty"`
}

// SnapshotRequest is the request body for POST /api/snapshot.
type SnapshotRequest struct {
	Path             string `json:"path"`
	WithSystemTables bool   `json:"with_system_tables,omitempty"`
}

// SnapshotResponse is the response for POST /api/snapshot.
type SnapshotResponse struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Error     string `json:"error,omitempty"`
}

type ConfigResponse struct {
	Daemon DaemonConfigResponse `json:"daemon"`
	Alerts AlertsConfigResponse `json:"alerts"`
	TUI    TUIConfigResponse    `json:"tui"`
}

type DaemonConfigResponse struct {
	Socket          string `json:"socket"`
	DBPath          string `json:"db_path"`
	DefaultInterval string `json:"default_interval"`
}

type AlertsConfigResponse struct {
	EvaluationInterval string               `json:"evaluation_interval"`
	Email              []EmailDestResponse  `json:"email,omitempty"`
	Commands           []CommandDestResponse `json:"commands,omitempty"`
}

type EmailDestResponse struct {
	UseMailCmd bool     `json:"use_mail_cmd,omitempty"`
	SMTPHost   string   `json:"smtp_host,omitempty"`
	SMTPPort   int      `json:"smtp_port,omitempty"`
	From       string   `json:"from,omitempty"`
	To         []string `json:"to"`
}

type CommandDestResponse struct {
	Cmd string `json:"cmd"`
}

// NotifyTestResponse is the response for POST /api/test-notifications.
type NotifyTestResponse struct {
	Results []alert.NotifyResult `json:"results"`
}

type TUIConfigResponse struct {
	RefreshInterval string `json:"refresh_interval"`
}

type ArchiveStatusResponse struct {
	Tables     []ArchiveStatusItem `json:"tables"`
	TotalFiles int64               `json:"total_files"`
	TotalBytes int64               `json:"total_bytes"`
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if status >= 500 {
		log.Errorf("API error %d %s: %s", status, r.URL.Path, msg)
	}
	writeJSON(w, status, GenericResponse{Error: msg})
}

func writeGenericStatus(w http.ResponseWriter, status int, s string) {
	writeJSON(w, status, GenericResponse{Status: s})
}
