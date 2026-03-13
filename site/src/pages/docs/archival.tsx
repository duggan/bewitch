import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const ArchivalDocs: FC = () => (
  <DocsLayout title="Storage & Archival" active="/docs/archival">
    <p>
      Bewitch stores metrics in DuckDB with optional data lifecycle management: retention pruning, compaction,
      and Parquet archival for long-term storage.
    </p>

    <h2>DuckDB Storage</h2>
    <p>
      The daemon writes metrics using DuckDB's Appender API for high-performance bulk inserts.
      The schema is applied automatically on startup (CREATE IF NOT EXISTS). Database writes are decoupled
      from collection via a buffered channel — the API cache is always updated immediately.
    </p>

    <h3>WAL checkpointing</h3>
    <p>
      DuckDB uses a write-ahead log (WAL) for crash safety. Checkpoints are handled automatically when the WAL
      exceeds <code>checkpoint_threshold</code> (default 16MB). For additional crash safety, set
      <code>checkpoint_interval</code> to force periodic checkpoints:
    </p>
    <CodeBlock title="bewitch.toml">
{`[daemon]
checkpoint_threshold = "16MB"  # auto-checkpoint WAL size
checkpoint_interval = "5m"     # forced periodic checkpoint`}
    </CodeBlock>

    <h2>Retention Pruning</h2>
    <p>
      When <code>retention</code> is configured, the daemon periodically deletes metrics older than the specified duration.
    </p>
    <CodeBlock title="bewitch.toml">
{`[daemon]
retention = "30d"         # delete data older than 30 days
prune_interval = "1h"     # run pruning every hour`}
    </CodeBlock>

    <h2>Compaction</h2>
    <p>
      Compaction performs a full database rebuild to reclaim fragmented space. It can run on a schedule or be triggered manually.
    </p>
    <CodeBlock title="bewitch.toml">
{`[daemon]
compaction_interval = "7d"  # weekly compaction`}
    </CodeBlock>
    <CodeBlock title="manual compaction">
{`bewitch -config /etc/bewitch.toml compact

# or remotely
bewitch -addr myserver:9119 -token secret compact`}
    </CodeBlock>
    <p>
      During compaction, incoming writes are buffered in memory and flushed on completion.
      Pruning, compaction, and archiving are mutually exclusive (coordinated via mutex).
    </p>

    <h2>Parquet Archival</h2>
    <p>
      For long-term storage efficiency, metrics older than <code>archive_threshold</code> can be exported to
      monthly Parquet files compressed with zstd (~10x smaller than DuckDB).
    </p>
    <CodeBlock title="bewitch.toml">
{`[daemon]
archive_threshold = "7d"
archive_interval = "6h"
archive_path = "/var/lib/bewitch/archive"
retention = "90d"  # also prunes old Parquet files`}
    </CodeBlock>

    <h3>How it works</h3>
    <ol>
      <li>Data older than <code>archive_threshold</code> is exported to monthly Parquet files</li>
      <li>Exported data is deleted from DuckDB to save space</li>
      <li>Dimension tables are snapshotted to Parquet on each archive run</li>
      <li>History API queries automatically combine DuckDB and Parquet data based on the time range</li>
      <li>Old Parquet files are deleted based on the <code>retention</code> setting</li>
    </ol>

    <h3>Manual archive/unarchive</h3>
    <CodeBlock>
{`# Archive old data to Parquet
bewitch -config /etc/bewitch.toml archive

# Reload all Parquet data back into DuckDB
bewitch -config /etc/bewitch.toml unarchive`}
    </CodeBlock>
    <p>
      <code>unarchive</code> reloads all Parquet data into DuckDB, removes the Parquet files, and resets the
      archive state. Useful for changing strategies or disabling archival.
    </p>

    <h2>Snapshots</h2>
    <p>
      Create standalone DuckDB files for offline analysis — complex queries, sharing with colleagues,
      or use with DBeaver, Jupyter, or the DuckDB CLI.
    </p>
    <CodeBlock>
{`# Metrics + dimensions only (default)
bewitch -config /etc/bewitch.toml snapshot /tmp/metrics.duckdb

# Include alerts, preferences, scheduled jobs
bewitch snapshot -with-system-tables /tmp/backup.duckdb`}
    </CodeBlock>
    <p>
      Snapshots merge the live database and any archived Parquet data into a single self-contained file.
      Open directly with any DuckDB-compatible tool:
    </p>
    <CodeBlock>
{`duckdb /tmp/metrics.duckdb "SELECT COUNT(*) FROM cpu_metrics"`}
    </CodeBlock>

    <h2>Concurrency</h2>
    <p>
      The daemon uses a DuckDB connection pool (<code>MaxOpenConns(4)</code>) to allow API handlers to execute
      concurrently with batch writes. The TUI opens a separate read-only connection. During pruning/compaction,
      the store buffers incoming writes in memory and flushes them on completion.
    </p>

    <h2>Schema</h2>
    <p>
      Tables are defined as a const string in the codebase and applied with CREATE IF NOT EXISTS on startup.
      Key tables:
    </p>
    <ul>
      <li><code>cpu_metrics</code> — per-core CPU usage</li>
      <li><code>memory_metrics</code> — memory usage</li>
      <li><code>disk_metrics</code> — disk space and I/O</li>
      <li><code>network_metrics</code> — network throughput</li>
      <li><code>temperature_metrics</code> — sensor temperatures</li>
      <li><code>power_metrics</code> — power consumption</li>
      <li><code>process_metrics</code> — process resource usage</li>
      <li><code>process_info</code> — enriched process metadata</li>
      <li><code>dimension_values</code> — normalized dimension lookups (mount, device, interface, sensor, zone)</li>
      <li><code>alert_rules</code> — alert rule definitions</li>
      <li><code>alerts</code> — fired alerts</li>
      <li><code>preferences</code> — key-value UI preferences</li>
      <li><code>archive_state</code> — archival tracking</li>
    </ul>
  </DocsLayout>
)
