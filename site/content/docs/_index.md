+++
title = "Documentation"
description = "How to install, configure, and get at your data — locally or over TLS."
sort_by = "weight"
template = "section.html"
page_template = "page.html"
+++

bewitch is two programs. **`bewitchd`** is a daemon that reads `/proc` and `/sys` on a
schedule, writes what it finds to a local DuckDB file, evaluates your alert rules, and serves
it all over an HTTP API. **`bewitch`** is the client: a terminal UI, a SQL console
(`bewitch repl`), and a handful of maintenance commands.

New here? Start with [Installation](@/docs/installation.md), then open the TUI with
`bewitch`. The defaults are sensible enough that you may never need
[Configuration](@/docs/configuration.md).

```
bewitchd ── collectors ──▶ DuckDB ──▶ alert engine ──▶ email / chat / command
    │                         │
    └── HTTP API ◀────────────┘
          ├── unix socket   (always)       ◀── bewitch, bewitch repl
          └── TCP + TLS     (optional)     ◀── remote clients, Prometheus
```
