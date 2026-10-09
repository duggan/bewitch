+++
title = "Storage & Archival"
description = "Retention, compaction, Parquet archival, and standalone snapshots for the long haul."
weight = 90

[extra]
group = "Reference"
+++

Bewitch stores metrics in DuckDB with optional data lifecycle management: retention pruning, compaction, and Parquet archival for long-term storage.

## DuckDB Storage

The schema is created and migrated automatically on startup. Writes happen in the background, so a slow disk doesn't delay the live views.

### WAL checkpointing

DuckDB uses a write-ahead log (WAL) for crash safety. Checkpoints are handled automatically when the WAL exceeds `checkpoint_threshold` (default 16MB). For additional crash safety, set `checkpoint_interval` to force periodic checkpoints:

```toml
[daemon]
checkpoint_threshold = "16MB"  # auto-checkpoint WAL size
checkpoint_interval = "5m"     # forced periodic checkpoint
```

## Retention Pruning

When `retention` is configured, the daemon periodically deletes metrics older than the specified duration.

```toml
[daemon]
retention = "30d"         # delete data older than 30 days
prune_interval = "1h"     # run pruning every hour
```

## Compaction

Compaction performs a full database rebuild to reclaim fragmented space. It can run on a schedule or be triggered manually.

```toml
[daemon]
compaction_interval = "7d"  # weekly compaction
```

```bash
bewitch compact

# or remotely
bewitch -addr myserver:9119 -token secret compact
```

During compaction, incoming writes are buffered in memory and written once it finishes, so no samples are lost. Pruning, compaction and archiving never run at the same time; one waits for the other. Over the API, a compaction or archive request is refused while another is running or within 30 seconds of the last one.

## Parquet Archival

For long-term storage efficiency, metrics older than `archive_threshold` can be exported to daily Parquet files compressed with zstd (~10x smaller than DuckDB).

```toml
[daemon]
archive_threshold = "7d"
archive_interval = "6h"
archive_path = "/var/lib/bewitch/archive"
retention = "90d"  # also prunes old Parquet files
```

### How it works

1. Data older than `archive_threshold` is exported one day at a time to
   `<archive_path>/<table>/YYYY-MM-DD.parquet`
2. Each exported day is deleted from DuckDB to save space
3. The dimension tables (`dimension_values`, `process_info`) are snapshotted to Parquet on each run
4. History charts and the API combine DuckDB and Parquet data automatically, based on the time range
5. Old Parquet files are deleted based on the `retention` setting

Collection keeps running while an archive runs. Work is committed day by day, so an interrupted
run picks up where it left off.

### Querying archived data

When archival is configured, each metric table gets an `all_` view that combines the live table
with its Parquet files: `all_cpu_metrics`, `all_disk_metrics`, …, plus `all_dimension_values` and
`all_process_info`. Use these in the [SQL REPL](@/docs/repl.md) to query your full history. Files
written before a schema change still work: columns they predate read as `NULL`.

### Manual archive/unarchive

```bash
# Archive old data to Parquet
bewitch archive

# Reload all Parquet data back into DuckDB
bewitch unarchive
```

`unarchive` reloads all Parquet data into DuckDB, removes the Parquet files, and resets the archive state. Useful for changing strategies or disabling archival.

## Snapshots

Create standalone DuckDB files for offline analysis — complex queries, sharing with colleagues, or use with DBeaver, Jupyter, or the DuckDB CLI.

```bash
# Metrics + dimensions only (default)
bewitch snapshot /var/lib/bewitch/metrics.duckdb

# Include alerts, alert rules, preferences
bewitch snapshot -with-system-tables /var/lib/bewitch/backup.duckdb
```

The daemon writes the file, so the path must be absolute, inside `export_dir` (default: the
directory holding the database, `/var/lib/bewitch`), and must not already exist. Copy it elsewhere
afterwards.

Snapshots merge the live database and any archived Parquet data into a single self-contained file. Open directly with any DuckDB-compatible tool:

```bash
duckdb /var/lib/bewitch/metrics.duckdb "SELECT COUNT(*) FROM cpu_metrics"
```

## Concurrency

API requests are served concurrently with database writes, so the TUI stays responsive during heavy collection. During pruning or compaction, incoming writes are buffered in memory and written when it finishes.

## Schema

Schema is applied automatically on startup. Key tables:

Metric tables (each has a `ts` column):

- `cpu_metrics` — per-core and overall CPU usage, including steal
- `memory_metrics` — memory and swap usage
- `load_metrics` — 1/5/15-minute load averages
- `disk_metrics` — disk space, inodes and I/O
- `network_metrics` — network throughput, packets, errors and drops
- `ecc_metrics` — ECC memory error counts
- `temperature_metrics` — sensor temperatures
- `power_metrics` — power consumption
- `gpu_metrics` — GPU utilization, frequency, power, memory
- `smart_metrics` — SMART health snapshots per disk
- `process_metrics` — resource usage of the fully tracked processes
- `custom_metrics` — numbers from [custom sources](@/docs/custom-sources.md)

Lookup tables:

- `dimension_values` — names behind the IDs in metric tables (mounts, devices, interfaces, sensors, zones, GPUs)
- `process_info` — process name, command line and user, keyed by PID and start time

System tables:

- `alert_rules` and `alert_rule_*` — alert rule definitions
- `alerts` — fired alerts
- `preferences` — saved TUI state (selections, pins)
- `archive_state` — archival progress
- `schema_version` — applied migrations
