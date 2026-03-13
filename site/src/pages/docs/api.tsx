import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const ApiDocs: FC = () => (
  <DocsLayout title="API Reference" active="/docs/api">
    <p>
      The daemon exposes an HTTP API over its unix socket. When TCP is enabled, the same API is available
      over TLS with optional bearer token authentication. All responses are JSON.
    </p>

    <h2>Endpoints</h2>

    <h3>Status & Config</h3>
    <table>
      <thead>
        <tr><th>Method</th><th>Path</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>GET</code></td><td><code>/api/status</code></td><td>Daemon status and uptime</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/config</code></td><td>Full configuration</td></tr>
      </tbody>
    </table>

    <h3>Live Metrics</h3>
    <table>
      <thead>
        <tr><th>Method</th><th>Path</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>GET</code></td><td><code>/api/metrics/cpu</code></td><td>CPU per-core metrics</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/metrics/memory</code></td><td>Memory metrics</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/metrics/disk</code></td><td>Disk space, I/O, SMART health</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/metrics/network</code></td><td>Network per-interface metrics</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/metrics/temperature</code></td><td>Temperature sensor readings</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/metrics/power</code></td><td>Power consumption per zone</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/metrics/process</code></td><td>All processes (live snapshot)</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/metrics/dashboard</code></td><td>Combined dashboard data</td></tr>
      </tbody>
    </table>

    <h3>History</h3>
    <p>
      All history endpoints accept <code>?start=&amp;end=</code> query parameters (Unix seconds).
      Bucket size auto-scales: 1 min (1h range) to 6 hr (30d range).
    </p>
    <table>
      <thead>
        <tr><th>Method</th><th>Path</th></tr>
      </thead>
      <tbody>
        <tr><td><code>GET</code></td><td><code>/api/history/cpu</code></td></tr>
        <tr><td><code>GET</code></td><td><code>/api/history/memory</code></td></tr>
        <tr><td><code>GET</code></td><td><code>/api/history/disk</code></td></tr>
        <tr><td><code>GET</code></td><td><code>/api/history/temperature</code></td></tr>
        <tr><td><code>GET</code></td><td><code>/api/history/power</code></td></tr>
        <tr><td><code>GET</code></td><td><code>/api/history/process</code></td></tr>
      </tbody>
    </table>

    <h3>Alerts</h3>
    <table>
      <thead>
        <tr><th>Method</th><th>Path</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>GET</code></td><td><code>/api/alerts</code></td><td>List alerts (<code>?ack=false</code> for unacknowledged)</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/alerts/&#123;id&#125;/ack</code></td><td>Acknowledge an alert</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/alert-rules</code></td><td>List all rules</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/alert-rules</code></td><td>Create a rule</td></tr>
        <tr><td><code>DELETE</code></td><td><code>/api/alert-rules/&#123;id&#125;</code></td><td>Delete a rule</td></tr>
        <tr><td><code>PUT</code></td><td><code>/api/alert-rules/&#123;id&#125;/toggle</code></td><td>Toggle rule enabled/disabled</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/test-notifications</code></td><td>Test all notification channels</td></tr>
      </tbody>
    </table>

    <h3>Query & Export</h3>
    <table>
      <thead>
        <tr><th>Method</th><th>Path</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>POST</code></td><td><code>/api/query</code></td><td>Execute read-only SQL</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/export</code></td><td>Export query results to file</td></tr>
      </tbody>
    </table>

    <h3>Data Management</h3>
    <table>
      <thead>
        <tr><th>Method</th><th>Path</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>POST</code></td><td><code>/api/compact</code></td><td>Trigger database compaction</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/snapshot</code></td><td>Create standalone DuckDB snapshot</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/archive</code></td><td>Trigger Parquet archival</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/unarchive</code></td><td>Reload Parquet data into DuckDB</td></tr>
        <tr><td><code>GET</code></td><td><code>/api/archive/status</code></td><td>Archive state and directory stats</td></tr>
      </tbody>
    </table>

    <h3>Preferences</h3>
    <table>
      <thead>
        <tr><th>Method</th><th>Path</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>GET</code></td><td><code>/api/preferences</code></td><td>Get all saved preferences</td></tr>
        <tr><td><code>POST</code></td><td><code>/api/preferences</code></td><td>Set a preference (key/value)</td></tr>
      </tbody>
    </table>

    <h2>Examples</h2>

    <CodeBlock title="get daemon status">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  http://localhost/api/status`}
    </CodeBlock>

    <CodeBlock title="get CPU metrics">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  http://localhost/api/metrics/cpu`}
    </CodeBlock>

    <CodeBlock title="get history with time range">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  "http://localhost/api/history/cpu?start=$(date -d '1 hour ago' +%s)&end=$(date +%s)"`}
    </CodeBlock>

    <CodeBlock title="create alert rule">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  -H 'Content-Type: application/json' \\
  -d '{
    "name": "high-cpu",
    "type": "threshold",
    "severity": "warning",
    "metric": "cpu.aggregate",
    "operator": ">",
    "value": 90,
    "duration": "5m"
  }' \\
  http://localhost/api/alert-rules`}
    </CodeBlock>

    <CodeBlock title="execute SQL query">
{`curl --unix-socket /run/bewitch/bewitch.sock \\
  -H 'Content-Type: application/json' \\
  -d '{"sql": "SELECT COUNT(*) as n FROM cpu_metrics"}' \\
  http://localhost/api/query`}
    </CodeBlock>

    <CodeBlock title="remote access (TCP + TLS + auth)">
{`curl -k -H "Authorization: Bearer my-secret-token" \\
  https://myserver:9119/api/status`}
    </CodeBlock>

    <h2>ETag Caching</h2>
    <p>
      Metric and process endpoints include <code>ETag</code> headers (generation counters). Clients can send
      <code>If-None-Match</code> to receive <code>304 Not Modified</code> when data hasn't changed, avoiding
      unnecessary serialization and transfer.
    </p>

    <h2>Response Format</h2>
    <p>
      All responses are JSON. Arrays are wrapped in objects (e.g., <code>&#123;"cores": [...]&#125;</code> not
      bare <code>[...]</code>). Timestamps are <code>int64</code> Unix nanoseconds. Errors return
      <code>&#123;"error": "message"&#125;</code>.
    </p>
  </DocsLayout>
)
