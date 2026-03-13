import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'

const sections = [
  { href: '/docs/installation', title: 'Installation', desc: 'Debian package, manual install, and systemd setup.' },
  { href: '/docs/configuration', title: 'Configuration', desc: 'Complete TOML config reference with all options.' },
  { href: '/docs/collectors', title: 'Collectors', desc: 'All 8 metric collectors: CPU, memory, disk, network, ECC, temperature, power, process.' },
  { href: '/docs/tui', title: 'TUI Guide', desc: 'Views, navigation, keybindings, debug mode, and process pinning.' },
  { href: '/docs/alerts', title: 'Alerts', desc: 'Threshold, predictive, and variance rules with multi-channel notifications.' },
  { href: '/docs/repl', title: 'SQL REPL', desc: 'Interactive DuckDB console with dot-commands and data export.' },
  { href: '/docs/remote-access', title: 'Remote Access', desc: 'TLS with TOFU fingerprint pinning and bearer token authentication.' },
  { href: '/docs/api', title: 'API Reference', desc: 'HTTP endpoints for metrics, alerts, history, query, export, and more.' },
  { href: '/docs/archival', title: 'Storage & Archival', desc: 'DuckDB storage, retention, compaction, Parquet archival, and snapshots.' },
]

export const DocsIndex: FC = () => (
  <DocsLayout title="Documentation" active="/docs">
    <p>
      Bewitch is a system monitoring daemon (<code>bewitchd</code>) and TUI client (<code>bewitch</code>) for Linux,
      built with Go and DuckDB. It collects CPU, memory, disk, network, temperature, power, and process metrics,
      stores them in DuckDB, and provides a rich interactive interface with historical charts, alerting, and a SQL REPL.
    </p>

    <h2>Architecture</h2>
    <pre class="bg-surface rounded-lg p-4 font-mono text-sm text-muted overflow-x-auto border border-deep-purple/20 mb-8">{`bewitchd (daemon)
\u251C\u2500\u2500 Collectors (procfs/sysfs, parallel goroutines) \u2192 Store (DuckDB)
\u251C\u2500\u2500 Alert Engine (threshold + predictive + variance \u2192 notifications)
\u251C\u2500\u2500 Pruner / Compactor / Archiver
\u2514\u2500\u2500 API Server
    \u251C\u2500\u2500 Unix socket (always, plain HTTP)
    \u2514\u2500\u2500 TCP listener (optional, TLS by default)

bewitch (TUI)
\u2514\u2500\u2500 Daemon Client (unix socket or TCP+TLS)

bewitch repl (SQL console)
\u2514\u2500\u2500 Daemon Client (POST /api/query)`}</pre>

    <h2>Sections</h2>
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
      {sections.map(s => (
        <a
          href={s.href}
          class="block p-4 rounded-lg border border-deep-purple/30 bg-surface/30 hover:border-deep-purple/60 hover:bg-surface/50 transition-all group"
        >
          <h3 class="font-mono font-semibold text-sm text-pink group-hover:glow-pink mb-1">{s.title}</h3>
          <p class="text-muted text-sm">{s.desc}</p>
        </a>
      ))}
    </div>
  </DocsLayout>
)
