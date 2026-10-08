---
name: capture-screenshots
description: "Capture Website Screenshots. Capture high-resolution PNG screenshots of all bewitch TUI views for the website homepage using mock data."
---

# /capture-screenshots - Capture Website Screenshots

Capture high-resolution PNG screenshots of all bewitch TUI views for the website homepage.

## Usage

```
/capture-screenshots
```

## What This Skill Does

1. **Builds** the project (`make build`)
2. **Starts `bewitchd` in mock mode** to generate synthetic data
3. **Captures** all views (9 in mock mode, including Services) using `bewitch capture-views`
4. **Checks** the homepage `<img>` dimensions in `site/templates/index.html` still match
5. **Cleans up** the daemon

## Prerequisites

- `bewitch` and `bewitchd` must compile (`make build`)

## Mock Daemon Setup

The screenshots use `data/bewitch.toml` which has `mock = true` — the daemon generates synthetic data without real collectors. Start it on a **fresh** database and wait for seeding to finish. On first start the daemon seeds 30 days of tiered history (with daily and weekly rhythms, so long-range charts aren't flat), custom-source history for the Services tab, and demo alert rules and fired alerts. Seeding holds the maintenance lock, so a startup archive or compaction (the mock config sets `archive_threshold`) waits for it:

```bash
rm -f /tmp/bewitch.duckdb /tmp/bewitch.duckdb.wal /tmp/bewitch.sock && rm -rf /tmp/archive
./bin/bewitchd -config data/bewitch.toml &>/tmp/bewitchd.log &
# wait until the log shows "mock alerts seeded" (and "compaction complete" if archiving), then ~20s more
```

The mock config uses `/tmp/bewitch.sock` and `/tmp/bewitch.duckdb`, so it won't conflict with a real installation.

Kill the daemon after capturing: `kill $(pgrep -f 'bewitchd.*data/bewitch.toml')`

## Capture Command

```bash
./bin/bewitch -config data/bewitch.toml capture-views site/static/screenshots/
```

This captures all views (Dashboard, CPU, Memory, Disk, Network, Hardware, Process, Alerts, plus Services whenever custom sources exist; mock mode always has two) as PNG files rendered with embedded Noto Sans Mono fonts at 144 DPI (2x resolution).

### Options

- `--cols N` — terminal columns (default: 120)
- `--rows N` — terminal rows (default: 32)

Adjust cols/rows to control the aspect ratio and information density of the output images.

## View Order

Views are defined in `internal/tui/app.go` as the `view` iota (1-8):

1. Dashboard
2. CPU
3. Memory
4. Disk
5. Network
6. Hardware (temperature, power, ECC, GPU sub-sections)
7. Process
8. Alerts

Mock mode generates synthetic data for all collectors including temperature, power, and GPU, so all views have content.

## Output Files

The command writes these files to `site/static/screenshots/`:

- `dashboard.png`
- `cpu.png`
- `memory.png`
- `disk.png`
- `network.png`
- `hardware.png`
- `process.png`
- `alerts.png`
- `services.png`

## Homepage Integration

The homepage template `site/templates/index.html` and the repo `README.md` reference these images. The homepage `<img>` tags have `width` and `height` attributes that should match the pixel dimensions printed by the capture command (currently 2072x1280).

## Instructions

When invoked:

1. Build the project:
   ```bash
   make build
   ```
2. Start the mock daemon on a fresh database and wait for seeding (see "Mock Daemon Setup"):
   ```bash
   rm -f /tmp/bewitch.duckdb /tmp/bewitch.duckdb.wal /tmp/bewitch.sock && rm -rf /tmp/archive
   ./bin/bewitchd -config data/bewitch.toml &>/tmp/bewitchd.log &
   # wait for "mock alerts seeded" (and "compaction complete") in /tmp/bewitchd.log, then ~20s
   ```
3. Capture all views:
   ```bash
   ./bin/bewitch -config data/bewitch.toml capture-views site/static/screenshots/
   ```
4. Note the pixel dimensions from the output (e.g., `2072x1288 pixels`)
5. Kill the daemon:
   ```bash
   kill $(pgrep -f 'bewitchd.*data/bewitch.toml')
   ```
6. Check the `width` and `height` attributes in `site/templates/index.html`; update them if the dimensions changed
7. Review the captured PNGs by reading them (Claude can view PNG images)
8. Ask if the user wants to rebuild the site (`make site`, or `cd site && zola build`)

## Capture Settings

The capture renderer is configured via `[tui.capture]` in the TOML config:

```toml
[tui.capture]
directory = "~/screenshots"    # default save directory (not used by capture-views)
dpi = 144                      # render DPI: 72 = 1x, 144 = 2x (default), 216 = 3x
compression = "best"           # PNG compression: "default", "best", "none"
background = "#1A1A2E"         # canvas background color
foreground = "#F8F8F2"         # default text color
```

The mock config at `data/bewitch.toml` can include these settings to customize screenshot appearance.

## Notes

- The capture renders ANSI output to PNG using embedded fonts — no terminal emulator involved, so output is pixel-perfect and consistent across machines
- If cols/rows need tuning, experiment with `--cols` and `--rows` to find the best aspect ratio for the homepage slideshow
- The old VHS-based screenshot workflow (`site/screenshots.tape`) is no longer needed for screenshots but can still be used for recording demo videos
