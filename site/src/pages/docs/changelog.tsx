import type { FC } from 'hono/jsx'
import { docsBase } from '../../docs-base'
import { DocsLayout } from '../../layouts/docs'

export const ChangelogDocs: FC = () => (
  <DocsLayout title="Changelog" active={`${docsBase}/changelog`}>
    <p>
      All notable changes to bewitch are documented here. See the full{' '}
      <a href="https://github.com/duggan/bewitch/blob/main/CHANGELOG.md">CHANGELOG.md</a> on GitHub.
    </p>

    <h2>0.3.1</h2>
    <p class="text-muted text-sm">2026-03-16</p>
    <h3>Fixed</h3>
    <ul>
      <li>Memory history chart empty on systems without swap</li>
      <li>Disk NULL handling in history scan</li>
      <li>Removed unnecessary <code>-config</code> flag from docs site command examples</li>
    </ul>

    <h2>0.3.0</h2>
    <p class="text-muted text-sm">2026-03-14</p>
    <h3>Changed</h3>
    <ul>
      <li>Renamed Go module from <code>github.com/ross</code> to <code>github.com/duggan</code></li>
      <li>Deduplicated schema definitions using runtime introspection</li>
      <li>Removed webhook, ntfy, and gotify notifiers in favour of simpler notification channels</li>
    </ul>
    <h3>Fixed</h3>
    <ul>
      <li>Sequence references breaking after compaction</li>
    </ul>
    <h3>Added</h3>
    <ul>
      <li>Local <code>mail</code> command support for email notifications (postfix/sendmail, no SMTP config needed)</li>
      <li>Version pulled from <code>VERSION</code> file for install script and docs</li>
    </ul>

    <h2>0.2.0</h2>
    <p class="text-muted text-sm">2026-03-14</p>
    <h3>Added</h3>
    <ul>
      <li>Braille charts with unified chart rendering across all views</li>
      <li>Hardware tab consolidating temperature, power, and ECC sub-sections</li>
      <li>Versioned docs for tagged releases</li>
      <li>Dev build pipeline for bleeding-edge apt channel</li>
    </ul>
    <h3>Fixed</h3>
    <ul>
      <li>Nil map panic in <code>updateNetSparklines</code></li>
      <li>Archive error when metric tables have no matching rows</li>
      <li>Various Cloudflare Pages Functions deployment issues</li>
    </ul>

    <h2>0.1.2</h2>
    <p class="text-muted text-sm">2026-03-13</p>
    <h3>Added</h3>
    <ul>
      <li>Initial public release</li>
      <li>Metric collectors: CPU, memory, disk, network, ECC, temperature, power, process</li>
      <li>DuckDB storage with schema migrations</li>
      <li>TUI with dashboard, per-metric views, and historical charts</li>
      <li>Alert engine with threshold, predictive, and variance rules</li>
      <li>SQL REPL with dot-commands and data export</li>
      <li>Remote access with TLS (TOFU) and bearer token auth</li>
      <li>Parquet archival and data pruning</li>
      <li>Debian packaging with systemd service</li>
      <li>APT repository with signed metadata</li>
    </ul>
  </DocsLayout>
)
