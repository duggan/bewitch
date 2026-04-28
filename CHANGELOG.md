# Changelog

All notable changes to bewitch are documented here.

## [0.5.1] - 2026-04-28

### Added

- `bewitch stats` subcommand for at-a-glance system footprint
- Auto-generated API reference docs page
- Uninstall script (`uninstall.sh`) supporting both APT and tarball installs, with `KEEP_DATA=1` to preserve the database
- Documented `b` keybinding (bits/bytes toggle) on the network view

### Changed

- Installer auto-starts `bewitchd` after install — quick start reduced from 4 steps to 2
- Internal refactors to consolidate duplicated plumbing across collectors, API handlers, REPL, and TUI client

## [0.5.0] - 2026-04-02

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

## [0.4.0] - 2026-03-30

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

## [0.3.1] - 2026-03-16

### Fixed

- Memory history chart empty on systems without swap
- Disk NULL handling in history scan
- Removed unnecessary `-config` flag from docs site command examples

## [0.3.0] - 2026-03-14

### Changed

- Renamed Go module from `github.com/ross` to `github.com/duggan`
- Deduplicated schema definitions using runtime introspection
- Removed webhook, ntfy, and gotify notifiers in favour of simpler notification channels

### Fixed

- Sequence references breaking after compaction

### Added

- Local `mail` command support for email notifications (postfix/sendmail, no SMTP config needed)
- Version pulled from `VERSION` file for install script and docs

## [0.2.0] - 2026-03-14

### Added

- Braille charts with unified chart rendering across all views
- Hardware tab consolidating temperature, power, and ECC sub-sections
- Versioned docs for tagged releases
- Dev build pipeline for bleeding-edge apt channel

### Fixed

- Nil map panic in `updateNetSparklines`
- Archive error when metric tables have no matching rows
- Various Cloudflare Pages Functions deployment issues

## [0.1.2] - 2026-03-13

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
