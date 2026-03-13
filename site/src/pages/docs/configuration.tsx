import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const ConfigurationDocs: FC = () => (
  <DocsLayout title="Configuration" active="/docs/configuration">
    <p>
      Bewitch uses a TOML configuration file. Both <code>bewitchd</code> and <code>bewitch</code> accept <code>-config &lt;path&gt;</code>.
      The default location when installed via the Debian package is <code>/etc/bewitch.toml</code>.
    </p>

    <h2>Daemon Settings</h2>
    <CodeBlock title="bewitch.toml — [daemon]">
{`[daemon]
# socket = "/run/bewitch/bewitch.sock"  # Unix socket path
# listen = ":9119"          # TCP listener for remote access (empty = disabled)
db_path = "/var/lib/bewitch/bewitch.duckdb"
# log_level = "info"        # debug, info, warn, error
# default_interval = "5s"   # global fallback collection interval (min 100ms)
# mock = false              # synthetic data for macOS TUI development`}
    </CodeBlock>

    <h3>Data management</h3>
    <CodeBlock title="bewitch.toml — data lifecycle">
{`[daemon]
# retention = "30d"           # delete metrics older than this (empty = keep forever)
# prune_interval = "1h"       # how often to run pruning (requires retention)
# compaction_interval = "7d"  # full DB rebuild interval (empty = manual only)
# checkpoint_threshold = "16MB"  # DuckDB WAL auto-checkpoint size
# checkpoint_interval = "5m"    # forced checkpoint for crash safety (empty = disabled)`}
    </CodeBlock>

    <h3>Parquet archival</h3>
    <CodeBlock title="bewitch.toml — archival">
{`[daemon]
# archive_threshold = "7d"   # archive data older than this to Parquet
# archive_interval = "6h"    # how often to run archive
# archive_path = "/var/lib/bewitch/archive"  # Parquet output directory`}
    </CodeBlock>

    <h3>TLS and authentication</h3>
    <CodeBlock title="bewitch.toml — TLS">
{`[daemon]
# listen = ":9119"           # must be set to enable TCP
# tls_cert = "/path/cert.pem"  # custom cert (empty = auto-generate)
# tls_key = "/path/key.pem"    # custom key (empty = auto-generate)
# tls_disabled = false          # set true for plain TCP (not recommended)
# auth_token = "my-secret"     # bearer token for TCP clients`}
    </CodeBlock>

    <h2>Alert Settings</h2>
    <p>
      Alert rules are managed via the TUI (Alerts tab, press <code>n</code>), not in the config file.
      The config file controls the evaluation interval and notification channels.
    </p>
    <CodeBlock title="bewitch.toml — [alerts]">
{`[alerts]
evaluation_interval = "10s"  # how often the alert engine evaluates rules

# Webhook notifications
[[alerts.webhooks]]
url = "https://hooks.example.com/alert"
# headers = { "X-Custom" = "value" }

# ntfy push notifications
# [[alerts.ntfy]]
# url = "https://ntfy.sh"
# topic = "bewitch-alerts"
# token = ""  # optional auth token

# Email via SMTP
# [[alerts.email]]
# smtp_host = "smtp.example.com"
# smtp_port = 587
# username = "alerts@example.com"
# password = "app-password"
# from = "alerts@example.com"
# to = ["admin@example.com"]
# starttls = true  # false for implicit TLS on port 465

# Gotify push server
# [[alerts.gotify]]
# url = "https://gotify.example.com"
# token = "AxxxxxxxxxxxxxxR"
# priority = 0  # 0 = auto-map from severity

# Shell command
# [[alerts.commands]]
# cmd = "/usr/local/bin/alert-handler"
# receives BEWITCH_RULE, BEWITCH_SEVERITY, BEWITCH_MESSAGE, BEWITCH_TIMESTAMP env vars`}
    </CodeBlock>

    <h2>TUI Settings</h2>
    <CodeBlock title="bewitch.toml — [tui]">
{`[tui]
refresh_interval = "2s"
# history_ranges = ["1h", "6h", "24h", "7d", "30d"]`}
    </CodeBlock>

    <h2>Collector Settings</h2>
    <p>
      Each collector has its own section with an <code>interval</code> field. If omitted, the collector uses
      <code>default_interval</code> from the <code>[daemon]</code> section (default 5s, minimum 100ms).
    </p>

    <CodeBlock title="bewitch.toml — collectors">
{`[collectors.cpu]
# interval = "1s"

[collectors.memory]
# interval = "5s"

[collectors.disk]
# interval = "30s"
# smart_interval = "5m"         # SMART polling (0 to disable, min 30s)
# exclude_mounts = ["/boot/efi"]  # additional mount exclusions
# no_default_excludes = false    # true to disable defaults (/snap/, /run/)

[collectors.network]
# interval = "5s"

[collectors.ecc]
# interval = "60s"

[collectors.temperature]
# interval = "5s"
# enabled = true   # false to disable

[collectors.power]
# interval = "5s"
# enabled = true   # false to disable

[collectors.process]
# interval = "5s"
# max_processes = 100
# pinned = ["nginx*", "postgres", "redis-server"]`}
    </CodeBlock>

    <h2>macOS Mock Mode</h2>
    <p>
      For TUI development on macOS, enable mock mode to generate synthetic metrics from a simulated server:
    </p>
    <CodeBlock title="dev.toml">
{`[daemon]
mock = true
socket = "/tmp/bewitch.sock"
db_path = "/tmp/bewitch.duckdb"`}
    </CodeBlock>
    <CodeBlock>
{`make build
bin/bewitchd -config dev.toml &
bin/bewitch -config dev.toml`}
    </CodeBlock>
    <p>
      Mock mode simulates an 8-core server with 32 GB RAM, two disks, two network interfaces, temperature sensors,
      power zones, and ~65 processes. Data uses smooth sine waves with jitter for a realistic feel.
    </p>
  </DocsLayout>
)
