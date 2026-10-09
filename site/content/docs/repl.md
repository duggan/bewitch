+++
title = "SQL REPL"
description = "The SQL console over your own metrics: dot-commands, multi-line editing, tab completion, and export."
weight = 50

[extra]
group = "Use it"
+++

`bewitch repl` connects to the running daemon and opens an interactive DuckDB SQL console.
It has full multi-line editing: move up and down between lines, edit earlier lines, and the input
area grows as you type.

```bash
bewitch repl

# or connect remotely
bewitch -addr myserver:9119 -token secret repl
```

## SQL Queries

SQL statements are terminated with `;`. Until a semicolon is entered, pressing Enter adds a new
line (the prompt changes to `...>`). Tab triggers context-aware completion using DuckDB's
built-in `sql_auto_complete()`.

```sql
bewitch> SELECT d.value AS mount,
    ...>   AVG(m.used_bytes * 100.0 / m.total_bytes) AS pct
    ...> FROM disk_metrics m
    ...> JOIN dimension_values d ON d.id = m.mount_id
    ...> WHERE m.ts > now() - INTERVAL '1 hour'
    ...> GROUP BY d.value;
 mount | pct
-------+------
 /     | 62.34
 /home | 41.17
(2 rows)
```

Timestamps (`ts`) are stored in UTC. The examples here compare against `now()`, which is right on
a host whose time zone is UTC. Elsewhere, use `(now() AT TIME ZONE 'UTC')` instead, or your
time windows will be shifted by the host's UTC offset.

### Restrictions

Queries run on the daemon, so the daemon enforces some limits:

- **Read-only.** Only `SELECT`, `EXPLAIN` and `PRAGMA` are allowed. DuckDB's parser decides, not
  keyword matching, so a write hidden in a CTE (`WITH … INSERT …`) is still rejected.
- **One statement at a time.** `SELECT 1; SELECT 2` is rejected.
- **No file or network access.** Functions such as `read_csv`, `read_parquet`, `read_text` and
  `glob` are blocked, so a query can't read files on the daemon host or fetch URLs. Archived data is
  still reachable through the `all_` views below.
- **30-second timeout** per query.
- **1 million rows** at most. Larger results return an error; add a `LIMIT` or aggregate.

## Key Bindings

| Key | Action |
| --- | --- |
| `Tab` | Autocomplete (SQL keywords, table names, dot-commands) |
| `Ctrl+D` | Exit |
| `Ctrl+C` | Cancel current input |
| `Ctrl+R` | Reverse search history |
| `Alt+P` / `Alt+N` | Navigate history (previous / next) |

## Dot-Commands

| Command | Description |
| --- | --- |
| `.metrics` | Metric tables with row counts and time ranges |
| `.tables` | List all tables with row counts |
| `.schema [table]` | Show column definitions (all tables, or one) |
| `.columns <table>` | Same as `.schema <table>` |
| `.count [table]` | Row counts with time ranges |
| `.dimensions` | Dimension lookup values (mounts, sensors, interfaces, zones) |
| `.export <table> <path>` | Export table to file |
| `.export (<sql>) <path>` | Export query results to file |
| `.help` | Show available commands and examples |
| `.quit` / `.exit` | Exit |

## Data Export

Export data to CSV, Parquet (zstd compressed), or JSON. Format is inferred from the file extension.

```bash
bewitch> .export cpu_metrics /var/lib/bewitch/cpu.csv
Exported 123456 rows to /var/lib/bewitch/cpu.csv

bewitch> .export (SELECT * FROM cpu_metrics
    ...> WHERE ts > now() - INTERVAL '1 hour') /var/lib/bewitch/recent.parquet
Exported 720 rows to /var/lib/bewitch/recent.parquet
```

The daemon writes the file, as the `bewitch` user on the daemon's host. The path must be absolute,
inside `[daemon] export_dir` (default: the database directory, `/var/lib/bewitch`), and must not
already exist. Set `export_dir` to somewhere you can read, or copy the file out afterwards.

## Archived Data

With [Parquet archival](@/docs/archival.md#parquet-archival) enabled, old rows move out of the
metric tables into Parquet files. Each metric table then has an `all_` view combining both:
`all_cpu_metrics`, `all_disk_metrics`, and so on, plus `all_dimension_values` and
`all_process_info`. Query the `all_` views to see your full history; `.tables` lists them. Without
archival, the views don't exist and the plain tables hold everything.

```sql
SELECT COUNT(*) FROM all_cpu_metrics WHERE ts > '2025-01-01';
```

## Dimension Tables

Metric tables use normalized dimension IDs for mount names, interfaces, sensors, and zones.
Use `.dimensions` to see the mapping, or JOIN with `dimension_values`:

```sql
SELECT d.value AS interface, n.rx_bytes_sec, n.tx_bytes_sec
FROM network_metrics n
JOIN dimension_values d ON d.category = 'interface' AND d.id = n.interface_id
WHERE n.ts > now() - INTERVAL '10 minutes';
```

## Scripting

Piped input works for non-interactive use:

```bash
echo "SELECT COUNT(*) FROM cpu_metrics;" | bewitch repl

# multi-line
cat <<'SQL' | bewitch repl
SELECT d.value AS mount, COUNT(*) as samples
FROM disk_metrics m
JOIN dimension_values d ON d.id = m.mount_id
GROUP BY d.value;
SQL
```

## History

Command history is saved to `~/.bewitch_sql_history` and persists across sessions.
