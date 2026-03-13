import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const TuiDocs: FC = () => (
  <DocsLayout title="TUI Guide" active="/docs/tui">
    <p>
      The bewitch TUI is built with <a href="https://github.com/charmbracelet/bubbletea">bubbletea</a> and
      provides 9 views for real-time system monitoring with historical charts.
    </p>

    <CodeBlock title="launch">
{`bewitch -config /etc/bewitch.toml

# or connect to a remote daemon
bewitch -addr myserver:9119 -token my-secret`}
    </CodeBlock>

    <h2>Views</h2>
    <p>
      Views are accessed via number keys. Temperature and Power tabs appear only when data is available.
      When tabs are hidden, subsequent tabs renumber automatically.
    </p>
    <table>
      <thead>
        <tr><th>Key</th><th>View</th><th>Description</th></tr>
      </thead>
      <tbody>
        <tr><td><code>1</code></td><td>Dashboard</td><td>Multi-column overview of all subsystems</td></tr>
        <tr><td><code>2</code></td><td>CPU</td><td>Per-core usage with historical chart</td></tr>
        <tr><td><code>3</code></td><td>Memory</td><td>Memory breakdown with ECC errors and history</td></tr>
        <tr><td><code>4</code></td><td>Disk</td><td>Per-mount space, I/O rates, SMART health</td></tr>
        <tr><td><code>5</code></td><td>Network</td><td>Per-interface throughput with bits/bytes toggle</td></tr>
        <tr><td><code>6</code></td><td>Temperature</td><td>Sensor readings with sparklines (if available)</td></tr>
        <tr><td><code>7</code></td><td>Power</td><td>Per-zone watts via RAPL (if available)</td></tr>
        <tr><td><code>8</code></td><td>Process</td><td>All processes, sortable, searchable, pinnable</td></tr>
        <tr><td><code>9</code></td><td>Alerts</td><td>Alert rules and fired alerts</td></tr>
      </tbody>
    </table>

    <h2>Navigation</h2>
    <table>
      <thead>
        <tr><th>Key</th><th>Action</th></tr>
      </thead>
      <tbody>
        <tr><td><code>Tab</code> / <code>Shift+Tab</code></td><td>Cycle views forward / backward</td></tr>
        <tr><td><code>&larr;</code> / <code>&rarr;</code> or <code>h</code> / <code>l</code></td><td>Cycle views forward / backward</td></tr>
        <tr><td><code>&lt;</code> / <code>&gt;</code></td><td>Cycle history time range (1h, 6h, 24h, 7d, 30d)</td></tr>
        <tr><td><code>q</code></td><td>Quit</td></tr>
      </tbody>
    </table>

    <h2>Temperature & Power Views</h2>
    <p>
      These views have per-sensor/zone toggle selection controlling which items appear on the history chart.
      Selections are persisted in the daemon's preferences database.
    </p>
    <table>
      <thead>
        <tr><th>Key</th><th>Action</th></tr>
      </thead>
      <tbody>
        <tr><td><code>j</code> / <code>k</code></td><td>Navigate sensor/zone list</td></tr>
        <tr><td><code>Space</code></td><td>Toggle sensor/zone in chart</td></tr>
        <tr><td><code>a</code></td><td>Select / deselect all</td></tr>
      </tbody>
    </table>

    <h2>Process View</h2>
    <p>
      Shows all processes on the system. Non-enriched processes display <code>--</code> for cmdline and FD count.
      The history chart shows top N processes by CPU over time.
    </p>
    <table>
      <thead>
        <tr><th>Key</th><th>Action</th></tr>
      </thead>
      <tbody>
        <tr><td><code>j</code> / <code>k</code></td><td>Navigate process list</td></tr>
        <tr><td><code>c</code></td><td>Sort by CPU</td></tr>
        <tr><td><code>m</code></td><td>Sort by memory</td></tr>
        <tr><td><code>p</code></td><td>Sort by PID</td></tr>
        <tr><td><code>n</code></td><td>Sort by name</td></tr>
        <tr><td><code>t</code></td><td>Sort by threads</td></tr>
        <tr><td><code>f</code></td><td>Sort by FDs</td></tr>
        <tr><td><code>*</code></td><td>Pin/unpin selected process</td></tr>
        <tr><td><code>a</code></td><td>Create alert for selected process</td></tr>
        <tr><td><code>/</code></td><td>Search by name or cmdline</td></tr>
        <tr><td><code>Esc</code></td><td>Clear search filter</td></tr>
        <tr><td><code>P</code></td><td>Toggle pinned-only filter</td></tr>
        <tr><td><code>Tab</code></td><td>Toggle history chart: Top CPU / Pinned</td></tr>
      </tbody>
    </table>

    <h2>Alerts View</h2>
    <p>
      Two panels: a rules list on the left and fired alerts on the right. Create, toggle, and delete rules directly from the TUI.
    </p>
    <table>
      <thead>
        <tr><th>Key</th><th>Action</th></tr>
      </thead>
      <tbody>
        <tr><td><code>j</code> / <code>k</code></td><td>Navigate rules or alerts</td></tr>
        <tr><td><code>Tab</code></td><td>Switch focus between rules and alerts</td></tr>
        <tr><td><code>n</code></td><td>Create new alert rule</td></tr>
        <tr><td><code>d</code></td><td>Delete selected rule</td></tr>
        <tr><td><code>Space</code></td><td>Toggle rule enabled/disabled</td></tr>
        <tr><td><code>Enter</code></td><td>Acknowledge selected alert</td></tr>
        <tr><td><code>t</code></td><td>Test all notification channels</td></tr>
        <tr><td><code>Esc</code></td><td>Cancel alert creation form</td></tr>
      </tbody>
    </table>

    <h2>Historical Charts</h2>
    <p>
      CPU, memory, disk, temperature, power, and process views include a historical line chart below the live data.
      Use <code>&lt;</code> / <code>&gt;</code> to cycle through time ranges: 1h, 6h, 24h, 7d, 30d.
    </p>
    <p>
      Charts use DuckDB's <code>time_bucket</code> aggregation. Bucket size auto-scales based on the selected range
      (1 minute for 1h up to 6 hours for 30d). History data is fetched asynchronously and cached per-view for instant display on tab switch.
    </p>

    <h2>Staleness Detection</h2>
    <p>
      The status bar monitors when fresh data last arrived for the current view. If no new data appears within
      3x the longest collector interval for that view, a stale indicator shows: <code>stale (Xs ago)</code>.
    </p>

    <h2>Debug Mode</h2>
    <CodeBlock>
{`bewitch -config /etc/bewitch.toml -debug`}
    </CodeBlock>
    <p>
      Adds a scrollable debug console at the bottom of the TUI showing timestamped diagnostic messages:
      data fetches, cache hits/misses, view transitions, errors, and pin operations.
    </p>
    <table>
      <thead>
        <tr><th>Key</th><th>Action</th></tr>
      </thead>
      <tbody>
        <tr><td><code>{'{'}</code> / <code>{'}'}</code></td><td>Scroll debug console up / down</td></tr>
        <tr><td><code>(</code> / <code>)</code></td><td>Shrink / grow debug panel</td></tr>
      </tbody>
    </table>

    <h2>Styling</h2>
    <p>
      The TUI uses a pink and purple color palette with bordered panels (<code>lipgloss.RoundedBorder</code>).
      The dashboard adapts to a multi-column grid layout on terminals wider than 120 columns.
    </p>
  </DocsLayout>
)
