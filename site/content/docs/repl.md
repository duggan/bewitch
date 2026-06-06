+++
title = "SQL REPL"
description = "The SQL console over your own metrics: dot-commands, multi-line editing, tab completion, and export."
weight = 60
+++

`bewitch repl` connects to the running daemon and opens an interactive DuckDB SQL console.
The REPL uses readline for line editing with full multi-line support — arrow up/down between lines,
edit earlier lines, and the input area auto-resizes.

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

Only **read-only** queries are allowed — SELECT, EXPLAIN, and PRAGMA. Write/DDL statements are
rejected server-side using DuckDB's statement parser (not keyword matching), so bypass attempts like
`WITH cte AS (...) INSERT INTO ...` are caught.

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
| `.schema [table]` | Show column definitions |
| `.count [table]` | Row counts with time ranges |
| `.dimensions` | Dimension lookup values (mounts, sensors, interfaces, zones) |
| `.export <table> <path>` | Export table to file |
| `.export (<sql>) <path>` | Export query results to file |
| `.help` | Show available commands and examples |
| `.quit` | Exit |

## Data Export

Export data to CSV, Parquet (zstd compressed), or JSON. Format is inferred from the file extension.

```bash
bewitch> .export all_cpu_metrics /tmp/cpu.csv
Exported 123456 rows to /tmp/cpu.csv

bewitch> .export (SELECT * FROM all_cpu_metrics
    ...> WHERE ts > now() - INTERVAL '1 hour') /tmp/recent.parquet
Exported 720 rows to /tmp/recent.parquet
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
