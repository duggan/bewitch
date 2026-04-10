package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"1s", time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"24h", 24 * time.Hour, false},
		{"100ms", 100 * time.Millisecond, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"0d", 0, false},
		{"1d", 24 * time.Hour, false},
		{"", 0, true},
		{"bad", 0, true},
		{"1x", 0, true},
		{"d", 0, true},   // no number before d
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCollectorInterval(t *testing.T) {
	defaultInterval := 5 * time.Second

	tests := []struct {
		name       string
		configured string
		want       time.Duration
	}{
		{"empty falls back to default", "", defaultInterval},
		{"valid duration", "2s", 2 * time.Second},
		{"valid duration minutes", "1m", time.Minute},
		{"below minimum clamps to 100ms", "50ms", 100 * time.Millisecond},
		{"exactly minimum", "100ms", 100 * time.Millisecond},
		{"invalid string falls back", "bad", defaultInterval},
		{"day format works", "1d", 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectorInterval(tt.configured, defaultInterval)
			if got != tt.want {
				t.Errorf("collectorInterval(%q, %v) = %v, want %v", tt.configured, defaultInterval, got, tt.want)
			}
		})
	}
}

func TestGetInterval(t *testing.T) {
	def := 5 * time.Second

	// All collector config types delegate to collectorInterval.
	// Test a representative sample.
	cpu := CPUCollectorConfig{Interval: "2s"}
	if got := cpu.GetInterval(def); got != 2*time.Second {
		t.Errorf("CPU GetInterval = %v, want 2s", got)
	}

	mem := MemoryCollectorConfig{Interval: ""}
	if got := mem.GetInterval(def); got != def {
		t.Errorf("Memory GetInterval = %v, want %v", got, def)
	}

	disk := DiskCollectorConfig{Interval: "10ms"} // below minimum
	if got := disk.GetInterval(def); got != 100*time.Millisecond {
		t.Errorf("Disk GetInterval = %v, want 100ms", got)
	}

	net := NetworkCollectorConfig{Interval: "1m"}
	if got := net.GetInterval(def); got != time.Minute {
		t.Errorf("Network GetInterval = %v, want 1m", got)
	}

	ecc := ECCCollectorConfig{Interval: "invalid"}
	if got := ecc.GetInterval(def); got != def {
		t.Errorf("ECC GetInterval = %v, want %v", got, def)
	}

	proc := ProcessCollectorConfig{Interval: "500ms"}
	if got := proc.GetInterval(def); got != 500*time.Millisecond {
		t.Errorf("Process GetInterval = %v, want 500ms", got)
	}

	temp := TemperatureCollectorConfig{Interval: "3s"}
	if got := temp.GetInterval(def); got != 3*time.Second {
		t.Errorf("Temperature GetInterval = %v, want 3s", got)
	}

	power := PowerCollectorConfig{Interval: "7s"}
	if got := power.GetInterval(def); got != 7*time.Second {
		t.Errorf("Power GetInterval = %v, want 7s", got)
	}
}

func TestTemperatureCollectorIsEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", &trueVal, true},
		{"explicit false", &falseVal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := TemperatureCollectorConfig{Enabled: tt.enabled}
			if got := c.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPowerCollectorIsEnabled(t *testing.T) {
	trueVal := true
	falseVal := false

	tests := []struct {
		name    string
		enabled *bool
		want    bool
	}{
		{"nil defaults to true", nil, true},
		{"explicit true", &trueVal, true},
		{"explicit false", &falseVal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := PowerCollectorConfig{Enabled: tt.enabled}
			if got := c.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetMaxProcesses(t *testing.T) {
	tests := []struct {
		name string
		max  int
		want int
	}{
		{"zero returns default", 0, DefaultMaxProcesses},
		{"negative returns default", -5, DefaultMaxProcesses},
		{"positive returns value", 50, 50},
		{"large value", 500, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ProcessCollectorConfig{MaxProcesses: tt.max}
			if got := c.GetMaxProcesses(); got != tt.want {
				t.Errorf("GetMaxProcesses() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGetDiskExcludes(t *testing.T) {
	tests := []struct {
		name string
		cfg  DiskCollectorConfig
		want []string
	}{
		{
			"defaults only",
			DiskCollectorConfig{},
			DefaultDiskExcludes,
		},
		{
			"merge user with defaults",
			DiskCollectorConfig{ExcludeMounts: []string{"/boot/"}},
			[]string{"/snap/", "/run/", "/boot/"},
		},
		{
			"no default excludes",
			DiskCollectorConfig{NoDefaultExcludes: true, ExcludeMounts: []string{"/custom/"}},
			[]string{"/custom/"},
		},
		{
			"no default excludes empty",
			DiskCollectorConfig{NoDefaultExcludes: true},
			nil,
		},
		{
			"dedup user and defaults",
			DiskCollectorConfig{ExcludeMounts: []string{"/snap/", "/extra/"}},
			[]string{"/snap/", "/run/", "/extra/"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetDiskExcludes()
			if len(got) != len(tt.want) {
				t.Errorf("GetDiskExcludes() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("GetDiskExcludes()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestGetSMARTInterval(t *testing.T) {
	tests := []struct {
		name string
		cfg  DiskCollectorConfig
		want time.Duration
	}{
		{"empty defaults to 5m", DiskCollectorConfig{}, 5 * time.Minute},
		{"valid interval", DiskCollectorConfig{SMARTInterval: "2m"}, 2 * time.Minute},
		{"below minimum clamps to 30s", DiskCollectorConfig{SMARTInterval: "10s"}, 30 * time.Second},
		{"exactly minimum", DiskCollectorConfig{SMARTInterval: "30s"}, 30 * time.Second},
		{"invalid falls back to 5m", DiskCollectorConfig{SMARTInterval: "bad"}, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetSMARTInterval(); got != tt.want {
				t.Errorf("GetSMARTInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseHistoryRanges(t *testing.T) {
	t.Run("empty returns defaults", func(t *testing.T) {
		c := TUIConfig{}
		got, err := c.ParseHistoryRanges()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(DefaultHistoryRanges) {
			t.Errorf("len = %d, want %d", len(got), len(DefaultHistoryRanges))
		}
		for i, r := range got {
			if r.Label != DefaultHistoryRanges[i].Label || r.Duration != DefaultHistoryRanges[i].Duration {
				t.Errorf("[%d] = %+v, want %+v", i, r, DefaultHistoryRanges[i])
			}
		}
	})

	t.Run("valid ranges", func(t *testing.T) {
		c := TUIConfig{HistoryRanges: []string{"1h", "6h", "7d"}}
		got, err := c.ParseHistoryRanges()
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("len = %d, want 3", len(got))
		}
		if got[0].Duration != time.Hour {
			t.Errorf("[0] duration = %v, want 1h", got[0].Duration)
		}
		if got[2].Duration != 7*24*time.Hour {
			t.Errorf("[2] duration = %v, want 168h", got[2].Duration)
		}
	})

	t.Run("invalid entry returns error", func(t *testing.T) {
		c := TUIConfig{HistoryRanges: []string{"1h", "bad"}}
		_, err := c.ParseHistoryRanges()
		if err == nil {
			t.Error("expected error for invalid range")
		}
	})
}

func TestDaemonConfigDurations(t *testing.T) {
	t.Run("DefaultCollectionInterval", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			want    time.Duration
			wantErr bool
		}{
			{"empty defaults to 5s", "", 5 * time.Second, false},
			{"valid", "1s", time.Second, false},
			{"below minimum", "50ms", 100 * time.Millisecond, false},
			{"invalid", "bad", 0, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c := DaemonConfig{DefaultInterval: tt.input}
				got, err := c.DefaultCollectionInterval()
				if (err != nil) != tt.wantErr {
					t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if got != tt.want {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("RetentionDuration", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			want    time.Duration
			wantErr bool
		}{
			{"empty = keep forever", "", 0, false},
			{"30 days", "30d", 30 * 24 * time.Hour, false},
			{"720 hours", "720h", 720 * time.Hour, false},
			{"invalid", "bad", 0, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				c := DaemonConfig{Retention: tt.input}
				got, err := c.RetentionDuration()
				if (err != nil) != tt.wantErr {
					t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				if got != tt.want {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("PruneDuration", func(t *testing.T) {
		c := DaemonConfig{}
		got, _ := c.PruneDuration()
		if got != time.Hour {
			t.Errorf("default = %v, want 1h", got)
		}
		c.PruneInterval = "30m"
		got, _ = c.PruneDuration()
		if got != 30*time.Minute {
			t.Errorf("30m = %v, want 30m", got)
		}
	})

	t.Run("CompactionDuration", func(t *testing.T) {
		c := DaemonConfig{}
		got, _ := c.CompactionDuration()
		if got != 0 {
			t.Errorf("default = %v, want 0 (disabled)", got)
		}
		c.CompactionInterval = "7d"
		got, _ = c.CompactionDuration()
		if got != 7*24*time.Hour {
			t.Errorf("7d = %v, want 168h", got)
		}
	})

	t.Run("CheckpointDuration", func(t *testing.T) {
		c := DaemonConfig{}
		got, _ := c.CheckpointDuration()
		if got != 0 {
			t.Errorf("default = %v, want 0", got)
		}
		c.CheckpointInterval = "5m"
		got, _ = c.CheckpointDuration()
		if got != 5*time.Minute {
			t.Errorf("5m = %v, want 5m", got)
		}
	})

	t.Run("ArchiveThresholdDuration", func(t *testing.T) {
		c := DaemonConfig{}
		got, _ := c.ArchiveThresholdDuration()
		if got != 0 {
			t.Errorf("default = %v, want 0", got)
		}
		c.ArchiveThreshold = "7d"
		got, _ = c.ArchiveThresholdDuration()
		if got != 7*24*time.Hour {
			t.Errorf("7d = %v, want 168h", got)
		}
	})

	t.Run("ArchiveIntervalDuration", func(t *testing.T) {
		c := DaemonConfig{}
		got, _ := c.ArchiveIntervalDuration()
		if got != 6*time.Hour {
			t.Errorf("default = %v, want 6h", got)
		}
		c.ArchiveInterval = "12h"
		got, _ = c.ArchiveIntervalDuration()
		if got != 12*time.Hour {
			t.Errorf("12h = %v, want 12h", got)
		}
	})
}

func TestValidateTLS(t *testing.T) {
	tests := []struct {
		name    string
		cert    string
		key     string
		wantErr bool
	}{
		{"both empty is valid", "", "", false},
		{"both set is valid", "/cert.pem", "/key.pem", false},
		{"cert only is error", "/cert.pem", "", true},
		{"key only is error", "", "/key.pem", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DaemonConfig{TLSCert: tt.cert, TLSKey: tt.key}
			err := c.ValidateTLS()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTLS() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Run("minimal config with defaults", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Daemon.Socket != "/run/bewitch/bewitch.sock" {
			t.Errorf("socket = %q, want default", cfg.Daemon.Socket)
		}
		if cfg.Daemon.DBPath != "/var/lib/bewitch/bewitch.duckdb" {
			t.Errorf("db_path = %q, want default", cfg.Daemon.DBPath)
		}
	})

	t.Run("archive path derived from db_path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]
db_path = "/data/bewitch.duckdb"
archive_threshold = "7d"
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Daemon.ArchivePath != "/data/archive" {
			t.Errorf("archive_path = %q, want /data/archive", cfg.Daemon.ArchivePath)
		}
	})

	t.Run("explicit values override defaults", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]
socket = "/tmp/test.sock"
db_path = "/tmp/test.db"
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Daemon.Socket != "/tmp/test.sock" {
			t.Errorf("socket = %q", cfg.Daemon.Socket)
		}
		if cfg.Daemon.DBPath != "/tmp/test.db" {
			t.Errorf("db_path = %q", cfg.Daemon.DBPath)
		}
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		_, err := Load("/nonexistent/path.toml")
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("log_level parsed from config", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte("[daemon]\nlog_level = \"debug\"\n"), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Daemon.LogLevel != "debug" {
			t.Errorf("log_level = %q, want debug", cfg.Daemon.LogLevel)
		}
	})

	t.Run("log_level defaults to empty", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte("[daemon]\n"), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Daemon.LogLevel != "" {
			t.Errorf("log_level = %q, want empty", cfg.Daemon.LogLevel)
		}
	})

	t.Run("tls fields parsed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]
tls_cert = "/etc/bewitch/cert.pem"
tls_key = "/etc/bewitch/key.pem"
tls_disabled = true
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Daemon.TLSCert != "/etc/bewitch/cert.pem" {
			t.Errorf("tls_cert = %q", cfg.Daemon.TLSCert)
		}
		if cfg.Daemon.TLSKey != "/etc/bewitch/key.pem" {
			t.Errorf("tls_key = %q", cfg.Daemon.TLSKey)
		}
		if !cfg.Daemon.TLSDisabled {
			t.Error("tls_disabled should be true")
		}
	})

	t.Run("tls_cert without tls_key is error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]
tls_cert = "/etc/bewitch/cert.pem"
`), 0644)
		_, err := Load(path)
		if err == nil {
			t.Error("expected error when tls_cert set without tls_key")
		}
	})

	t.Run("tls_key without tls_cert is error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]
tls_key = "/etc/bewitch/key.pem"
`), 0644)
		_, err := Load(path)
		if err == nil {
			t.Error("expected error when tls_key set without tls_cert")
		}
	})

	t.Run("auth_token parsed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]
auth_token = "my-secret"
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Daemon.AuthToken != "my-secret" {
			t.Errorf("auth_token = %q, want my-secret", cfg.Daemon.AuthToken)
		}
	})
}

func TestLoadNotificationConfig(t *testing.T) {
	t.Run("email destinations", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]

[[alerts.email]]
smtp_host = "smtp.example.com"
smtp_port = 465
username = "user"
password = "pass"
from = "alerts@example.com"
to = ["admin@example.com", "ops@example.com"]
starttls = false
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Alerts.Email) != 1 {
			t.Fatalf("expected 1 email dest, got %d", len(cfg.Alerts.Email))
		}
		e := cfg.Alerts.Email[0]
		if e.SMTPHost != "smtp.example.com" {
			t.Errorf("smtp_host = %q", e.SMTPHost)
		}
		if e.GetSMTPPort() != 465 {
			t.Errorf("smtp_port = %d", e.GetSMTPPort())
		}
		if e.IsStartTLS() != false {
			t.Error("starttls should be false")
		}
		if len(e.To) != 2 {
			t.Errorf("to has %d recipients, want 2", len(e.To))
		}
	})

	t.Run("email with use_mail_cmd", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]

[[alerts.email]]
use_mail_cmd = true
from = "bewitch@myserver.local"
to = ["admin@example.com"]
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Alerts.Email) != 1 {
			t.Fatalf("expected 1 email dest, got %d", len(cfg.Alerts.Email))
		}
		e := cfg.Alerts.Email[0]
		if !e.UseMailCmd {
			t.Error("use_mail_cmd should be true")
		}
		if e.From != "bewitch@myserver.local" {
			t.Errorf("from = %q", e.From)
		}
		if len(e.To) != 1 || e.To[0] != "admin@example.com" {
			t.Errorf("to = %v", e.To)
		}
	})

	t.Run("command destinations", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]

[[alerts.commands]]
cmd = "/usr/local/bin/alert-handler"
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Alerts.Commands) != 1 {
			t.Fatalf("expected 1 command dest, got %d", len(cfg.Alerts.Commands))
		}
		if cfg.Alerts.Commands[0].Cmd != "/usr/local/bin/alert-handler" {
			t.Errorf("cmd = %q", cfg.Alerts.Commands[0].Cmd)
		}
	})

	t.Run("multiple notification types coexist", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.toml")
		os.WriteFile(path, []byte(`[daemon]

[[alerts.email]]
use_mail_cmd = true
to = ["admin@example.com"]

[[alerts.commands]]
cmd = "notify-send"
`), 0644)
		cfg, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(cfg.Alerts.Email) != 1 {
			t.Errorf("email: %d, want 1", len(cfg.Alerts.Email))
		}
		if len(cfg.Alerts.Commands) != 1 {
			t.Errorf("commands: %d, want 1", len(cfg.Alerts.Commands))
		}
	})
}

func TestCaptureConfigGetDPI(t *testing.T) {
	tests := []struct {
		name string
		dpi  int
		want int
	}{
		{"zero defaults to 144", 0, 144},
		{"negative defaults to 144", -1, 144},
		{"72 returns 72", 72, 72},
		{"144 returns 144", 144, 144},
		{"216 returns 216", 216, 216},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := CaptureConfig{DPI: tt.dpi}
			if got := c.GetDPI(); got != tt.want {
				t.Errorf("GetDPI() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCaptureConfigGetCompression(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty defaults to best", "", "best"},
		{"best", "best", "best"},
		{"default", "default", "default"},
		{"none", "none", "none"},
		{"invalid defaults to best", "turbo", "best"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := CaptureConfig{Compression: tt.input}
			if got := c.GetCompression(); got != tt.want {
				t.Errorf("GetCompression() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadCaptureConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	os.WriteFile(path, []byte(`[daemon]

[tui.capture]
directory = "~/screenshots"
dpi = 216
compression = "none"
background = "#000000"
foreground = "#FFFFFF"
`), 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cap := cfg.TUI.Capture
	if cap.Directory != "~/screenshots" {
		t.Errorf("directory = %q", cap.Directory)
	}
	if cap.GetDPI() != 216 {
		t.Errorf("dpi = %d", cap.GetDPI())
	}
	if cap.GetCompression() != "none" {
		t.Errorf("compression = %q", cap.GetCompression())
	}
	if cap.Background != "#000000" {
		t.Errorf("background = %q", cap.Background)
	}
	if cap.Foreground != "#FFFFFF" {
		t.Errorf("foreground = %q", cap.Foreground)
	}
}

func TestValidateAuth(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		token    string
		wantWarn bool
	}{
		{"listen with token", ":9119", "secret", false},
		{"listen without token", ":9119", "", true},
		{"no listen without token", "", "", false},
		{"no listen with token", "", "secret", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DaemonConfig{Listen: tt.listen, AuthToken: tt.token}
			warn := c.ValidateAuth()
			if (warn != "") != tt.wantWarn {
				t.Errorf("ValidateAuth() = %q, wantWarn %v", warn, tt.wantWarn)
			}
		})
	}
}
