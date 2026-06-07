+++
title = "Changelog"
description = "What changed, version by version."
weight = 100
+++

All notable changes to bewitch are documented here. See the full [CHANGELOG.md](https://github.com/duggan/bewitch/blob/main/CHANGELOG.md) on GitHub.

## 0.7.0

2026-06-07

### Added

- **Prometheus `/metrics` endpoint** — exposes hardware/ECC/power/SMART/GPU metrics in OpenMetrics format for scraping into an existing Prometheus/Grafana stack (honours bearer auth on TCP)
- **Shoutrrr notifications** — Discord, Telegram, ntfy, Slack, Gotify, webhook and more from a single `shoutrrr_urls` config list
- **SMART history & alerting** — SMART data is now persisted to a `smart_metrics` table and alertable via `smart.reallocated`, `smart.pending`, `smart.uncorrectable`, `smart.percent_used` (NVMe wear) and `smart.unhealthy`, with a "Disk health (SMART)" category in the alert form
- **Daemon self-metrics** — write-queue depth, dropped batches, process-info cache size, heap/RSS, goroutines and per-collector backoff state on both `GET /api/stats` and `/metrics` (`bewitch_self_*`); `bewitch stats` gains a "Daemon health" section
- **Stateful alert lifecycle** — alerts fire on the rising edge, suppress while breaching, and send a recovery/all-clear notification when they resolve, plus a **dead-man's-switch** alert that fires when metric collection stalls
- **Per-rule threshold aggregate** (avg/max/min) so a rule can catch a transient spike or a sustained floor, not just the average
- New collectors: **system load average** (1/5/15 min), **per-NIC dropped packets** (rx/tx), and **filesystem inode usage**
- "Daemon unreachable" indicator in the TUI status bar

### Changed

- **Hardened the TCP API** — multi-statement SQL is rejected, a plaintext (non-TLS) listener with no auth token is now a fatal startup error, request bodies are capped, and slow-loris timeouts are set
- `cpu.aggregate` alerts now use true utilization (`100 - idle`, including steal) rather than just user+system of core 0
- The alert form only offers hardware categories (SMART/GPU/temperature) the host actually reports, so unfireable rules can't be created
- Per-view keyboard shortcuts render in a fixed footer that stays visible as content scrolls
- A missing config file is no longer fatal — the daemon starts with defaults

### Fixed

- The four process/threshold/predictive/variance alert rules that advertised behaviour the engine didn't implement (sustained `process_down`, target force-enrichment, honest threshold wording, predictive already-breached firing) now work as described
- RAPL power counters no longer vanish on counter wrap, and duplicate power zones discovered via flat powercap symlinks are deduped
- The alert form's threshold hint is now metric-aware (previously showed a stale CPU label for SMART rules)
- The variance rule is guarded to memory-only metrics instead of silently running under another label
- Database compaction is now reader- and restart-safe
- The query API no longer swallows row scan/iteration errors
- Rune-safe truncation in TUI and REPL table rendering
- TUI viewport content is rendered once per message instead of twice

## 0.6.0

2026-06-04

### Added

- Active alerts are now surfaced in the TUI status bar
- Alert rules can be edited in place — `e` in the alerts view, or `PUT /api/alert-rules/{id}` — instead of delete-and-recreate
- Alerts view now shows a full rule detail pane, delete confirmation, and surfaced create/update errors

### Changed

- DuckDB memory is capped via a configurable `[daemon] db_memory_limit` (default `512MB`) with spill-to-disk, instead of DuckDB's ~80%-of-RAM default that could exhaust low-memory hosts
- Parquet archival now runs incrementally (per-day, resumable) and no longer pauses the daemon for the whole run
- Alert rule names must now be unique, enforced by a database index; any pre-existing duplicates are auto-renamed on upgrade

### Fixed

- Alert rules created via the API or TUI never fired — the type-specific config row was linked with `rule_id=0` (the DuckDB driver has no `LastInsertId()`), so the engine's join never matched. Rules created before this fix must be recreated.
- Deleting an alert rule now also clears its fired alerts, instead of leaving orphaned, undismissable "active" alerts
- `bewitchd` memory leak that could OOM-kill small/low-RAM hosts — the process-info cache is now bounded by an always-on eviction sweep regardless of retention settings
- Process `rss_bytes` was stored 1024× too large (≈1 TB reported for a ~1 GB process); now correct for new samples
- Enabling Parquet archival no longer freezes the daemon on large databases

## 0.5.2

2026-05-26

### Fixed

- Unbounded `process_info` cache growth on hosts with high process churn
- Background goroutines (scheduled jobs, checkpoint loop) now stop cleanly on daemon shutdown instead of running against a closing DB
- Notification sends bounded by an 8-slot semaphore
- Intel GPU reader accumulation buffer capped at 1MB

## 0.5.1

2026-04-28

### Added

- `bewitch stats` subcommand for at-a-glance system footprint
- Auto-generated API reference docs page
- Uninstall script (`uninstall.sh`) supporting both APT and tarball installs, with `KEEP_DATA=1` to preserve the database
- Documented `b` keybinding (bits/bytes toggle) on the network view

### Changed

- Installer auto-starts `bewitchd` after install — quick start reduced from 4 steps to 2
- Internal refactors to consolidate duplicated plumbing across collectors, API handlers, REPL, and TUI client

## 0.5.0

2026-04-02

### Added

- Screen capture to PNG via `x` key with configurable `[tui.capture]` settings
- `capture-views` subcommand for batch screenshot capture
- Dev docs channel with automatic publishing for pre-release versions

### Changed

- Restart bewitchd automatically on package upgrade via `dh_installsystemd`

### Fixed

- DuckDB WAL replay crash by checkpointing after migrations
- History chart flicker on tab switch via per-view chart cache
- Empty history charts when switching hardware sub-tabs
- Docs version dropdown not showing latest stable release

## 0.4.0

2026-03-30

### Added

- GPU collector with Intel iGPU (`intel_gpu_top`) and NVIDIA (`nvidia-smi`) support
- Actionable hints in GPU view when monitoring tools are missing
- Per-collector API cache push for immediate data freshness
- Load live data and hardware history immediately on startup
- Show enriched processes above the fold in process view
- Copy-to-clipboard button on docs code blocks
- E2E installation testing workflow

### Fixed

- Stale history chart shown on view switch cache miss
- Maintainer email in Debian packaging
- Dev version ordering (full timestamp instead of git SHA)

### Changed

- Enhanced installer with optional dependency prompts, dev channel, and version stamping

## 0.3.1

2026-03-16

### Fixed

- Memory history chart empty on systems without swap
- Disk NULL handling in history scan
- Removed unnecessary `-config` flag from docs site command examples

## 0.3.0

2026-03-14

### Changed

- Renamed Go module from `github.com/ross` to `github.com/duggan`
- Deduplicated schema definitions using runtime introspection
- Removed webhook, ntfy, and gotify notifiers in favour of simpler notification channels

### Fixed

- Sequence references breaking after compaction

### Added

- Local `mail` command support for email notifications (postfix/sendmail, no SMTP config needed)
- Version pulled from `VERSION` file for install script and docs

## 0.2.0

2026-03-14

### Added

- Braille charts with unified chart rendering across all views
- Hardware tab consolidating temperature, power, and ECC sub-sections
- Versioned docs for tagged releases
- Dev build pipeline for bleeding-edge apt channel

### Fixed

- Nil map panic in `updateNetSparklines`
- Archive error when metric tables have no matching rows
- Various Cloudflare Pages Functions deployment issues

## 0.1.2

2026-03-13

### Added

- Initial public release
- Metric collectors: CPU, memory, disk, network, ECC, temperature, power, process
- DuckDB storage with schema migrations
- TUI with dashboard, per-metric views, and historical charts
- Alert engine with threshold, predictive, and variance rules
- SQL REPL with dot-commands and data export
- Remote access with TLS (TOFU) and bearer token auth
- Parquet archival and data pruning
- Debian packaging with systemd service
- APT repository with signed metadata
