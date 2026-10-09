+++
title = "TUI Guide"
description = "Views, keybindings, time-range scrubbing, process pinning, and debug mode."
weight = 30

[extra]
group = "Use it"
+++

The bewitch TUI shows live metrics and history charts in eight views, plus a ninth **Services**
view when you've configured [custom sources](@/docs/custom-sources.md).

```bash
bewitch

# or connect to a remote daemon
bewitch -addr myserver:9119 -token my-secret
```

## Views

Switch views with the number keys. The numbering is fixed, whatever hardware the host has.

| Key | View | Description |
| --- | --- | --- |
| `1` | Dashboard | Overview of every subsystem |
| `2` | CPU | Per-core usage (including steal on VMs) with a history chart |
| `3` | Memory | RAM and swap breakdown with history |
| `4` | Disk | Per-mount space, I/O rates, SMART health |
| `5` | Network | Per-interface throughput with a bits/bytes toggle |
| `6` | Hardware | Temperature, power (RAPL), ECC memory, and GPU sub-sections |
| `7` | Process | All processes: sortable, searchable, pinnable |
| `8` | Alerts | Alert rules and fired alerts |
| `9` | Services | Your custom sources (only shown when at least one is configured) |

The keys available in the current view are listed in a footer at the bottom of the screen.

## Global Keys

| Key | Action |
| --- | --- |
| `1`–`9` | Jump to a view |
| `←` / `→` | Previous / next view |
| `<` / `>` (or `,` / `.`) | Shorter / longer history range |
| `r` | Pick a custom date range for the history chart |
| `PgUp` / `PgDn` | Scroll the view |
| `x` | Save a screenshot of the current view as a PNG |
| `q` / `Ctrl+C` | Quit |

## Network View

Per-interface throughput with sparklines and a history chart. Choose which interfaces appear on the chart.

| Key | Action |
| --- | --- |
| `↑` / `↓` | Move through the interface list |
| `Space` | Show / hide the interface on the chart |
| `a` | Select / deselect all |
| `b` | Toggle bits / bytes |

## Hardware View

The Hardware view has four sub-sections: Temperature, Power, ECC and GPU. Sections the host
has no data for say so. The TUI remembers the active sub-section between sessions.

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Next / previous sub-section |
| `↑` / `↓` | Move through the sensor, zone or GPU list |
| `Space` | Show / hide the item on the chart |
| `a` | Select / deselect all |

## Process View

A scrolling table of every process, a detail line for the selected process, and a history chart
of the busiest processes below. Processes outside the most active set (the top
`max_processes` plus pinned ones) show fewer details: their FDs, disk and network I/O and
command line read `--`, and the row is dimmed. Narrow terminals drop the less important columns.

| Key | Action |
| --- | --- |
| `↑` / `↓` or `j` / `k` | Move the selection |
| `PgUp` / `PgDn`, `Home` / `End` | Jump a page / to the top or bottom |
| `c` `m` `p` `n` `t` `f` | Sort by CPU, memory, PID, name, threads, FDs |
| `d` | Sort by disk I/O (read + write) |
| `w` | Sort by network I/O (receive + transmit) |
| `/` | Search by name or command line (`Enter` keeps the filter, `Esc` clears it) |
| `Esc` | Clear the search filter |
| `*` | Pin / unpin the selected process |
| `P` | Show pinned processes only |
| `Tab` | Switch the chart between top CPU and pinned processes (when you have pins) |
| `a` | Create an alert rule for the selected process |

Pinned processes are always tracked in full and are saved on the daemon, so they persist across
restarts. See [process pinning](@/docs/collectors.md#process).

## Alerts View

A rules panel with a detail pane for the selected rule, and a fired-alerts panel below it. `Tab`
switches focus between them; the footer shows the keys for the focused panel.

| Key | Panel | Action |
| --- | --- | --- |
| `Tab` | both | Switch focus between rules and fired alerts |
| `↑` / `↓` | both | Move the selection |
| `n` | both | Create a rule |
| `e` | rules | Edit the selected rule |
| `d` | rules | Delete the selected rule and its fired alerts (confirm with `y`) |
| `Space` | rules | Enable / disable the selected rule |
| `Space` | fired alerts | Select / deselect the alert |
| `a` | fired alerts | Select all / none |
| `Enter` | fired alerts | Acknowledge the selected alerts (or the one under the cursor) |
| `c` | fired alerts | Clear (delete) the selected alerts (confirm with `y`) |
| `c` | rules | Dismiss the notification test results |
| `t` | both | Send a test notification to every channel |

Any key other than `y` cancels a pending delete or clear. Clearing removes the fired alert only;
if the rule is still breaching it fires again on the next evaluation. In the rule form, `Esc`
cancels. See [Alerts](@/docs/alerts.md) for what each rule type does.

## Services View

One sub-section per custom source: a status strip, the current value of each metric, and a
history chart for the selected metric.

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Next / previous source |
| `↑` / `↓` or `j` / `k` | Choose which metric to chart |

## History Charts

The CPU, Memory, Disk, Network, Hardware, Process and Services views have a history chart. Use
`<` / `>` to step through the ranges (by default 1h, 6h, 24h, 7d, 30d; set your own with
`history_ranges` in [`[tui]`](@/docs/configuration.md#tui-settings)), or `r` to pick dates. Longer
ranges use coarser points, from 1 minute for an hour up to 6 hours for 30 days.

## Status Bar

- **`stale (Xs ago)`**: no fresh data has arrived for this view in 3× its slowest collector
  interval. Collection may be stuck.
- **`⚠ daemon unreachable (Xs)`**: the TUI can't reach the daemon (it stopped, or the network
  dropped). This shows on every view and clears once the daemon answers again.

## Debug Mode

```bash
bewitch -debug
```

Adds a scrollable console at the bottom of the screen with timestamped diagnostics: data fetches,
cache hits and misses, view switches, errors and pin operations.

| Key | Action |
| --- | --- |
| `{` / `}` | Scroll the console up / down |
| `(` / `)` | Shrink / grow the console |

## Layout

The dashboard switches to a multi-column grid on terminals wider than 120 columns.
