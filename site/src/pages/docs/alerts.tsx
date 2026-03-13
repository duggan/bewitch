import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const AlertsDocs: FC = () => (
  <DocsLayout title="Alerts" active="/docs/alerts">
    <p>
      Alert rules are created and managed from the TUI (Alerts view, press <code>n</code>). Rules are stored
      in the database and evaluated by the daemon on every evaluation cycle.
    </p>

    <h2>Alert Types</h2>

    <h3>Threshold</h3>
    <p>
      Fires when a metric average exceeds a value for a sustained duration.
    </p>
    <table>
      <thead>
        <tr><th>Metric</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>cpu.aggregate</code></td><td>Aggregate CPU usage %</td></tr>
        <tr><td><code>memory.used_pct</code></td><td>Memory usage %</td></tr>
        <tr><td><code>disk.used_pct</code></td><td>Disk usage % (per mount)</td></tr>
        <tr><td><code>network.rx</code></td><td>Network receive bytes/sec (per interface)</td></tr>
        <tr><td><code>network.tx</code></td><td>Network transmit bytes/sec (per interface)</td></tr>
        <tr><td><code>temperature.sensor</code></td><td>Temperature in &deg;C (per sensor)</td></tr>
      </tbody>
    </table>

    <h3>Predictive</h3>
    <p>
      Uses linear regression to predict when a metric will breach a threshold within a given timeframe.
    </p>
    <ul>
      <li><code>disk.used_pct</code> — predicts disk fill within 24h, 3d, or 7d</li>
    </ul>

    <h3>Variance</h3>
    <p>
      Fires when memory usage changes exceed a delta threshold a certain number of times within a window.
      Useful for detecting memory thrashing or crash loops.
    </p>
    <ul>
      <li><code>memory.variance</code> — counts memory usage spikes exceeding N% within a time window</li>
    </ul>

    <h2>Debouncing</h2>
    <p>
      The alert engine won't re-fire the same rule if an unacknowledged alert exists within 3x the evaluation interval.
      This prevents alert storms for persistent conditions.
    </p>

    <h2>Notification Channels</h2>
    <p>
      Alerts can be delivered to any combination of five notification methods. All are configured in the
      TOML config file under <code>[alerts]</code>.
    </p>

    <h3>Webhook</h3>
    <p>HTTP POST with JSON payload. Supports custom headers.</p>
    <CodeBlock title="bewitch.toml">
{`[[alerts.webhooks]]
url = "https://hooks.example.com/alert"
# headers = { "X-Auth" = "secret" }`}
    </CodeBlock>

    <h3>ntfy</h3>
    <p>Push notifications via <a href="https://ntfy.sh">ntfy.sh</a> or a self-hosted server. Alert severity maps to ntfy priority.</p>
    <CodeBlock title="bewitch.toml">
{`[[alerts.ntfy]]
url = "https://ntfy.sh"
topic = "bewitch-alerts"
# token = ""  # optional auth`}
    </CodeBlock>

    <h3>Email (SMTP)</h3>
    <p>Send email alerts via SMTP with STARTTLS or implicit TLS.</p>
    <CodeBlock title="bewitch.toml">
{`[[alerts.email]]
smtp_host = "smtp.example.com"
smtp_port = 587
username = "alerts@example.com"
password = "app-password"
from = "alerts@example.com"
to = ["admin@example.com", "ops@example.com"]
starttls = true  # false for implicit TLS (port 465)`}
    </CodeBlock>

    <h3>Gotify</h3>
    <p>Push notifications to a self-hosted <a href="https://gotify.net">Gotify</a> server.</p>
    <CodeBlock title="bewitch.toml">
{`[[alerts.gotify]]
url = "https://gotify.example.com"
token = "AxxxxxxxxxxxxxxR"  # application token
priority = 0  # 0 = auto-map from severity (warning=5, critical=8)`}
    </CodeBlock>

    <h3>Command</h3>
    <p>Execute an arbitrary shell command with alert details as environment variables.</p>
    <CodeBlock title="bewitch.toml">
{`[[alerts.commands]]
cmd = "/usr/local/bin/alert-handler"`}
    </CodeBlock>
    <p>Available environment variables:</p>
    <table>
      <thead>
        <tr><th>Variable</th><th>Content</th></tr>
      </thead>
      <tbody>
        <tr><td><code>BEWITCH_RULE</code></td><td>Rule name</td></tr>
        <tr><td><code>BEWITCH_SEVERITY</code></td><td>warning or critical</td></tr>
        <tr><td><code>BEWITCH_MESSAGE</code></td><td>Alert message</td></tr>
        <tr><td><code>BEWITCH_TIMESTAMP</code></td><td>ISO 8601 timestamp</td></tr>
      </tbody>
    </table>
    <p>Commands run with a 10-second timeout.</p>

    <h2>Testing Notifications</h2>
    <p>
      Press <code>t</code> on the Alerts view to send a test notification through all configured channels.
      This triggers the <code>POST /api/test-notifications</code> endpoint which sends synchronously (blocks until all channels respond).
    </p>

    <h2>Managing Rules via API</h2>
    <p>
      Rules can also be managed programmatically. See the <a href="/docs/api">API Reference</a> for the
      alert-rules endpoints.
    </p>
  </DocsLayout>
)
