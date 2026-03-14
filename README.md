# Bewitch

A charming server monitoring system for Linux, built with Go.

Bewitch is comprised of two applications:

- **bewitchd** — a daemon that continuously collects system metrics and stores them in DuckDB
- **bewitch** — a TUI that provides a rich, interactive interface to the collected data

## Features

- **Metrics collection** — CPU (per-core), memory, disk (space + I/O + SMART health), network, ECC errors, temperature sensors, power consumption (powercap/RAPL), process tracking (all processes visible, top N enriched with full details)
- **Process pinning** — pin processes by glob pattern (config or TUI) to always collect full metrics, regardless of CPU/memory ranking
- **Per-collector intervals** — each collector has a configurable collection interval (e.g., CPU at 1s, disk at 30s, ECC at 60s) with a global default; failing collectors automatically back off exponentially and recover on success
- **Persistent storage** — DuckDB with automatic WAL checkpointing, optional retention pruning, scheduled or on-demand compaction, and Parquet archival for long-term storage
- **Alerting** — threshold, predictive (linear regression), and variance alert rules, manageable from the TUI
- **Webhook delivery** — alert notifications to external services
- **TUI dashboard** — real-time system overview with detail views per subsystem; status bar indicates stale data when a collector stops producing updates
- **Historical charts** — high-resolution braille charts for CPU, memory, disk, hardware (temperature/power), and process CPU with selectable time ranges and dynamic height
- **Interactive SQL REPL** — `bewitch repl` opens an interactive DuckDB query console against the daemon, with multi-line editing, dot-commands (`.tables`, `.schema`, `.metrics`, `.dimensions`, `.export`), persistent history, tab completion, piped input support, and read-only enforcement via DuckDB's statement parser
- **Remote access with TLS** — optional TCP listener with auto-generated self-signed certificates, SSH-style trust-on-first-use fingerprint pinning, and bearer token authentication
- **Unix socket API** — daemon control without network exposure, JSON API

## macOS development (mock mode)

Bewitch targets Linux but supports a **mock mode** for developing and testing the TUI on macOS. Mock mode generates synthetic metrics from a simulated 8-core server with 32 GB RAM, two disks, two network interfaces, temperature sensors, power zones, and ~65 processes.

```toml
[daemon]
mock = true
socket = "/tmp/bewitch.sock"
db_path = "/tmp/bewitch.duckdb"
```

```
make build
bin/bewitchd -config dev.toml &
bin/bewitch -config dev.toml
```

All TUI features work normally — live metrics, history charts (accumulate over time in DuckDB), process pinning, alerts, etc. The synthetic data uses smooth sine waves with per-metric phase offsets and jitter for a realistic feel.

## Testing

```
make test           # run all unit tests
make test-verbose   # verbose output
make test-integration  # integration tests (requires DuckDB/CGO)
```

Unit tests cover config parsing, alert logic, CPU/disk/SMART data processing, API serialization round-trips, history bucketing, and TUI formatting. UI tests use Charm's [teatest](https://github.com/charmbracelet/x/tree/main/exp/teatest) library with a mock daemon client to verify view switching, tab cycling, history range controls, hardware tab visibility, and process view interactions. All tests are cross-platform and pass on both Linux and macOS.

## Requirements

- Go 1.21+
- Linux (Debian/Ubuntu targeted, uses procfs and sysfs); macOS supported via mock mode
- DuckDB (bundled via duckdb-go)

## Build

```
make build
```

Produces `bin/bewitchd` and `bin/bewitch`.

## Install

### Debian/Ubuntu package (recommended)

Build and install the `.deb` package:

```
make deb
sudo dpkg -i ../bewitch_0.1.0-1_*.deb
```

This automatically:
- Creates the `bewitch` system user/group
- Installs binaries to `/usr/local/bin/`
- Sets up `/var/lib/bewitch/` with correct ownership
- Installs the systemd service
- Copies example config to `/etc/bewitch.toml`

### Manual install

```
sudo make install
sudo useradd -r -s /usr/sbin/nologin bewitch
sudo mkdir -p /var/lib/bewitch
sudo chown bewitch:bewitch /var/lib/bewitch
sudo cp bewitch.example.toml /etc/bewitch.toml
sudo cp bewitchd.service /etc/systemd/system/
```

## Configuration

Key settings in `/etc/bewitch.toml`:

```toml
[daemon]
# socket defaults to /run/bewitch/bewitch.sock (created by systemd RuntimeDirectory)
db_path = "/var/lib/bewitch/bewitch.duckdb"
# log_level = "info"       # log level: debug, info, warn, error
# default_interval = "5s"  # default collection interval for all collectors; minimum 100ms
# retention = "30d"       # delete metrics older than this; empty = keep forever
# prune_interval = "1h"   # how often to delete old data (requires retention)
# compaction_interval = "7d"  # full DB rebuild interval; empty = manual only
# checkpoint_threshold = "16MB"  # DuckDB WAL auto-checkpoint size
# archive_threshold = "7d"  # archive data older than this to Parquet
# archive_interval = "6h"   # how often to run archive; default "6h"
# archive_path = "/var/lib/bewitch/archive"  # directory for Parquet files

[alerts]
evaluation_interval = "10s"
# Alert rules are managed via the TUI (Alerts tab, press 'n')

[[alerts.webhooks]]
url = "https://hooks.example.com/alert"

[tui]
refresh_interval = "2s"

[collectors.cpu]
# interval = "1s"  # override default_interval for CPU collection

[collectors.memory]
# interval = "5s"

[collectors.disk]
# interval = "30s"
# exclude_mounts = ["/boot/efi"]  # add to default exclusions
# no_default_excludes = false     # set true to disable defaults
# smart_interval = "5m"           # SMART health polling interval (0 to disable)

[collectors.network]
# interval = "5s"

[collectors.ecc]
# interval = "60s"

[collectors.temperature]
# interval = "5s"
# enabled = true  # set to false to disable temperature collection

[collectors.power]
# interval = "5s"
# enabled = true  # set to false to disable power collection

[collectors.process]
# interval = "5s"
# max_processes = 100  # maximum number of processes to enrich per collection cycle
# pinned = ["nginx*", "postgres", "redis-server"]  # glob patterns to always enrich
```

### Enabling/disabling collectors

Temperature and power collectors can be disabled in the config. This is useful for machines without the relevant hardware, or to reduce overhead:

```toml
[collectors.temperature]
enabled = false

[collectors.power]
enabled = false
```

When a collector is disabled, its sub-section within the Hardware tab shows a "no data" message. The Hardware tab itself is always visible.

### Disk mount filtering

By default, `/snap/` and `/run/` mount paths are excluded from collection. This prevents Ubuntu snap loopback mounts from cluttering the dashboard. To add additional exclusions:

```toml
[collectors.disk]
exclude_mounts = ["/boot/efi", "/mnt/backup"]
```

To disable defaults and only use your explicit list:

```toml
[collectors.disk]
exclude_mounts = ["/", "/home"]
no_default_excludes = true
```

### SMART disk health

The disk collector reads SMART health data directly from physical block devices using `smart.Open()` (via the `anatol/smart.go` library). SMART data is live-only — it is not stored in the database since it changes slowly.

SMART reads require `CAP_SYS_RAWIO` capability (the Debian package configures this automatically). When the capability is not available, SMART fields are simply absent and the disk view renders identically to before.

Each disk panel in the TUI gains up to 3 extra lines when SMART is available: health status (OK/FAILING), temperature, NVMe spare/used percentages, power-on hours, power cycles, and error counters (reallocated/pending/uncorrectable sectors). Non-zero error counters are highlighted in orange.

SMART is read per physical device (e.g., `/dev/nvme0n1`), not per partition. Multiple mount points from the same disk share one SMART read. The polling interval is configurable:

```toml
[collectors.disk]
smart_interval = "5m"  # default 5m, minimum 30s, set to "0" to disable
```

### Process tracking and pinning

The process collector works in two phases. Phase 1 scans every process on the system cheaply (reading `/proc/[pid]/stat` only), collecting PID, name, state, CPU%, RSS, and thread count. Phase 2 enriches a subset with expensive data: cmdline, UID, FD count, and detailed memory breakdown.

By default, the top 100 processes by resource usage are enriched. **All** processes are visible in the TUI process list — non-enriched processes show `--` for cmdline and FD count.

To adjust the enrichment limit:

```toml
[collectors.process]
max_processes = 50  # enrich fewer processes to reduce storage
```

**Process pinning** ensures specific processes always receive full enrichment and DB storage, regardless of their CPU/memory ranking. This is useful for monitoring low-resource but critical services. Pinned processes also appear in history charts and can be used in alert rules.

Pin via config (glob patterns):

```toml
[collectors.process]
pinned = ["nginx*", "postgres", "redis-server"]
```

Pin interactively in the TUI: navigate to a process and press `*`. TUI pins are stored in the daemon's preferences DB and persist across restarts. A `*` indicator appears next to pinned processes in the process list.

## Usage

### Running the daemon

```
bewitchd -config /etc/bewitch.toml
```

The `-log-level` flag overrides the config file setting:

```
bewitchd -config /etc/bewitch.toml -log-level debug
```

Or with systemd:

```
sudo cp bewitchd.service /etc/systemd/system/
sudo systemctl enable --now bewitchd
```

### Database compaction

Compaction rebuilds the database file to reclaim fragmented space. It can run automatically on a schedule or be triggered manually.

**Scheduled compaction:** Set `compaction_interval` in the config (e.g., `"7d"` for weekly).

**Manual compaction:**

```
bewitch -config /etc/bewitch.toml compact
```

### Interactive SQL REPL

`bewitch repl` connects to the running daemon and provides an interactive DuckDB SQL console for ad-hoc queries against collected metrics. The editor supports full multi-line editing — you can arrow up/down between lines, edit earlier lines, and the input area auto-resizes as you type.

```
bewitch -config /etc/bewitch.toml repl
```

```
bewitch sql — interactive DuckDB query console
Connected to daemon at /run/bewitch/bewitch.sock
Rows:     cpu: 1234567, memory: 245678, disk: 89012, network: 67890, process: 345678
Type .help for commands, Ctrl+D to exit.

bewitch> SELECT d.value AS mount, AVG(m.used_bytes * 100.0 / m.total_bytes) AS used_pct
    ...> FROM disk_metrics m JOIN dimension_values d ON d.category = 'mount' AND d.id = m.mount_id
    ...> WHERE m.ts > now() - INTERVAL '1 hour' GROUP BY d.value;
 mount | used_pct
-------+---------
 /     |    62.34
 /home |    41.17
(2 rows)
```

SQL statements are terminated with `;`. Until a semicolon is entered, pressing Enter adds a new line (the prompt changes to `...>`). Tab triggers context-aware completion using DuckDB's built-in `sql_auto_complete()`.

**REPL key bindings:**

| Key | Action |
|-----|--------|
| Tab | Autocomplete (SQL keywords, table names, dot-commands) |
| Ctrl+D | Exit |
| Ctrl+C | Cancel current input |
| Ctrl+R | Reverse search history |
| Alt+P / Alt+N | Navigate history (previous / next) |

**Dot-commands:**

| Command | Description |
|---------|-------------|
| `.metrics` | Metric tables with row counts and time ranges (prefers `all_*` archive views) |
| `.tables` | List all tables with row counts |
| `.schema [table]` | Show column definitions |
| `.count [table]` | Row counts with time ranges |
| `.dimensions` | Show dimension lookup values (mount names, sensors, interfaces, zones) |
| `.export <table> <path>` | Export table to file (csv, parquet, json — inferred from extension) |
| `.export (<sql>) <path>` | Export query results to file |
| `.help` | Show available commands and example queries |
| `.quit` | Exit |

Only read-only queries (SELECT, EXPLAIN, PRAGMA) are allowed — write/DDL statements are rejected server-side using DuckDB's statement parser.

**Piped input** works for scripting:

```bash
echo "SELECT COUNT(*) FROM cpu_metrics;" | bewitch -config /etc/bewitch.toml repl
```

**Exporting data:**

```bash
bewitch> .export all_cpu_metrics /tmp/cpu.csv
Exported 123456 rows to /tmp/cpu.csv

bewitch> .export (SELECT * FROM all_cpu_metrics WHERE ts > now() - INTERVAL '1 hour') /tmp/recent.parquet
Exported 720 rows to /tmp/recent.parquet
```

Metric tables use normalized dimension IDs — use `.dimensions` to see the mapping, or JOIN with `dimension_values` in queries (as shown in the `.help` examples).

### Database snapshots

For advanced analysis — complex ad-hoc queries, sharing data with colleagues, or offline analysis in tools like DBeaver or Jupyter — create a standalone DuckDB snapshot:

```
bewitch -config /etc/bewitch.toml snapshot /tmp/metrics.duckdb
```

The snapshot merges the live database and any archived Parquet data into a single self-contained DuckDB file. It includes all metric tables and dimension lookups, but excludes daemon-internal state (alerts, preferences, scheduled jobs) by default.

For ad-hoc backups that include everything (alerts, alert rules, preferences, etc.):

```
bewitch snapshot -with-system-tables /tmp/backup.duckdb
```

Open the snapshot directly with the DuckDB CLI, DBeaver, Jupyter, or any DuckDB-compatible tool — no running daemon needed:

```bash
duckdb /tmp/metrics.duckdb "SELECT COUNT(*) FROM cpu_metrics"
```

### Parquet archival

For long-term storage efficiency, bewitch can archive older metrics data to Parquet files. Archived data is compressed with zstd (~10x smaller than DuckDB) and remains queryable through the history API.

**How it works:**
- Data older than `archive_threshold` is exported to monthly Parquet files
- Exported data is deleted from DuckDB to save space
- History API queries automatically combine DuckDB and Parquet data
- Old Parquet files are deleted based on `retention` setting

**Configuration:**

```toml
[daemon]
archive_threshold = "7d"   # archive data older than 7 days
archive_interval = "6h"    # run archive every 6 hours
archive_path = "/var/lib/bewitch/archive"
retention = "90d"          # delete Parquet files older than 90 days
```

**Manual archive/unarchive:**

```
bewitch -config /etc/bewitch.toml archive
bewitch -config /etc/bewitch.toml unarchive
```

`archive` exports data older than `archive_threshold` to Parquet and deletes it from DuckDB. `unarchive` reloads all Parquet data back into DuckDB, removes the Parquet files, and resets the archive state — useful for changing partition strategies or disabling archival.

### Remote access

The daemon can listen on TCP for remote TUI, REPL, and CLI access. TCP connections use TLS by default with auto-generated self-signed certificates.

**Daemon config:**

```toml
[daemon]
listen = ":9119"  # enable TCP listener
auth_token = "my-secret-token"  # require clients to authenticate
# tls_cert = "/etc/bewitch/tls/cert.pem"  # optional: use your own cert
# tls_key = "/etc/bewitch/tls/key.pem"
# tls_disabled = false  # set true for plain TCP (not recommended)
```

On first start with TCP enabled, the daemon generates a self-signed certificate and persists it next to the database (e.g., `/var/lib/bewitch/tls-cert.pem`). The certificate is reused on subsequent starts so the fingerprint remains stable. The fingerprint is logged at startup.

When `auth_token` is set, all TCP connections must include the token. Unix socket connections are never authenticated (filesystem permissions are sufficient). The daemon logs a warning at startup if TCP is enabled without an auth token.

**Connecting remotely:**

```
bewitch -addr myserver:9119 -token my-secret-token
```

If the daemon and client share the same config file (e.g., on the same machine), the token is read from the config automatically — no `-token` flag needed.

On first connection, the client displays the server's certificate fingerprint and asks you to verify it (like SSH):

```
TLS fingerprint for myserver:9119:
  sha256:a1b2c3d4e5f6...
Trust this server? [y/N]: y
```

Accepted fingerprints are saved to `~/.config/bewitch/known_hosts`. On subsequent connections, the fingerprint is verified silently. If the server's certificate changes unexpectedly, the connection is refused:

```
TLS: server fingerprint changed!
  Expected: sha256:a1b2c3d4...
  Got:      sha256:e5f6a7b8...
If this is expected, reconnect with -tls-reset-fingerprint to update.
```

**Client flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `-token` | `""` | Bearer token for TCP authentication (falls back to config) |
| `-tls` | `true` | Use TLS for TCP connections |
| `-tls-skip-verify` | `false` | Skip fingerprint verification |
| `-tls-reset-fingerprint` | `false` | Update stored fingerprint for this server |

All subcommands support remote access:

```
bewitch -addr myserver:9119 -token secret repl
bewitch -addr myserver:9119 -token secret compact
bewitch -addr myserver:9119 -token secret archive
bewitch -addr myserver:9119 -token secret snapshot /tmp/remote-metrics.duckdb
```

### Running the TUI

```
bewitch -config /etc/bewitch.toml
```

#### Debug mode

Launch with `-debug` to enable a debug console at the bottom of the TUI. It shows a scrollable ring buffer of timestamped diagnostic messages covering data fetches, cache hits/misses, view transitions, errors, and process pin operations.

```
bewitch -config /etc/bewitch.toml -debug
```

### TUI navigation

Views are accessed via number keys. Tab numbering is fixed regardless of hardware availability.

| Key | View |
|-----|------|
| 1   | Dashboard (overview) |
| 2   | CPU details (per-core) |
| 3   | Memory details |
| 4   | Disk details (per-mount with I/O and SMART health) |
| 5   | Network details (per-interface) |
| 6   | Hardware (temperature, power, ECC sub-sections) |
| 7   | Process details |
| 8   | Alerts |
| ← / → or h / l | Cycle views forward / backward |
| < / > | Cycle history time range (1h, 6h, 24h, 7d, 30d) |
| Tab / Shift+Tab (hardware) | Cycle sub-sections (Temperature, Power, ECC) |
| j / k (hardware) | Navigate sensor/zone list |
| Space (hardware) | Toggle sensor/zone on/off in chart |
| a (hardware) | Select / deselect all sensors/zones |
| j / k (alerts) | Navigate rules list or alert rows |
| Tab (alerts) | Switch focus between rules and fired alerts |
| n (alerts) | Create a new alert rule |
| d (alerts) | Delete selected rule |
| Space (alerts) | Toggle rule enabled/disabled |
| Enter (alerts) | Acknowledge selected alert |
| Esc (alerts) | Cancel alert creation form |
| j / k (process) | Navigate process list |
| c / m / p / n / t / f (process) | Sort by CPU / mem / PID / name / threads / FDs |
| * (process) | Pin/unpin selected process |
| a (process) | Create alert for selected process |
| / (process) | Search processes by name or cmdline |
| Esc (process) | Clear search filter |
| P (process) | Toggle pinned-only filter in process table |
| Tab (process) | Toggle history chart between Top CPU and Pinned |
| { / } (debug) | Scroll debug console up / down |
| ( / ) (debug) | Shrink / grow debug console |
| q   | Quit |

### API

The daemon exposes a JSON HTTP API over its unix socket. POST endpoints accept JSON request bodies with `Content-Type: application/json`.

Endpoints:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/status` | Daemon status and uptime |
| GET | `/api/alerts` | List alerts (`?ack=false` for unacknowledged) |
| POST | `/api/alerts/{id}/ack` | Acknowledge an alert |
| GET | `/api/config` | Get full configuration |
| GET | `/api/metrics/cpu` | CPU per-core metrics |
| GET | `/api/metrics/memory` | Memory metrics |
| GET | `/api/metrics/disk` | Disk space, I/O, and SMART health metrics |
| GET | `/api/metrics/network` | Network per-interface metrics |
| GET | `/api/metrics/temperature` | Temperature sensor readings |
| GET | `/api/metrics/power` | Power consumption per zone (watts) |
| GET | `/api/metrics/dashboard` | Combined dashboard data |
| GET | `/api/history/cpu` | Aggregated CPU history (`?start=&end=` unix seconds) |
| GET | `/api/history/memory` | Aggregated memory history |
| GET | `/api/history/disk` | Aggregated disk history |
| GET | `/api/history/temperature` | Aggregated temperature history |
| GET | `/api/history/power` | Aggregated power history |
| GET | `/api/alert-rules` | List all alert rules |
| POST | `/api/alert-rules` | Create an alert rule |
| DELETE | `/api/alert-rules/{id}` | Delete an alert rule |
| PUT | `/api/alert-rules/{id}/toggle` | Toggle rule enabled/disabled |
| GET | `/api/preferences` | Get all saved preferences |
| POST | `/api/preferences` | Set a preference (key/value) |
| POST | `/api/compact` | Trigger database compaction |
| POST | `/api/query` | Execute a read-only SQL query (`{"sql": "..."}`) |
| POST | `/api/export` | Export query results to file (`{"sql": "...", "path": "...", "format": "..."}`) |
| POST | `/api/snapshot` | Create standalone DuckDB snapshot (`{"path": "...", "with_system_tables": false}`) |
| POST | `/api/archive` | Trigger Parquet archival |
| POST | `/api/unarchive` | Reload all Parquet data into DuckDB and remove archive files |
| GET | `/api/archive/status` | Archive state and directory stats |

**Using curl:**

```bash
# Get daemon status
curl --unix-socket /run/bewitch/bewitch.sock http://localhost/api/status

# Get CPU metrics
curl --unix-socket /run/bewitch/bewitch.sock http://localhost/api/metrics/cpu

# Get alerts
curl --unix-socket /run/bewitch/bewitch.sock 'http://localhost/api/alerts?ack=false'

# Create an alert rule
curl --unix-socket /run/bewitch/bewitch.sock \
  -H 'Content-Type: application/json' \
  -d '{"name":"high-cpu","type":"threshold","severity":"warning","metric":"cpu.aggregate","operator":">","value":90,"duration":"5m"}' \
  http://localhost/api/alert-rules

# Get history with time range
curl --unix-socket /run/bewitch/bewitch.sock \
  "http://localhost/api/history/cpu?start=$(date -d '1 hour ago' +%s)&end=$(date +%s)"
```

## Architecture

```
bewitchd (daemon)
├── Collectors (procfs/sysfs, parallel goroutines) → Store (DuckDB Appender API)
├── Alert Engine (threshold + predictive + variance rules → webhooks)
├── Pruner (optional, deletes old data per retention setting)
├── Compactor (optional, scheduled full DB rebuild)
├── Archiver (optional, exports old data to Parquet files)
└── API Server
    ├── Unix socket (always, plain HTTP)
    └── TCP listener (optional, TLS by default)

bewitch (TUI)
└── Daemon Client (unix socket or TCP+TLS, JSON)

bewitch repl (SQL console)
└── Daemon Client (unix socket or TCP+TLS, JSON, POST /api/query)

curl / scripts / external clients
└── JSON API (unix socket or TCP+TLS)
```

### Performance notes

- Collectors run in parallel, reducing total cycle time; collection is decoupled from DB writes via a buffered channel so slow writes never delay the next tick
- Database writes use DuckDB's Appender API for bulk inserts (bypasses SQL prepared statements)
- SMART health data is cached per physical device (configurable interval, default 5m) to avoid expensive ioctl calls on every collection cycle
- Temperature and power collectors cache sensor/zone paths (60s refresh) to avoid per-cycle glob operations
- Dimension IDs (mount names, interfaces, sensors, etc.) and process info are cached in memory
- **ETag-based change detection** — API responses include `ETag` headers (generation counters). The TUI sends `If-None-Match` on subsequent requests; when data hasn't changed, the daemon returns `304 Not Modified` and the TUI skips decoding, allocation, and chart re-rendering.
- **History cache with eviction** — history query results are cached for 5 seconds with automatic eviction of expired entries every 30 seconds, preventing unbounded memory growth in long-running daemons.

## Alert types

Alert rules are created and managed from the TUI (Alerts tab, press `n`). Rules are stored in the database and evaluated by the daemon on every cycle.

**Threshold** — fires when a metric average exceeds a value for a sustained duration. Supported metrics:
- `cpu.aggregate` — aggregate CPU usage %
- `memory.used_pct` — memory usage %
- `disk.used_pct` — disk usage % (per mount)
- `network.rx` / `network.tx` — network throughput bytes/sec (per interface)
- `temperature.sensor` — temperature in °C (per sensor)

**Predictive** — uses linear regression to predict when a metric will breach a threshold within a given timeframe:
- `disk.used_pct` — predicts disk fill within <24h, <3d, or <7d

**Variance** — fires when memory usage changes exceed a delta threshold a certain number of times within a window (thrashing/crash detection):
- `memory.variance` — counts memory usage spikes exceeding N% within a time window

## License

TBD
