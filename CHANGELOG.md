# Changelog

All notable changes to bewitch are documented here.

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
