# CLAUDE.md

## Build & test

```
make build          # builds bin/bewitchd and bin/bewitch
make deb            # build Debian package (requires dpkg-buildpackage)
go build ./...      # compile check all packages
go vet ./...        # static analysis
make test           # run all unit tests (go test ./...)
make test-verbose   # run tests with verbose output
make test-integration  # run integration tests requiring DuckDB (go:build integration)
```

The project targets Linux (procfs/sysfs); it compiles and tests pass on macOS but collectors won't function. Tests are table-driven, colocated with source (`*_test.go`), and avoid filesystem/sysfs dependencies so they work cross-platform.

**Test coverage by package:**

- `internal/db` — migration runner (fresh DB, existing DB detection, dirty state, idempotent restart)
- `internal/config` — config parsing, duration handling, interval clamping, disk excludes, collector enable/disable, TLS config validation, auth config validation
- `internal/alert` — linear regression, glob-to-SQL, threshold comparison, query building
- `internal/collector` — CPU delta computation, physical device mapping, SMART parsing (NVMe + ATA)
- `internal/api` — JSON round-trips, history bucket scaling, cache keys, query source routing, read-only SQL validation via DuckDB statement types, self-signed cert generation and persistence, bearer token auth middleware and AuthTransport
- `internal/tui` — byte/bit formatting, bar rendering, status bar, time labels, process sorting, age formatting, known_hosts round-trip; teatest-based UI tests for view switching, tab cycling, history range, dynamic tab hiding, and process view interaction via `mockClient` implementing the `daemonClient` interface
- `internal/repl` — value formatting, column width computation, table rendering, byte formatting, EXPLAIN output rendering, export arg parsing

## Project structure

```
cmd/bewitchd/       — daemon entrypoint (collection loop, API, alerts, pruner, archiver)
cmd/bewitch/        — TUI entrypoint (bubbletea app)
internal/config/    — TOML config parsing
internal/db/        — DuckDB connection, schema migrations (applied on startup)
internal/collector/ — metric collectors (CPU, memory, disk, network, ECC, temperature, power, GPU, process)
internal/store/     — writes collected samples to DuckDB; archive.go handles Parquet export
internal/alert/     — alert engine (threshold, predictive, variance rules; DB-stored; email/command delivery)
internal/api/       — HTTP API over unix socket + optional TCP (status, alerts, config, history), JSON serialization
internal/tui/       — bubbletea views (dashboard, cpu, memory, disk, network, hardware, process, alerts)
internal/repl/      — interactive SQL REPL (readline-based, queries daemon via POST /api/query)
debian/             — Debian packaging (control, rules, postinst, postrm, systemd service)
```

## Key conventions

- **Collector interface**: all metric collectors implement `collector.Collector` (Name + Collect methods). Each returns a `collector.Sample` with typed data. Collectors that need configuration (e.g., exclusion patterns) accept it via constructor parameters. The process collector constructor takes `(maxProcs int, configPins []string)`.
- **Collector intervals**: each collector has a configurable `interval` field in its `[collectors.*]` TOML section (e.g., `[collectors.cpu]` `interval = "1s"`). If omitted, falls back to `[daemon]` `default_interval` (default 5s). Minimum 100ms. All eight collectors have config structs with `GetInterval(defaultInterval)` methods. The daemon uses a GCD-based tick scheduler: it computes the GCD of all collector intervals, ticks at that rate, and fires each collector when `tickCount % tickMod == 0`.
- **Collector enable/disable**: temperature and power collectors can be disabled via `[collectors.temperature]` and `[collectors.power]` config sections with `enabled = false`. Uses pointer bool (`*bool`) to distinguish "not set" (defaults to enabled) from "explicitly disabled". See `config.TemperatureCollectorConfig.IsEnabled()` and `config.PowerCollectorConfig.IsEnabled()`.
- **Collector filtering**: the disk collector filters mount points by prefix (default excludes: `/snap/`, `/run/`). Config is in `[collectors.disk]` section; see `config.DiskCollectorConfig` and `config.DefaultDiskExcludes`.
- **SMART health data**: the disk collector reads SMART data per physical device. SMART is **live-only** (not stored in DB) since it changes slowly. Data is cached per physical device with a configurable `smart_interval` (default 5m, min 30s). `physicalDevice()` strips partition suffixes (NVMe `pN`, SATA trailing digits) so multiple mounts from the same disk share one SMART read. The `DiskCollector` constructor takes `(excludeMounts []string, smartInterval time.Duration)`. SATA devices read attrs 5/197/198/1; NVMe reads `AvailSpare`, `PercentUsed`, `CritWarning` from the health log.
- **SMART data sources**: the disk collector prefers `smartctl` (smartmontools) when available, detected once at startup via `exec.LookPath`. If smartctl fails for a specific device, falls back to `anatol/smart.go` library, then direct SAT passthrough (`sat_linux.go`). Fallback is per-device, not global. smartctl is NOT a hard dependency. smartctl execution uses a 30-second timeout to avoid blocking on degraded devices. Requires `CAP_SYS_RAWIO` capability for direct device access (both smartctl and library paths).
- **Rate metrics**: disk I/O, network, power, and process collectors keep previous readings in memory and compute deltas. First sample after startup is discarded.
- **Parallel collection**: collectors due on each tick run concurrently via goroutines, reducing total cycle time. Results are gathered with `sync.WaitGroup` before API cache push. Collectors not due on a given tick produce nil samples, which `WriteBatch` skips.
- **Decoupled write pipeline**: after each collection tick, the daemon pushes metrics to the API cache immediately (so the TUI always sees fresh data), then enqueues samples to a buffered channel (capacity 8) for asynchronous DB writing. A dedicated writer goroutine drains the channel and calls `WriteBatch`. If the channel is full (writer slower than collector), the batch is dropped with a warning. On shutdown, the channel is closed and drained before the DB connection closes.
- **Collector backoff**: when a collector's `Collect()` returns an error, consecutive failures trigger exponential backoff. The collector skips `2^(n-1)` intervals (capped at 64×) before retrying. On success, the failure count resets immediately. First error is always logged; subsequent errors include attempt count and backoff duration. Recovery is logged at INFO level. Backoff fields (`consecutiveFails`, `skipUntilTick`) live on `scheduledCollector` in `cmd/bewitchd/main.go`.
- **DuckDB concurrency**: daemon uses a connection pool (`SetMaxOpenConns(4)`) to allow API handlers to execute concurrently with batch writes. TUI opens a separate read-only connection. During pruning/compaction, the store buffers incoming writes in memory and flushes them on completion.
- **Batch writes with Appenders**: `WriteBatch()` uses DuckDB's Appender API for high-performance bulk inserts. To avoid deadlock with the single connection, writes use a 3-phase pattern: (1) SQL operations (dimension/process_info inserts), (2) acquire driver connection, (3) appender writes only. Phase 3 must never call SQL methods.
- **DuckDB checkpointing**: WAL checkpoints are handled automatically by DuckDB via `wal_autocheckpoint` (configurable via `checkpoint_threshold`, default 16MB). For crash safety, `checkpoint_interval` can be set (e.g., `5m`) to force periodic checkpoints regardless of WAL size, ensuring data is flushed to the main database file.
- **Data pruning**: if `retention` is configured, the daemon periodically deletes old rows from metric tables (configurable via `prune_interval`, default 1 hour).
- **Compaction**: full database rebuild to reclaim fragmented space. Can be scheduled via `compaction_interval` or triggered manually via `POST /api/compact`. Pruning, compaction, and archiving are mutually exclusive (coordinated via mutex).
- **Parquet archival**: if `archive_threshold` and `archive_path` are configured, data older than the threshold is exported to monthly Parquet files (zstd compressed) and deleted from DuckDB. History API queries automatically union DuckDB and Parquet data based on the requested time range. Archive state is tracked in the `archive_state` table. Dimension tables (`dimension_values`, `process_info`) are snapshotted to Parquet on each archive run. Old Parquet files are pruned based on `retention`.
- **Schema migrations**: versioned SQL files in `internal/db/migrations/` (embedded via `//go:embed`), applied automatically on startup by the migration runner in `internal/db/migrate.go`. Version tracked in `schema_version` table. Go-function migrations supported for data transforms (e.g., `migrateAlertRules` in `db.go`). Existing databases without `schema_version` are auto-detected and stamped at version 1.
- **TLS for TCP connections**: when `[daemon] listen` is configured, the TCP listener uses TLS by default. On first start, the daemon auto-generates a self-signed ECDSA P-256 certificate (persisted to `tls-cert.pem`/`tls-key.pem` next to the DB file) so the fingerprint is stable across restarts. Users can provide their own cert/key via `tls_cert`/`tls_key` config fields, or disable TLS with `tls_disabled = true` for plain TCP. The daemon logs the SHA-256 fingerprint at startup. Cert generation and persistence live in `internal/api/selfsigned.go`; TLS listener wrapping is in `Server.buildTLSConfig()`. The unix socket is never TLS-wrapped.
- **TLS client TOFU (trust-on-first-use)**: the CLI performs a pre-flight TLS handshake before entering the TUI alt screen. On first connection, it displays the server cert fingerprint and prompts `Trust this server? [y/N]`. Accepted fingerprints are saved to `~/.config/bewitch/known_hosts` (one line per server: `addr fingerprint`). On subsequent connections, the client verifies the cert matches the stored fingerprint; mismatches are refused with an error. CLI flags: `-tls` (default true), `-tls-skip-verify` (bypass TOFU), `-tls-reset-fingerprint` (update a changed fingerprint). The `known_hosts` path function is a package-level variable (`knownHostsPathFn`) for test overriding. Fingerprint pinning uses `tls.Config.VerifyPeerCertificate` with `InsecureSkipVerify: true` (skip CA chain, verify fingerprint only). Code lives in `cmd/bewitch/main.go` (`resolveTLS()`) and `internal/tui/knownhosts.go`.
- **Bearer token authentication**: TCP connections can require a shared-secret bearer token via `auth_token` in the `[daemon]` config section. The server uses two `http.Server` instances sharing one mux: the unix socket server has no auth (filesystem permissions suffice), while the TCP server wraps the mux with `bearerAuth()` middleware (`internal/api/auth.go`). The middleware uses `crypto/subtle.ConstantTimeCompare` for timing-safe token comparison and returns `401 Unauthorized` on failure. Clients inject the token via `AuthTransport` (an `http.RoundTripper` wrapper in `internal/api/auth.go`) which transparently adds `Authorization: Bearer <token>` to every request. CLI flag: `-token` (or falls back to `auth_token` from config). The daemon logs a warning at startup if TCP is enabled without auth.
- **API routing**: uses Go 1.22+ `http.ServeMux` method patterns (e.g., `"GET /api/status"`). The server always listens on a unix socket; if `[daemon] listen` is configured (e.g., `listen = ":9119"`), it also listens on TCP (with TLS by default) for remote TUI/REPL connections. The unix socket and TCP listeners use separate `http.Server` instances sharing the same mux — this allows the TCP server to wrap the mux with auth middleware while the unix socket server remains unauthenticated.
- **API serialization**: JSON only. Response types are defined in `internal/api/encode.go` (e.g., `CPUResponse`, `DiskResponse`). Handlers use `writeJSON(w, status, v)` to send responses. `writeError(w, r, status, msg)` handles error responses (keeps `r` for URL path logging on 5xx). POST endpoints accept JSON bodies. Timestamps are `int64` Unix nanoseconds.
- **TUI views**: each view is a standalone render function returning a string. Views are switched via number keys (1–8) or arrow keys. CPU, memory, disk, hardware (temperature/power), and process views show a high-resolution braille chart below live data, with time range cycling via `<`/`>`. Charts use `DrawBrailleAll()` for 4x vertical resolution and dynamic height based on terminal size. All content is wrapped in bordered panels via `renderPanel()`. The alerts view has two panels: a rules list (with cursor navigation, toggle, delete) and a fired alerts table. The hardware view combines temperature, power, and ECC into sub-sections navigated with Tab/Shift+Tab; temperature and power sub-sections have per-sensor/zone toggle selection (j/k to navigate, space to toggle, a for all) controlling which items appear on the history chart; selections are persisted in the `preferences` DB table via the API. The process view has a sortable table (c/m/p/n/t/f keys), search/filter (/), process pinning (_), and shows top N processes by CPU over time in the history chart. All system processes are visible (not just top N); non-enriched processes display `--` for FDs and cmdline columns. Pinned processes show a `_` indicator; pins are persisted via the preferences API.
- **TUI render purity**: render functions (`render*View()`, `render*Chart()`) must be pure—they receive pre-cached data and return strings, never making API calls. This is critical because `View()` is called on every frame, including navigation events like scrolling. `View()` is the sole render path—it renders content from cached Model fields and sets the viewport content on every frame. Data fetching happens only in `Update()`: the `refresh*Data()` methods fetch from the API on tick events and cache results in Model fields (`m.cpuData`, `m.memData`, etc.). History charts are pre-rendered via `regenerateHistoryChart()` and cached in `m.cachedHistoryChart` to avoid expensive chart rebuilds during scrolling. Sparkline data is similarly cached in `m.tempSparkData`, `m.netSparkData`, `m.powerSparkData`.
- **TUI staleness detection**: the Model tracks `lastDataChange map[view]time.Time`, updated in each `refresh*Data()` method when fresh (non-304, non-error) data arrives. The status bar compares `time.Since(lastChange)` against `3× maxCollectorInterval` for the current view; if exceeded, it appends `" · stale (Xs ago)"`. The threshold uses the longest collector interval for multi-collector views (e.g., dashboard). Zero time (no data ever received) does not trigger the stale indicator. `buildStatusBar()` in `styles.go` accepts the `lastChange` parameter.
- **Fixed tab layout**: tab numbering is fixed (1–8) regardless of hardware availability. Temperature and power are sub-sections within the Hardware tab (key 6), which is always visible. Sub-sections without data show a "no data" message. The `Model.visibleTabs` slice is rebuilt by `updateVisibleTabs()` but always contains all 8 views. The active hardware sub-section (`hardwareSection`) is persisted via the preferences API.
- **TUI alert form**: pressing `n` on the alerts view opens a multi-step `huh` form for creating alert rules. The form dynamically adjusts fields based on selected metric category and alert type. Form state is managed via `alertFormState` struct in `internal/tui/alertform.go`.
- **TUI styling**: pink & purple color palette defined in `internal/tui/styles.go`. Key colors: hot pink `#FF6EC7` (titles, active tab), soft purple `#BB86FC` (headers, borders), magenta `#E040FB` (labels), lavender `#CF6EFF` (bar fills), deep purple `#7C4DFF` (panel borders). Tab bar at top of every view; bordered panels (`lipgloss.RoundedBorder`) wrap each section. Dashboard uses multi-column grid layout on wide terminals (>120 cols).
- **TUI debug console**: launched with `bewitch -debug`. Displays a scrollable ring buffer (100 lines) of timestamped debug messages in a bordered panel at the bottom of the screen. The `debugLog` type lives in `internal/tui/debug.go`; instrumentation calls use the `m.d(format, args...)` helper (no-op when debug is nil). Panel height is resizable (5–20 lines) and scrollable. Keys: `{`/`}` scroll up/down, `(`/`)` shrink/grow panel. Instrumented points: startup config, view switches (cache hit/miss), all refresh errors, sync/async history fetches, history range changes, date picker, process pin/unpin, window resize, and the full pinned-chart pipeline (fetch → result → tab toggle → render selection).
- **Lipgloss layout**: `panelStyle` uses `Border()` and `Padding(0, 1)`. When calling `style.Width(n)`, lipgloss sets the width **including padding but excluding border**. So for a target rendered width of `W`, use `Width(W - 2)` (subtract border only, not padding). The `renderPanel()` helper in `styles.go` handles this. For side-by-side panels, use `halfWidth * 2` for full-width elements below to ensure alignment (handles odd terminal widths).
- **History API**: `GET /api/history/{cpu,memory,disk,temperature,power,process}` endpoints accept `?start=&end=` (unix seconds) and return aggregated time-series data using DuckDB `time_bucket`. Bucket size auto-scales based on range (1min–6hr). The process history endpoint uses a CTE to identify top N processes by average CPU usage over the requested period. When archiving is enabled, queries automatically determine whether to read from DuckDB, Parquet, or both based on the archive cutoff time.
- **Archive API**: `POST /api/archive` triggers manual archival; `POST /api/unarchive` reloads all Parquet data back into DuckDB, removes the Parquet files, resets `archive_state`, and clears the archive config so all queries use DuckDB only; `GET /api/archive/status` returns the `archive_state` table contents and directory statistics (file count, total bytes). CLI commands: `bewitch archive`, `bewitch unarchive`.
- **Query API**: `POST /api/query` accepts `{"sql": "..."}` and returns `{"columns": [...], "rows": [...]}` or `{"error": "..."}`. JSON-only (no FlatBuffers). Used by `bewitch repl`. **Read-only enforcement**: the handler uses DuckDB's prepared statement `StatementType()` to verify queries are SELECT, EXPLAIN, or PRAGMA before execution — this is parser-level validation, not keyword matching, so it catches bypass attempts like `WITH cte AS (...) INSERT INTO ...`. Validation lives in `query_validate.go` using `checkReadOnly()`. The handler converts DuckDB driver types (`time.Time`, `duckdb.Decimal`, `duckdb.Interval`) to JSON-safe values via `toJSONSafe()` in `handlers.go`.
- **Export API**: `POST /api/export` accepts `{"sql": "...", "path": "...", "format": "..."}` and exports query results to a file using DuckDB's `COPY TO`. Format is inferred from file extension (`.csv`, `.parquet`, `.json`); parquet uses zstd compression. The SQL is validated read-only via `checkReadOnly()`. Returns `{"row_count": N, "path": "..."}`. Types: `ExportRequest`/`ExportResponse` in `encode.go`.
- **Snapshot API**: `POST /api/snapshot` accepts `{"path": "...", "with_system_tables": false}` and creates a standalone DuckDB file containing all metric and dimension data from both the live database and Parquet archives. Uses the same ATTACH pattern as `Compact()` in `store.go`. By default excludes daemon-internal tables (alerts, preferences, scheduled_jobs); pass `with_system_tables: true` (or CLI flag `-with-system-tables`) to include them for backup purposes. The `Store.Snapshot()` method pauses writes, ATTACHes the snapshot DB, creates schema, INSERTs from each metric table (UNION ALL with `read_parquet()` when archives exist), checkpoints, and detaches. `bewitch snapshot [-with-system-tables] <path>` is the CLI command. Types: `SnapshotRequest`/`SnapshotResponse` in `encode.go`.
- **REPL**: `bewitch repl` connects to the daemon via the unix socket (or TCP with `-addr`) and sends SQL through `POST /api/query`. Uses `github.com/chzyer/readline` for line editing and history (`~/.bewitch_sql_history`). Supports dot-commands (`.tables`, `.schema`, `.metrics`, `.dimensions`, `.export`, etc.), multi-line SQL (semicolon-terminated), and piped stdin for scripting. The `.export` command supports `<table> <path>` and `(<sql>) <path>` syntax with parenthesis-matched SQL parsing. EXPLAIN output is rendered cleanly (plan tree only, no column labels) via `formatExplain()`; other multiline results use expanded format via `formatExpanded()`. Code lives in `internal/repl/`.
- **Preferences API**: `GET /api/preferences` returns all key-value pairs; `POST /api/preferences` sets a single key-value pair. Used by the TUI to persist UI state (e.g., temperature sensor selection) in the `preferences` DB table.
- **JSON API**: all endpoints use JSON. POST endpoints accept JSON request bodies with `Content-Type: application/json`. Responses wrap arrays in objects (e.g., `{"cores": [...]}` not bare `[...]`). Handlers call `writeJSON(w, status, v)` directly. For errors: `writeError(w, r, status, msg)`. For status: `writeGenericStatus(w, status, msg)`. When adding a new endpoint, define a response type in `encode.go` and call `writeJSON()` with it.
- **Alert rules**: stored in the `alert_rules` DB table (not config file). Managed via TUI or the alert-rules API endpoints. The engine reloads rules from DB on every evaluation cycle.
- **Alert rule types**: threshold (sustained metric over/under value), predictive (linear regression extrapolation), variance (memory delta rate detection). Supported metrics: `cpu.aggregate`, `memory.used_pct`, `memory.variance`, `disk.used_pct`, `network.rx`, `network.tx`, `temperature.sensor`.
- **Alert rules API**: `GET /api/alert-rules` lists all rules; `POST /api/alert-rules` creates a rule; `DELETE /api/alert-rules/{id}` deletes; `PUT /api/alert-rules/{id}/toggle` enables/disables.
- **Alert debouncing**: the engine won't re-fire the same rule if an unacknowledged alert exists within 3x the evaluation interval.
- **Notification methods**: the `Notifier` interface in `internal/alert/notifier.go` abstracts all notification delivery. Each type implements `Name()`, `Method()`, and `Send(*Alert) NotifyResult`. The engine constructs `[]Notifier` from config at startup. `sendNotifications()` is async fire-and-forget; `SendTestNotifications()` is synchronous for the test endpoint. Supported methods: **email** (SMTP with STARTTLS/implicit TLS via `net/smtp`, or local `mail` command via `use_mail_cmd = true` for postfix/sendmail setups), **command** (arbitrary shell command with alert details as `BEWITCH_*` env vars, 10s timeout). Config: `[[alerts.email]]`, `[[alerts.commands]]`. Test endpoint: `POST /api/test-notifications`. TUI: `t` key on alerts view tests all configured notifiers.
- **GPU monitoring**: the GPU collector detects Intel iGPUs via `intel_gpu_top -J` (long-lived subprocess, JSON stream) and NVIDIA GPUs via `nvidia-smi` (point-in-time CSV). Both backends auto-detect tool availability via `exec.LookPath`. The Intel backend runs `intel_gpu_top` as a persistent subprocess with a reader goroutine caching the latest sample; the first sample is discarded (needs prior period for deltas). GPU metrics include utilization %, frequency, power, memory (NVIDIA only), and temperature. The collector has a `Stop()` method called on daemon shutdown to kill the subprocess. GPU data is stored via a separate `SetGPUSnapshot()` API method (matching `SetProcessSnapshot` pattern). The Hardware view (tab 6) has a GPU sub-section alongside temperature/power/ECC. Alert rules support `gpu.utilization`, `gpu.temperature`, and `gpu.power` metrics.
- **Sensor/zone caching**: temperature and power collectors cache discovered sensor/zone paths and refresh every 60 seconds, avoiding expensive `filepath.Glob()` calls on every collection cycle.
- **Dimension ID caching**: dimension values (mount, device, interface, sensor, zone names) are cached in memory with IDs assigned at first sight. `ensureDimensionID()` handles inserts during Phase 1; `getCachedDimensionID()` is cache-only for Phase 3.
- **Process info caching**: `procInfoCache` tracks which `(pid, start_time)` pairs have been inserted into `process_info`, skipping duplicate inserts for known processes.
- **Process collector two-phase pattern**: Phase 1 scans all `/proc/[pid]/stat` cheaply (PID, name, state, CPU%, RSS, threads). Phase 2 enriches only the top N + pinned processes with expensive data (cmdline, UID, FDs via `/proc/[pid]/status`, `/proc/[pid]/cmdline`, `/proc/[pid]/fd`). After Phase 1, the collector stores a lightweight `AllProcessSnapshot()` of all processes. The daemon pushes a merged snapshot (enriched + non-enriched) to the API server each cycle. `ProcessMetric` has an `Enriched bool` field to distinguish Phase 1 vs Phase 2 data in both the API response and TUI display.
- **Process pinning**: glob patterns (`filepath.Match`) that force specific processes into Phase 2 enrichment regardless of ranking. Dual source: config file `pinned` field (static) + TUI runtime pins stored as `pinned_processes` JSON array in the preferences DB. The collector reads runtime pins via a `SetRuntimePinsFunc()` callback that queries the preferences table each cycle.
- **Live process snapshot**: the `/api/metrics/process` endpoint serves an in-memory snapshot (all processes from the collector) rather than querying the DB. The daemon calls `apiServer.SetProcessSnapshot()` after each collection cycle. This avoids DB overhead and enables all-process visibility in the TUI. The snapshot is protected by `sync.RWMutex`.
- **Metrics caching**: `SetMetricsSnapshot()` and `SetProcessSnapshot()` store raw Go structs. `getCached*()` accessor methods are pure readers (use `RLock`), except `getCachedDashboard()` which lazily composes the dashboard from cached metric components (uses `Lock`). The dashboard is invalidated on each `SetMetricsSnapshot` call and rebuilt on next access.
- **ETag change detection**: the metrics cache has a monotonic `gen` counter (incremented on each `SetMetricsSnapshot` call); the process snapshot has a separate `procGen`. Metric and process handlers set an `ETag` response header and check `If-None-Match`, returning `304 Not Modified` when data hasn't changed. History endpoints use the quantized cache key as the ETag. On the TUI side, `DaemonClient.getJSON()` sends `If-None-Match` and returns an `ErrNotModified` sentinel on 304. All `refresh*Data()` and `fetchHistory()` methods skip data replacement, chart re-rendering, and sparkline updates when they receive `ErrNotModified`.
- **History cache eviction**: the `historyCache` map entries have a 5-second TTL. A `cleanHistoryCache()` goroutine runs every 30 seconds to evict expired entries, preventing unbounded memory growth. The goroutine is stopped via the `Server.done` channel on shutdown.

## Dependencies

- `github.com/duckdb/duckdb-go/v2` — DuckDB driver (CGO, bundles libduckdb)
- `github.com/BurntSushi/toml` — config parsing
- `github.com/prometheus/procfs` — Linux /proc and /sys reading
- `github.com/charmbracelet/bubbletea` v1 — TUI framework
- `github.com/charmbracelet/lipgloss` — TUI styling (borders, panels, tab bar, color palette)
- `github.com/charmbracelet/bubbles` — TUI components (table for alerts, viewport for scrolling)
- `github.com/charmbracelet/huh` — TUI form library (alert rule creation wizard)
- `golang.org/x/sys/unix` — statfs for disk space
- `github.com/NimbleMarkets/ntcharts` — terminal line charts for historical data views
- `github.com/anatol/smart.go` — SMART disk health reading (NVMe + SATA/SCSI)

## Configuration

Config file is TOML. See `bewitch.example.toml` for all options. Both binaries accept `-config <path>`. The TUI and all subcommands accept `-addr <host:port>` to connect to a remote daemon over TCP instead of the local unix socket. The daemon enables TCP listening via `[daemon] listen` (e.g., `listen = ":9119"`). TCP connections use TLS by default (auto-generated self-signed cert); disable with `tls_disabled = true` or provide your own cert/key via `tls_cert`/`tls_key`. Client-side TLS flags: `-tls` (default true), `-tls-skip-verify`, `-tls-reset-fingerprint`. For authentication, set `auth_token` in the daemon config; clients pass `-token <value>` or inherit the token from the shared config file.

## Adding a new collector

1. Define data types in `internal/collector/` (e.g., `type FooData struct{...}`)
2. Implement the `Collector` interface in a new file
3. Add cases to `store.writeSample()`, `store.writeSampleAppender()`, and `store.prepareSampleForAppender()` in `internal/store/store.go`
4. Add a new migration in `internal/db/migrations/` for the CREATE TABLE (see "Adding a new migration")
5. Add a config struct with `Interval` field to `internal/config/config.go` (with `GetInterval` method), add it to `CollectorsConfig`
6. Register the collector in the `scheduled` slice in `cmd/bewitchd/main.go`
7. Optionally add a TUI view in `internal/tui/`

## Adding a new alert rule type

1. Implement the `Rule` interface in `internal/alert/rules.go`
2. Add a case to the switch in `engine.ReloadRules()`
3. Add the metric to the appropriate `buildQuery()` method
4. Update the TUI form in `internal/tui/alertform.go` to expose the new type

## Adding a new migration

1. Create `internal/db/migrations/000NNN_description.up.sql` with the next sequential version number
2. For Go-logic migrations (data transforms), add an entry to `goMigrations()` in `internal/db/migrate.go`
3. Version numbers must be sequential with no gaps
4. SQL files can contain multiple statements (DuckDB executes them in one call)
5. Test with both fresh databases and existing databases (`make test`)

## Debian packaging

The `debian/` directory contains full packaging for Debian/Ubuntu:

- `make deb` builds the package (outputs `../bewitch_<version>_<arch>.deb`)
- Package creates `bewitch` system user/group on install
- Installs binaries to `/usr/local/bin/`, config to `/etc/bewitch.toml`
- systemd service runs as `bewitch` user with `RuntimeDirectory=bewitch` (creates `/run/bewitch/`), `SupplementaryGroups=disk` for block device access, and `AmbientCapabilities=CAP_SYS_RAWIO` for SMART device access
- Default socket path is `/run/bewitch/bewitch.sock` (world-accessible, 0666)
- Data stored in `/var/lib/bewitch/` (owned by `bewitch:bewitch`)
- Package recommends `smartmontools` for enhanced SMART disk health coverage via smartctl

To release a new version:

1. Update `CHANGELOG.md` and `site/src/pages/docs/changelog.tsx` (use `/changelog` skill)
2. Update `debian/changelog` with new version and changes
3. Run `make deb` on a Debian/Ubuntu system
4. Test with `sudo dpkg -i ../bewitch_<version>_<arch>.deb`
5. Add the new version to the docs site version dropdown in `site/src/versions.ts`

## Documentation

After completing significant changes, consider updating:

- **CLAUDE.md** — architecture, key patterns, build notes, hardware details
- **README.md** — user-facing setup/usage instructions
- **MEMORY.md** — non-obvious lessons learned, debugging insights for future sessions
