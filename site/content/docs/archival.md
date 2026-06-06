+++
title = "Storage & Archival"
description = "Retention, compaction, Parquet archival, and standalone snapshots for the long haul."
weight = 90
+++

Bewitch stores metrics in DuckDB with optional data lifecycle management: retention pruning, compaction, and Parquet archival for long-term storage.

## DuckDB Storage

The daemon writes metrics using high-performance bulk inserts. The schema is applied automatically on startup. Database writes are decoupled from collection — the API cache is always updated immediately, so the TUI never waits on disk I/O.

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

During compaction, incoming writes are buffered in memory and flushed on completion. Pruning, compaction, and archiving are mutually exclusive (coordinated via mutex).

## Parquet Archival

For long-term storage efficiency, metrics older than `archive_threshold` can be exported to monthly Parquet files compressed with zstd (~10x smaller than DuckDB).

```toml
[daemon]
archive_threshold = "7d"
archive_interval = "6h"
archive_path = "/var/lib/bewitch/archive"
retention = "90d"  # also prunes old Parquet files
```

### How it works

1. Data older than `archive_threshold` is exported to monthly Parquet files
2. Exported data is deleted from DuckDB to save space
3. Dimension tables are snapshotted to Parquet on each archive run
4. History API queries automatically combine DuckDB and Parquet data based on the time range
5. Old Parquet files are deleted based on the `retention` setting

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
bewitch snapshot /tmp/metrics.duckdb

# Include alerts, preferences, scheduled jobs
bewitch snapshot -with-system-tables /tmp/backup.duckdb
```

Snapshots merge the live database and any archived Parquet data into a single self-contained file. Open directly with any DuckDB-compatible tool:

```bash
duckdb /tmp/metrics.duckdb "SELECT COUNT(*) FROM cpu_metrics"
```

## Concurrency

API requests are served concurrently with database writes, so the TUI stays responsive during heavy collection. During pruning or compaction, incoming writes are buffered in memory and flushed on completion.

## Schema

Schema is applied automatically on startup. Key tables:

- `cpu_metrics` — per-core CPU usage
- `memory_metrics` — memory usage
- `disk_metrics` — disk space and I/O
- `network_metrics` — network throughput
- `temperature_metrics` — sensor temperatures
- `power_metrics` — power consumption
- `gpu_metrics` — GPU utilization, frequency, power, memory
- `process_metrics` — process resource usage
- `process_info` — enriched process metadata
- `dimension_values` — normalized dimension lookups (mount, device, interface, sensor, zone)
- `alert_rules` — alert rule definitions
- `alerts` — fired alerts
- `preferences` — key-value UI preferences
- `archive_state` — archival tracking
