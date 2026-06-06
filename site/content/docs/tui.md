+++
title = "TUI Guide"
description = "Views, keybindings, time-range scrubbing, process pinning, and debug mode."
weight = 40
+++

The bewitch TUI provides 8 views for real-time system monitoring with historical charts.

```bash
bewitch

# or connect to a remote daemon
bewitch -addr myserver:9119 -token my-secret
```

## Views

Views are accessed via number keys. Tab numbering is fixed regardless of hardware availability.

| Key | View | Description |
| --- | --- | --- |
| `1` | Dashboard | Multi-column overview of all subsystems |
| `2` | CPU | Per-core usage with historical chart |
| `3` | Memory | RAM and swap breakdown with history |
| `4` | Disk | Per-mount space, I/O rates, SMART health |
| `5` | Network | Per-interface throughput with bits/bytes toggle |
| `6` | Hardware | Temperature, power (RAPL), ECC memory, and GPU sub-sections |
| `7` | Process | All processes, sortable, searchable, pinnable |
| `8` | Alerts | Alert rules and fired alerts |

## Navigation

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Cycle views forward / backward |
| `←` / `→` or `h` / `l` | Cycle views forward / backward |
| `<` / `>` | Cycle history time range (1h, 6h, 24h, 7d, 30d) |
| `q` | Quit |

## Network View

Per-interface throughput with sparklines and historical chart. Select which interfaces appear on the chart.

| Key | Action |
| --- | --- |
| `j` / `k` | Navigate interface list |
| `Space` | Toggle interface in chart |
| `a` | Select / deselect all |
| `b` | Toggle bits/bytes display |

## Hardware View

The Hardware view combines temperature sensors, power consumption (RAPL), ECC memory errors, and GPU
metrics into sub-sections. Use `Tab` / `Shift+Tab` to cycle between sub-sections.
Sections without data are dimmed but still accessible. The active sub-section is persisted across sessions.

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Cycle sub-sections (Temperature, Power, ECC, GPU) |
| `j` / `k` | Navigate sensor/zone list |
| `Space` | Toggle sensor/zone in chart |
| `a` | Select / deselect all |

## Process View

Shows all processes on the system. Non-enriched processes display `--` for cmdline and FD count.
The history chart shows top N processes by CPU over time.

| Key | Action |
| --- | --- |
| `j` / `k` | Navigate process list |
| `c` | Sort by CPU |
| `m` | Sort by memory |
| `p` | Sort by PID |
| `n` | Sort by name |
| `t` | Sort by threads |
| `f` | Sort by FDs |
| `*` | Pin/unpin selected process |
| `a` | Create alert for selected process |
| `/` | Search by name or cmdline |
| `Esc` | Clear search filter |
| `P` | Toggle pinned-only filter |
| `Tab` | Toggle history chart: Top CPU / Pinned |

## Alerts View

Two panels: a rules list on the left and fired alerts on the right. Create, toggle, and delete rules directly from the TUI.

| Key | Action |
| --- | --- |
| `j` / `k` | Navigate rules or alerts |
| `Tab` | Switch focus between rules and alerts |
| `n` | Create new alert rule |
| `d` | Delete selected rule |
| `Space` | Toggle rule enabled/disabled |
| `Enter` | Acknowledge selected alert |
| `t` | Test all notification channels |
| `Esc` | Cancel alert creation form |

## Historical Charts

CPU, memory, disk, hardware (temperature/power/GPU), and process views include a historical braille chart below the live data.
Use `<` / `>` to cycle through time ranges: 1h, 6h, 24h, 7d, 30d.

Bucket size auto-scales based on the selected range (1 minute for 1h up to 6 hours for 30d).
History data is fetched asynchronously and cached per-view for instant display on tab switch.

## Staleness Detection

The status bar monitors when fresh data last arrived for the current view. If no new data appears within
3x the longest collector interval for that view, a stale indicator shows: `stale (Xs ago)`.

## Debug Mode

```bash
bewitch -debug
```

Adds a scrollable debug console at the bottom of the TUI showing timestamped diagnostic messages:
data fetches, cache hits/misses, view transitions, errors, and pin operations.

| Key | Action |
| --- | --- |
| `{` / `}` | Scroll debug console up / down |
| `(` / `)` | Shrink / grow debug panel |

## Layout

The dashboard adapts to a multi-column grid layout on terminals wider than 120 columns.
