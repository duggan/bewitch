package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Daemon     DaemonConfig     `toml:"daemon"`
	Alerts     AlertsConfig     `toml:"alerts"`
	TUI        TUIConfig        `toml:"tui"`
	Collectors CollectorsConfig `toml:"collectors"`
}

type DaemonConfig struct {
	Mock                bool   `toml:"mock"`                  // synthetic data for macOS TUI development
	Socket              string `toml:"socket"`
	Listen              string `toml:"listen"`                // optional TCP listen address, e.g. ":9119"
	DBPath              string `toml:"db_path"`
	LogLevel            string `toml:"log_level"`             // "debug", "info", "warn", "error"; default "info"
	DefaultInterval     string `toml:"default_interval"`     // e.g. "5s", "1s", "100ms"; default collection interval for all collectors
	Retention           string `toml:"retention"`             // e.g. "30d", "720h"; empty = keep forever
	PruneInterval       string `toml:"prune_interval"`        // e.g. "1h", "30m"; default "1h"
	CompactionInterval  string `toml:"compaction_interval"`   // e.g. "24h", "7d"; empty = disabled
	CheckpointThreshold string `toml:"checkpoint_threshold"`  // e.g. "16MB", "256MB"; default "16MB" (DuckDB default)
	CheckpointInterval  string `toml:"checkpoint_interval"`   // e.g. "5m", "1m"; forced checkpoint interval for crash safety
	ArchiveThreshold    string `toml:"archive_threshold"`     // e.g. "7d"; archive data older than this to Parquet
	ArchiveInterval     string `toml:"archive_interval"`      // e.g. "6h"; how often to run archive; default "6h"
	ArchivePath         string `toml:"archive_path"`          // directory for Parquet archive files
	TLSCert             string `toml:"tls_cert"`              // PEM certificate path; empty = auto-generate self-signed
	TLSKey              string `toml:"tls_key"`               // PEM private key path; empty = auto-generate self-signed
	TLSDisabled         bool   `toml:"tls_disabled"`          // set true to disable TLS on TCP listener
	AuthToken           string `toml:"auth_token"`            // bearer token for TCP client authentication; empty = no auth
}

type AlertsConfig struct {
	EvaluationInterval string        `toml:"evaluation_interval"`
	Email              []EmailDest   `toml:"email"`
	Commands           []CommandDest `toml:"commands"`
}

type EmailDest struct {
	UseMailCmd bool     `toml:"use_mail_cmd"` // use local mail command instead of SMTP
	SMTPHost   string   `toml:"smtp_host"`
	SMTPPort   int      `toml:"smtp_port"` // default 587
	Username   string   `toml:"username"`
	Password   string   `toml:"password"`
	From       string   `toml:"from"`
	To         []string `toml:"to"`
	StartTLS   *bool    `toml:"starttls"` // nil = default true
}

// IsStartTLS returns whether STARTTLS is enabled (defaults to true).
func (e *EmailDest) IsStartTLS() bool {
	if e.StartTLS == nil {
		return true
	}
	return *e.StartTLS
}

// GetSMTPPort returns the configured SMTP port, defaulting to 587.
func (e *EmailDest) GetSMTPPort() int {
	if e.SMTPPort == 0 {
		return 587
	}
	return e.SMTPPort
}

type CommandDest struct {
	Cmd string `toml:"cmd"` // command to execute; alert details passed as env vars
}

type TUIConfig struct {
	RefreshInterval string   `toml:"refresh_interval"`
	HistoryRanges   []string `toml:"history_ranges"`
}

type CollectorsConfig struct {
	CPU         CPUCollectorConfig         `toml:"cpu"`
	Memory      MemoryCollectorConfig      `toml:"memory"`
	Disk        DiskCollectorConfig        `toml:"disk"`
	Network     NetworkCollectorConfig     `toml:"network"`
	ECC         ECCCollectorConfig         `toml:"ecc"`
	Temperature TemperatureCollectorConfig `toml:"temperature"`
	Power       PowerCollectorConfig       `toml:"power"`
	Process     ProcessCollectorConfig     `toml:"process"`
}

type CPUCollectorConfig struct {
	Interval string `toml:"interval"`
}

type MemoryCollectorConfig struct {
	Interval string `toml:"interval"`
}

type NetworkCollectorConfig struct {
	Interval string `toml:"interval"`
}

type ECCCollectorConfig struct {
	Interval string `toml:"interval"`
}

// collectorInterval parses a configured interval string, falling back to defaultInterval.
// Enforces a minimum of 100ms.
func collectorInterval(configured string, defaultInterval time.Duration) time.Duration {
	if configured == "" {
		return defaultInterval
	}
	d, err := parseDuration(configured)
	if err != nil {
		return defaultInterval
	}
	if d < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return d
}

func (c *CPUCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

func (c *MemoryCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

func (c *NetworkCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

func (c *ECCCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

type ProcessCollectorConfig struct {
	Interval     string   `toml:"interval"`
	MaxProcesses int      `toml:"max_processes"`
	Pinned       []string `toml:"pinned"` // Glob patterns of process names to always track with full metrics
}

func (c *ProcessCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

// DefaultMaxProcesses is the default number of processes to track.
const DefaultMaxProcesses = 100

// GetMaxProcesses returns the configured max processes, or the default if not set.
func (c *ProcessCollectorConfig) GetMaxProcesses() int {
	if c.MaxProcesses <= 0 {
		return DefaultMaxProcesses
	}
	return c.MaxProcesses
}

type TemperatureCollectorConfig struct {
	Interval string `toml:"interval"`
	Enabled  *bool  `toml:"enabled"`
}

func (c *TemperatureCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

// IsEnabled returns whether the temperature collector is enabled.
// Defaults to true if not explicitly set.
func (c *TemperatureCollectorConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

type PowerCollectorConfig struct {
	Interval string `toml:"interval"`
	Enabled  *bool  `toml:"enabled"`
}

func (c *PowerCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

// IsEnabled returns whether the power collector is enabled.
// Defaults to true if not explicitly set.
func (c *PowerCollectorConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

type DiskCollectorConfig struct {
	Interval          string   `toml:"interval"`
	ExcludeMounts     []string `toml:"exclude_mounts"`
	NoDefaultExcludes bool     `toml:"no_default_excludes"`
	SMARTInterval     string   `toml:"smart_interval"`
}

func (c *DiskCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

// GetSMARTInterval returns the interval between SMART data reads.
// Defaults to 5 minutes, minimum 30 seconds.
func (c *DiskCollectorConfig) GetSMARTInterval() time.Duration {
	if c.SMARTInterval == "" {
		return 5 * time.Minute
	}
	d, err := parseDuration(c.SMARTInterval)
	if err != nil {
		return 5 * time.Minute
	}
	if d < 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// DefaultDiskExcludes are mount path prefixes excluded by default.
var DefaultDiskExcludes = []string{"/snap/", "/run/"}

// GetDiskExcludes returns the effective list of mount exclusion prefixes.
func (c *DiskCollectorConfig) GetDiskExcludes() []string {
	if c.NoDefaultExcludes {
		return c.ExcludeMounts
	}
	// Merge defaults with user-specified excludes
	seen := make(map[string]bool)
	var result []string
	for _, e := range DefaultDiskExcludes {
		seen[e] = true
		result = append(result, e)
	}
	for _, e := range c.ExcludeMounts {
		if !seen[e] {
			result = append(result, e)
		}
	}
	return result
}

// HistoryRange is a parsed history range with label and duration.
type HistoryRange struct {
	Label    string
	Duration time.Duration
}

// DefaultHistoryRanges are used when no ranges are configured.
var DefaultHistoryRanges = []HistoryRange{
	{"1h", time.Hour},
	{"6h", 6 * time.Hour},
	{"24h", 24 * time.Hour},
	{"7d", 7 * 24 * time.Hour},
	{"30d", 30 * 24 * time.Hour},
}

// ParseHistoryRanges parses the configured history range strings into durations.
// Supports Go duration strings (e.g. "6h") and day suffixes (e.g. "7d").
// Returns DefaultHistoryRanges if none are configured.
func (c *TUIConfig) ParseHistoryRanges() ([]HistoryRange, error) {
	if len(c.HistoryRanges) == 0 {
		return DefaultHistoryRanges, nil
	}
	ranges := make([]HistoryRange, 0, len(c.HistoryRanges))
	for _, s := range c.HistoryRanges {
		d, err := parseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid history range %q: %w", s, err)
		}
		ranges = append(ranges, HistoryRange{Label: s, Duration: d})
	}
	return ranges, nil
}

// parseDuration parses a duration string supporting "Nd" day format and Go durations.
func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var n int
		if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// DefaultCollectionInterval returns the default collection interval for collectors
// that don't specify their own interval. Returns 5s if not configured.
func (c *DaemonConfig) DefaultCollectionInterval() (time.Duration, error) {
	if c.DefaultInterval == "" {
		return 5 * time.Second, nil
	}
	d, err := parseDuration(c.DefaultInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid default_interval %q: %w", c.DefaultInterval, err)
	}
	if d < 100*time.Millisecond {
		return 100 * time.Millisecond, nil
	}
	return d, nil
}

// RetentionDuration parses the retention string into a time.Duration.
// Supports Go duration strings (e.g. "720h") and day suffixes (e.g. "30d").
// Returns 0 if retention is empty (keep forever).
func (c *DaemonConfig) RetentionDuration() (time.Duration, error) {
	if c.Retention == "" {
		return 0, nil
	}
	// Handle "Nd" day format
	if len(c.Retention) > 1 && c.Retention[len(c.Retention)-1] == 'd' {
		days := c.Retention[:len(c.Retention)-1]
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err != nil {
			return 0, fmt.Errorf("invalid retention %q: %w", c.Retention, err)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(c.Retention)
	if err != nil {
		return 0, fmt.Errorf("invalid retention %q: %w", c.Retention, err)
	}
	return d, nil
}

// PruneDuration parses the prune_interval string.
// Returns the default 1 hour if empty.
func (c *DaemonConfig) PruneDuration() (time.Duration, error) {
	if c.PruneInterval == "" {
		return time.Hour, nil
	}
	return parseDuration(c.PruneInterval)
}

// CompactionDuration parses the compaction_interval string.
// Returns 0 if empty (compaction disabled).
func (c *DaemonConfig) CompactionDuration() (time.Duration, error) {
	if c.CompactionInterval == "" {
		return 0, nil
	}
	return parseDuration(c.CompactionInterval)
}

// CheckpointDuration parses the checkpoint_interval string.
// Returns 0 if empty (periodic checkpoints disabled, relies on wal_autocheckpoint).
func (c *DaemonConfig) CheckpointDuration() (time.Duration, error) {
	if c.CheckpointInterval == "" {
		return 0, nil
	}
	return parseDuration(c.CheckpointInterval)
}

// ArchiveThresholdDuration parses the archive_threshold string.
// Returns 0 if empty (archiving disabled).
func (c *DaemonConfig) ArchiveThresholdDuration() (time.Duration, error) {
	if c.ArchiveThreshold == "" {
		return 0, nil
	}
	return parseDuration(c.ArchiveThreshold)
}

// ArchiveIntervalDuration parses the archive_interval string.
// Returns the default 6 hours if empty.
func (c *DaemonConfig) ArchiveIntervalDuration() (time.Duration, error) {
	if c.ArchiveInterval == "" {
		return 6 * time.Hour, nil
	}
	return parseDuration(c.ArchiveInterval)
}

// ValidateTLS checks TLS configuration for consistency.
// Returns an error if only one of tls_cert/tls_key is set.
func (c *DaemonConfig) ValidateTLS() error {
	hasCert := c.TLSCert != ""
	hasKey := c.TLSKey != ""
	if hasCert != hasKey {
		return fmt.Errorf("tls_cert and tls_key must both be set or both be empty")
	}
	return nil
}

// ValidateAuth checks authentication configuration for consistency.
// Returns a warning message if TCP listening is enabled without auth.
func (c *DaemonConfig) ValidateAuth() string {
	if c.Listen != "" && c.AuthToken == "" {
		return "TCP listener enabled without auth_token; any client can connect"
	}
	return ""
}

// Load reads and parses a TOML config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Daemon.Socket == "" {
		cfg.Daemon.Socket = "/run/bewitch/bewitch.sock"
	}
	if cfg.Daemon.DBPath == "" {
		cfg.Daemon.DBPath = "/var/lib/bewitch/bewitch.duckdb"
	}
	if cfg.Daemon.ArchivePath == "" && cfg.Daemon.ArchiveThreshold != "" {
		cfg.Daemon.ArchivePath = filepath.Join(filepath.Dir(cfg.Daemon.DBPath), "archive")
	}
	if err := cfg.Daemon.ValidateTLS(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
