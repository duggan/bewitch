import type { FC } from 'hono/jsx'
import { docsBase } from '../../docs-base'
import { DocsLayout } from '../../layouts/docs'

export const ChangelogDocs: FC = () => (
  <DocsLayout title="Changelog" active={`${docsBase}/changelog`}>
    <p>
      All notable changes to bewitch are documented here. See the full{' '}
      <a href="https://github.com/duggan/bewitch/blob/main/CHANGELOG.md">CHANGELOG.md</a> on GitHub.
    </p>

    <h2>0.6.0</h2>
    <p class="text-muted text-sm">2026-06-04</p>
    <h3>Added</h3>
    <ul>
      <li>Active alerts are now surfaced in the TUI status bar</li>
      <li>Alert rules can be edited in place — <code>e</code> in the alerts view, or <code>PUT /api/alert-rules/{'{id}'}</code> — instead of delete-and-recreate</li>
      <li>Alerts view now shows a full rule detail pane, delete confirmation, and surfaced create/update errors</li>
    </ul>
    <h3>Changed</h3>
    <ul>
      <li>DuckDB memory is capped via a configurable <code>[daemon] db_memory_limit</code> (default <code>512MB</code>) with spill-to-disk, instead of DuckDB's ~80%-of-RAM default that could exhaust low-memory hosts</li>
      <li>Parquet archival now runs incrementally (per-day, resumable) and no longer pauses the daemon for the whole run</li>
      <li>Alert rule names must now be unique, enforced by a database index; any pre-existing duplicates are auto-renamed on upgrade</li>
    </ul>
    <h3>Fixed</h3>
    <ul>
      <li>Alert rules created via the API or TUI never fired — the type-specific config row was linked with <code>rule_id=0</code> (the DuckDB driver has no <code>LastInsertId()</code>), so the engine's join never matched. Rules created before this fix must be recreated.</li>
      <li>Deleting an alert rule now also clears its fired alerts, instead of leaving orphaned, undismissable "active" alerts</li>
      <li><code>bewitchd</code> memory leak that could OOM-kill small/low-RAM hosts — the process-info cache is now bounded by an always-on eviction sweep regardless of retention settings</li>
      <li>Process <code>rss_bytes</code> was stored 1024× too large (≈1 TB reported for a ~1 GB process); now correct for new samples</li>
      <li>Enabling Parquet archival no longer freezes the daemon on large databases</li>
    </ul>

    <h2>0.5.2</h2>
    <p class="text-muted text-sm">2026-05-26</p>
    <h3>Fixed</h3>
    <ul>
      <li>Unbounded <code>process_info</code> cache growth on hosts with high process churn</li>
      <li>Background goroutines (scheduled jobs, checkpoint loop) now stop cleanly on daemon shutdown instead of running against a closing DB</li>
      <li>Notification sends bounded by an 8-slot semaphore</li>
      <li>Intel GPU reader accumulation buffer capped at 1MB</li>
    </ul>

    <h2>0.5.1</h2>
    <p class="text-muted text-sm">2026-04-28</p>
    <h3>Added</h3>
    <ul>
      <li><code>bewitch stats</code> subcommand for at-a-glance system footprint</li>
      <li>Auto-generated API reference docs page</li>
      <li>Uninstall script (<code>uninstall.sh</code>) supporting both APT and tarball installs, with <code>KEEP_DATA=1</code> to preserve the database</li>
      <li>Documented <code>b</code> keybinding (bits/bytes toggle) on the network view</li>
    </ul>
    <h3>Changed</h3>
    <ul>
      <li>Installer auto-starts <code>bewitchd</code> after install — quick start reduced from 4 steps to 2</li>
      <li>Internal refactors to consolidate duplicated plumbing across collectors, API handlers, REPL, and TUI client</li>
    </ul>

    <h2>0.5.0</h2>
    <p class="text-muted text-sm">2026-04-02</p>
    <h3>Added</h3>
    <ul>
      <li>Screen capture to PNG via <code>x</code> key with configurable <code>[tui.capture]</code> settings</li>
      <li><code>capture-views</code> subcommand for batch screenshot capture</li>
      <li>Dev docs channel with automatic publishing for pre-release versions</li>
    </ul>
    <h3>Changed</h3>
    <ul>
      <li>Restart bewitchd automatically on package upgrade via <code>dh_installsystemd</code></li>
    </ul>
    <h3>Fixed</h3>
    <ul>
      <li>DuckDB WAL replay crash by checkpointing after migrations</li>
      <li>History chart flicker on tab switch via per-view chart cache</li>
      <li>Empty history charts when switching hardware sub-tabs</li>
      <li>Docs version dropdown not showing latest stable release</li>
    </ul>

    <h2>0.4.0</h2>
    <p class="text-muted text-sm">2026-03-30</p>
    <h3>Added</h3>
    <ul>
      <li>GPU collector with Intel iGPU (<code>intel_gpu_top</code>) and NVIDIA (<code>nvidia-smi</code>) support</li>
      <li>Actionable hints in GPU view when monitoring tools are missing</li>
      <li>Per-collector API cache push for immediate data freshness</li>
      <li>Load live data and hardware history immediately on startup</li>
      <li>Show enriched processes above the fold in process view</li>
      <li>Copy-to-clipboard button on docs code blocks</li>
      <li>E2E installation testing workflow</li>
    </ul>
    <h3>Fixed</h3>
    <ul>
      <li>Stale history chart shown on view switch cache miss</li>
      <li>Maintainer email in Debian packaging</li>
      <li>Dev version ordering (full timestamp instead of git SHA)</li>
    </ul>
    <h3>Changed</h3>
    <ul>
      <li>Enhanced installer with optional dependency prompts, dev channel, and version stamping</li>
    </ul>

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
