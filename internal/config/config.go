package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultConfigPath is the default location of the configuration file.
const DefaultConfigPath = "/etc/bewitch.toml"

type Config struct {
	Daemon     DaemonConfig     `toml:"daemon"`
	Alerts     AlertsConfig     `toml:"alerts"`
	TUI        TUIConfig        `toml:"tui"`
	Collectors CollectorsConfig `toml:"collectors"`
	// CustomSources are user-defined HTTP data sources polled by the daemon.
	// Repeated [[custom_source]] sections, sibling of [[alerts.email]] — host
	// config, not a collector. Sources may also live in drop-in *.toml files
	// under [daemon] sources_dir; both are merged by LoadSources (sources.go).
	CustomSources []CustomSourceConfig `toml:"custom_source"`
}

type DaemonConfig struct {
	Mock                bool   `toml:"mock"` // synthetic data for macOS TUI development
	Socket              string `toml:"socket"`
	Listen              string `toml:"listen"` // optional TCP listen address, e.g. ":9119"
	DBPath              string `toml:"db_path"`
	LogLevel            string `toml:"log_level"`            // "debug", "info", "warn", "error"; default "info"
	DefaultInterval     string `toml:"default_interval"`     // e.g. "5s", "1s", "100ms"; default collection interval for all collectors
	Retention           string `toml:"retention"`            // e.g. "30d", "720h"; empty = keep forever
	PruneInterval       string `toml:"prune_interval"`       // e.g. "1h", "30m"; default "1h"
	CompactionInterval  string `toml:"compaction_interval"`  // e.g. "24h", "7d"; empty = disabled
	CheckpointThreshold string `toml:"checkpoint_threshold"` // e.g. "16MB", "256MB"; default "16MB" (DuckDB default)
	DBMemoryLimit       string `toml:"db_memory_limit"`      // e.g. "512MB", "1GB"; caps DuckDB working memory; empty = DuckDB default (~80% RAM)
	CheckpointInterval  string `toml:"checkpoint_interval"`  // e.g. "5m", "1m"; forced checkpoint interval for crash safety
	ArchiveThreshold    string `toml:"archive_threshold"`    // e.g. "7d"; archive data older than this to Parquet
	ArchiveInterval     string `toml:"archive_interval"`     // e.g. "6h"; how often to run archive; default "6h"
	ArchivePath         string `toml:"archive_path"`         // directory for Parquet archive files
	TLSCert             string `toml:"tls_cert"`             // PEM certificate path; empty = auto-generate self-signed
	TLSKey              string `toml:"tls_key"`              // PEM private key path; empty = auto-generate self-signed
	TLSDisabled         bool   `toml:"tls_disabled"`         // set true to disable TLS on TCP listener
	AuthToken           string `toml:"auth_token"`           // bearer token for TCP client authentication; empty = no auth
	SourcesDir          string `toml:"sources_dir"`          // directory of drop-in custom-source *.toml files; default <configdir>/sources.d
	ExportDir           string `toml:"export_dir"`           // directory that /api/export and /api/snapshot output is confined to; empty = the db_path directory
}

type AlertsConfig struct {
	EvaluationInterval string        `toml:"evaluation_interval"`
	Email              []EmailDest   `toml:"email"`
	Commands           []CommandDest `toml:"commands"`
	// ShoutrrrURLs are Shoutrrr service URLs (Discord/Telegram/ntfy/Slack/etc.);
	// alerts are delivered to each. See https://containrrr.dev/shoutrrr/services.
	ShoutrrrURLs []string `toml:"shoutrrr_urls"`
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
	RefreshInterval string        `toml:"refresh_interval"`
	HistoryRanges   []string      `toml:"history_ranges"`
	Capture         CaptureConfig `toml:"capture"`
}

type CaptureConfig struct {
	Directory   string `toml:"directory"`   // default save directory; empty = home directory
	DPI         int    `toml:"dpi"`         // render DPI; default 144 (2x); 72 = 1x, 216 = 3x
	Compression string `toml:"compression"` // "default", "best", "none"; default "best"
	Background  string `toml:"background"`  // hex background color; default "#1A1A2E"
	Foreground  string `toml:"foreground"`  // hex foreground color; default "#F8F8F2"
}

// GetDPI returns the configured DPI, defaulting to 144.
func (c *CaptureConfig) GetDPI() int {
	if c.DPI <= 0 {
		return 144
	}
	return c.DPI
}

// GetCompression returns the configured compression level string, defaulting to "best".
func (c *CaptureConfig) GetCompression() string {
	switch c.Compression {
	case "default", "best", "none":
		return c.Compression
	default:
		return "best"
	}
}

type CollectorsConfig struct {
	CPU         CPUCollectorConfig         `toml:"cpu"`
	Memory      MemoryCollectorConfig      `toml:"memory"`
	Load        LoadCollectorConfig        `toml:"load"`
	Disk        DiskCollectorConfig        `toml:"disk"`
	Network     NetworkCollectorConfig     `toml:"network"`
	ECC         ECCCollectorConfig         `toml:"ecc"`
	Temperature TemperatureCollectorConfig `toml:"temperature"`
	Power       PowerCollectorConfig       `toml:"power"`
	Process     ProcessCollectorConfig     `toml:"process"`
	GPU         GPUCollectorConfig         `toml:"gpu"`
}

type CPUCollectorConfig struct {
	Interval string `toml:"interval"`
}

type MemoryCollectorConfig struct {
	Interval string `toml:"interval"`
}

type LoadCollectorConfig struct {
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
	d, err := ParseDuration(configured)
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

func (c *LoadCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
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
	// NetworkIO toggles per-process network I/O collection (the eBPF reader). Pointer
	// so an unset value defaults to enabled; set false to skip loading/attaching the BPF
	// programs entirely, removing both the per-syscall kernel overhead and the per-cycle
	// map iteration. Disk I/O (cheap /proc reads) is unaffected.
	NetworkIO *bool `toml:"network_io"`
}

func (c *ProcessCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

// IsNetworkIOEnabled reports whether per-process network I/O (eBPF) should be collected.
// Unset defaults to enabled, mirroring the temperature/power collector convention.
func (c *ProcessCollectorConfig) IsNetworkIOEnabled() bool {
	if c.NetworkIO == nil {
		return true
	}
	return *c.NetworkIO
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

type GPUCollectorConfig struct {
	Interval string `toml:"interval"`
	Enabled  *bool  `toml:"enabled"`
}

func (c *GPUCollectorConfig) GetInterval(defaultInterval time.Duration) time.Duration {
	return collectorInterval(c.Interval, defaultInterval)
}

// IsEnabled returns whether the GPU collector is enabled.
// Defaults to true if not explicitly set.
func (c *GPUCollectorConfig) IsEnabled() bool {
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
	d, err := ParseDuration(c.SMARTInterval)
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
		d, err := ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("invalid history range %q: %w", s, err)
		}
		ranges = append(ranges, HistoryRange{Label: s, Duration: d})
	}
	return ranges, nil
}

// ParseDuration parses a duration string supporting "Nd" day format and Go durations.
func ParseDuration(s string) (time.Duration, error) {
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
	d, err := ParseDuration(c.DefaultInterval)
	if err != nil {
		return 0, fmt.Errorf("invalid default_interval %q: %w", c.DefaultInterval, err)
	}
	if d < 100*time.Millisecond {
		return 100 * time.Millisecond, nil
	}
	return d, nil
}

// DefaultDBMemoryLimit caps DuckDB's working memory when the user hasn't set
// one. DuckDB otherwise defaults to ~80% of physical RAM, which is far too much
// for a background monitoring daemon and is the proximate cause of OOM kills on
// small hosts. A conservative cap (with temp_directory spilling) is always safer
// for a tool that is meant to sit quietly in the background.
const DefaultDBMemoryLimit = "512MB"

// DBMemoryLimitValue returns the configured DuckDB memory limit, falling back to
// DefaultDBMemoryLimit. It never returns "" so DuckDB's ~80%-of-RAM default is
// never used.
func (c *DaemonConfig) DBMemoryLimitValue() string {
	if c.DBMemoryLimit == "" {
		return DefaultDBMemoryLimit
	}
	return c.DBMemoryLimit
}

// RetentionDuration parses the retention string into a time.Duration.
// Supports Go duration strings (e.g. "720h") and day suffixes (e.g. "30d").
// Returns 0 if retention is empty (keep forever).
func (c *DaemonConfig) RetentionDuration() (time.Duration, error) {
	if c.Retention == "" {
		return 0, nil
	}
	d, err := ParseDuration(c.Retention)
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
	return ParseDuration(c.PruneInterval)
}

// CompactionDuration parses the compaction_interval string.
// Returns 0 if empty (compaction disabled).
func (c *DaemonConfig) CompactionDuration() (time.Duration, error) {
	if c.CompactionInterval == "" {
		return 0, nil
	}
	return ParseDuration(c.CompactionInterval)
}

// CheckpointDuration parses the checkpoint_interval string.
// Returns 0 if empty (periodic checkpoints disabled, relies on wal_autocheckpoint).
func (c *DaemonConfig) CheckpointDuration() (time.Duration, error) {
	if c.CheckpointInterval == "" {
		return 0, nil
	}
	return ParseDuration(c.CheckpointInterval)
}

// ArchiveThresholdDuration parses the archive_threshold string.
// Returns 0 if empty (archiving disabled).
func (c *DaemonConfig) ArchiveThresholdDuration() (time.Duration, error) {
	if c.ArchiveThreshold == "" {
		return 0, nil
	}
	return ParseDuration(c.ArchiveThreshold)
}

// ArchiveIntervalDuration parses the archive_interval string.
// Returns the default 6 hours if empty.
func (c *DaemonConfig) ArchiveIntervalDuration() (time.Duration, error) {
	if c.ArchiveInterval == "" {
		return 6 * time.Hour, nil
	}
	return ParseDuration(c.ArchiveInterval)
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

// ValidateAuth checks the TCP listener's authentication/encryption configuration.
// It returns a fatal error for a plaintext TCP listener with no auth_token — any
// host on the network could read and write the API in the clear, including the
// destructive maintenance endpoints — and a non-fatal warning for a TLS listener
// with no token (encrypted and fingerprint-pinned, but unauthenticated).
func (c *DaemonConfig) ValidateAuth() (warn string, err error) {
	if c.Listen == "" || c.AuthToken != "" {
		return "", nil
	}
	if c.TLSDisabled {
		return "", fmt.Errorf("refusing to start: plaintext TCP listener %q has no auth_token; "+
			"set auth_token, or remove tls_disabled to use the auto-generated TLS certificate", c.Listen)
	}
	return fmt.Sprintf("TLS TCP listener %s enabled without auth_token; any client that trusts the certificate can connect", c.Listen), nil
}

// Load reads and parses a TOML config file. A missing file is not fatal: it
// returns a config with defaults applied, so a client invocation (e.g.
// `bewitch -addr host:9119 -token X`) works on a machine with no
// /etc/bewitch.toml, and the daemon can come up on defaults. A file that exists
// but cannot be read or parsed is still an error.
func Load(path string) (*Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing config: %w", err)
		}
	case os.IsNotExist(err):
		// Proceed with defaults (applied below).
	default:
		return nil, fmt.Errorf("reading config: %w", err)
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
