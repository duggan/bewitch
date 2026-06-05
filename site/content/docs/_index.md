+++
title = "Documentation"
description = "How to install, configure, and get at your data — locally or over TLS."
sort_by = "weight"
template = "section.html"
page_template = "page.html"
+++

bewitch is a monitoring daemon (`bewitchd`) and a TUI (`bewitch`) for Linux. It reads
`/proc` and `/sys`, stores what it finds in DuckDB, and gives you charts, alerts, remote
access, and a SQL console over the lot. These pages cover installing it, configuring it,
and getting at your data — locally or over TLS.

## Architecture

```
bewitchd (daemon)
├── Collectors (procfs/sysfs, parallel goroutines) → Store (DuckDB)
├── Alert Engine (threshold + predictive + variance → notifications)
├── Pruner / Compactor / Archiver
└── API Server
    ├── Unix socket (always, plain HTTP)
    └── TCP listener (optional, TLS by default)

bewitch (TUI)
└── Daemon Client (unix socket or TCP+TLS)

bewitch repl (SQL console)
└── Daemon Client (POST /api/query)
```
