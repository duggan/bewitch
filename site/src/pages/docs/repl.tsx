import type { FC } from 'hono/jsx'
import { docsBase } from '../../docs-base'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const ReplDocs: FC = () => (
  <DocsLayout title="SQL REPL" active={`${docsBase}/repl`}>
    <p>
      <code>bewitch repl</code> connects to the running daemon and opens an interactive DuckDB SQL console.
      The REPL uses readline for line editing with full multi-line support — arrow up/down between lines,
      edit earlier lines, and the input area auto-resizes.
    </p>

    <CodeBlock title="launch">
{`bewitch repl

# or connect remotely
bewitch -addr myserver:9119 -token secret repl`}
    </CodeBlock>

    <h2>SQL Queries</h2>
    <p>
      SQL statements are terminated with <code>;</code>. Until a semicolon is entered, pressing Enter adds a new
      line (the prompt changes to <code>...&gt;</code>). Tab triggers context-aware completion using DuckDB's
      built-in <code>sql_auto_complete()</code>.
    </p>

    <CodeBlock title="example query">
{`bewitch> SELECT d.value AS mount,
    ...>   AVG(m.used_bytes * 100.0 / m.total_bytes) AS pct
    ...> FROM disk_metrics m
    ...> JOIN dimension_values d ON d.id = m.mount_id
    ...> WHERE m.ts > now() - INTERVAL '1 hour'
    ...> GROUP BY d.value;
 mount | pct
-------+------
 /     | 62.34
 /home | 41.17
(2 rows)`}
    </CodeBlock>

    <p>
      Only <strong>read-only</strong> queries are allowed — SELECT, EXPLAIN, and PRAGMA. Write/DDL statements are
      rejected server-side using DuckDB's statement parser (not keyword matching), so bypass attempts like
      <code>WITH cte AS (...) INSERT INTO ...</code> are caught.
    </p>

    <h2>Key Bindings</h2>
    <table>
      <thead>
        <tr><th>Key</th><th>Action</th></tr>
      </thead>
      <tbody>
        <tr><td><code>Tab</code></td><td>Autocomplete (SQL keywords, table names, dot-commands)</td></tr>
        <tr><td><code>Ctrl+D</code></td><td>Exit</td></tr>
        <tr><td><code>Ctrl+C</code></td><td>Cancel current input</td></tr>
        <tr><td><code>Ctrl+R</code></td><td>Reverse search history</td></tr>
        <tr><td><code>Alt+P</code> / <code>Alt+N</code></td><td>Navigate history (previous / next)</td></tr>
      </tbody>
    </table>

    <h2>Dot-Commands</h2>
    <table>
      <thead>
        <tr><th>Command</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>.metrics</code></td><td>Metric tables with row counts and time ranges</td></tr>
        <tr><td><code>.tables</code></td><td>List all tables with row counts</td></tr>
        <tr><td><code>.schema [table]</code></td><td>Show column definitions</td></tr>
        <tr><td><code>.count [table]</code></td><td>Row counts with time ranges</td></tr>
        <tr><td><code>.dimensions</code></td><td>Dimension lookup values (mounts, sensors, interfaces, zones)</td></tr>
        <tr><td><code>.export &lt;table&gt; &lt;path&gt;</code></td><td>Export table to file</td></tr>
        <tr><td><code>.export (&lt;sql&gt;) &lt;path&gt;</code></td><td>Export query results to file</td></tr>
        <tr><td><code>.help</code></td><td>Show available commands and examples</td></tr>
        <tr><td><code>.quit</code></td><td>Exit</td></tr>
      </tbody>
    </table>

    <h2>Data Export</h2>
    <p>
      Export data to CSV, Parquet (zstd compressed), or JSON. Format is inferred from the file extension.
    </p>

    <CodeBlock title="export examples">
{`bewitch> .export all_cpu_metrics /tmp/cpu.csv
Exported 123456 rows to /tmp/cpu.csv

bewitch> .export (SELECT * FROM all_cpu_metrics
    ...> WHERE ts > now() - INTERVAL '1 hour') /tmp/recent.parquet
Exported 720 rows to /tmp/recent.parquet`}
    </CodeBlock>

    <h2>Dimension Tables</h2>
    <p>
      Metric tables use normalized dimension IDs for mount names, interfaces, sensors, and zones.
      Use <code>.dimensions</code> to see the mapping, or JOIN with <code>dimension_values</code>:
    </p>

    <CodeBlock title="dimension join">
{`SELECT d.value AS interface, n.rx_bytes_sec, n.tx_bytes_sec
FROM network_metrics n
JOIN dimension_values d ON d.category = 'interface' AND d.id = n.interface_id
WHERE n.ts > now() - INTERVAL '10 minutes';`}
    </CodeBlock>

    <h2>Scripting</h2>
    <p>Piped input works for non-interactive use:</p>
    <CodeBlock>
{`echo "SELECT COUNT(*) FROM cpu_metrics;" | bewitch repl

# multi-line
cat <<'SQL' | bewitch repl
SELECT d.value AS mount, COUNT(*) as samples
FROM disk_metrics m
JOIN dimension_values d ON d.id = m.mount_id
GROUP BY d.value;
SQL`}
    </CodeBlock>

    <h2>History</h2>
    <p>
      Command history is saved to <code>~/.bewitch_sql_history</code> and persists across sessions.
    </p>
  </DocsLayout>
)
