import type { FC } from 'hono/jsx'
import { DocsLayout } from '../../layouts/docs'
import { CodeBlock } from '../../components/terminal-block'

export const CollectorsDocs: FC = () => (
  <DocsLayout title="Collectors" active="/docs/collectors">
    <p>
      Bewitch has 8 metric collectors. All implement the <code>Collector</code> interface with <code>Name()</code> and <code>Collect()</code> methods.
      Collectors run in parallel via goroutines on each tick. The daemon uses a GCD-based tick scheduler to fire each collector at its configured interval.
    </p>

    <h2>CPU</h2>
    <p>
      Reads per-core CPU usage from <code>/proc/stat</code>. Computes delta percentages between samples.
      The first sample after startup is discarded (needs a baseline).
    </p>
    <ul>
      <li><strong>Metrics:</strong> per-core usage %, aggregate %</li>
      <li><strong>Storage:</strong> <code>cpu_metrics</code> table</li>
      <li><strong>Default interval:</strong> inherits <code>default_interval</code> (5s)</li>
    </ul>

    <h2>Memory</h2>
    <p>
      Reads <code>/proc/meminfo</code> for total, free, available, buffers, cached, and swap.
      Computes used bytes and used percentage.
    </p>
    <ul>
      <li><strong>Metrics:</strong> total, used, free, available, buffers, cached, swap (bytes + percentages)</li>
      <li><strong>Storage:</strong> <code>memory_metrics</code> table</li>
    </ul>

    <h2>Disk</h2>
    <p>
      Three data sources per mount: space usage (via <code>statfs</code>), I/O rates (via <code>/proc/diskstats</code>),
      and SMART health (via <code>smartctl</code> or direct device access).
    </p>

    <h3>Space</h3>
    <ul>
      <li><strong>Metrics:</strong> total, used, free bytes; used percentage per mount</li>
      <li>Mount filtering: <code>/snap/</code> and <code>/run/</code> excluded by default</li>
    </ul>

    <h3>I/O</h3>
    <ul>
      <li><strong>Metrics:</strong> read/write bytes per second per device</li>
      <li>Delta-based: keeps previous reading, computes rate. First sample discarded.</li>
    </ul>

    <h3>SMART Health</h3>
    <p>
      Reads SMART data per physical device (not per partition). Multiple mounts from the same disk share one SMART read.
      SMART data is <strong>live-only</strong> — not stored in the database since it changes slowly.
    </p>
    <ul>
      <li><strong>NVMe:</strong> available spare %, percent used, critical warning, temperature, power-on hours, power cycles</li>
      <li><strong>SATA:</strong> reallocated sectors, pending sectors, uncorrectable errors, temperature, power-on hours</li>
      <li><strong>Fallback chain:</strong> smartctl (preferred) &#8594; smart.go library &#8594; direct SAT passthrough</li>
      <li><strong>Requires:</strong> <code>CAP_SYS_RAWIO</code> capability (configured by Debian package)</li>
    </ul>

    <CodeBlock title="bewitch.toml">
{`[collectors.disk]
interval = "30s"
smart_interval = "5m"  # min 30s, "0" to disable
exclude_mounts = ["/boot/efi"]`}
    </CodeBlock>

    <h2>Network</h2>
    <p>
      Reads per-interface bytes from <code>/proc/net/dev</code>. Computes RX/TX bytes per second.
      Delta-based with first sample discarded.
    </p>
    <ul>
      <li><strong>Metrics:</strong> rx_bytes/sec, tx_bytes/sec per interface</li>
      <li><strong>Storage:</strong> <code>network_metrics</code> table with dimension IDs for interface names</li>
    </ul>

    <h2>ECC</h2>
    <p>
      Reads ECC memory error counts from <code>/sys/devices/system/edac/</code>. Live-only data — not stored in DB.
      Useful for servers with ECC memory.
    </p>
    <ul>
      <li><strong>Metrics:</strong> correctable and uncorrectable error counts per DIMM</li>
      <li><strong>Default interval:</strong> 60s (ECC errors change very infrequently)</li>
    </ul>

    <h2>Temperature</h2>
    <p>
      Reads hardware sensor temperatures from <code>/sys/class/hwmon/</code>. Caches sensor paths and refreshes
      every 60 seconds to avoid expensive glob operations.
    </p>
    <ul>
      <li><strong>Metrics:</strong> temperature in &deg;C per sensor</li>
      <li><strong>Storage:</strong> <code>temperature_metrics</code> table with dimension IDs for sensor names</li>
      <li>Can be disabled via <code>enabled = false</code> in config</li>
      <li>TUI tab auto-hides when no temperature data is available</li>
    </ul>

    <h2>Power</h2>
    <p>
      Reads power consumption from Linux powercap/RAPL zones at <code>/sys/class/powercap/</code>.
      Delta-based, computes watts from energy counter differences. Caches zone paths (60s refresh).
    </p>
    <ul>
      <li><strong>Metrics:</strong> watts per power zone (package, core, uncore, DRAM)</li>
      <li><strong>Storage:</strong> <code>power_metrics</code> table with dimension IDs for zone names</li>
      <li>Can be disabled via <code>enabled = false</code> in config</li>
    </ul>

    <h2>Process</h2>
    <p>
      Two-phase collection. Phase 1 cheaply scans all <code>/proc/[pid]/stat</code> files. Phase 2 enriches the
      top N processes (by CPU/memory) plus pinned processes with expensive data.
    </p>

    <h3>Phase 1 (all processes)</h3>
    <ul>
      <li>PID, name, state, CPU%, RSS, thread count</li>
      <li>Very fast — reads a single file per process</li>
    </ul>

    <h3>Phase 2 (enriched processes)</h3>
    <ul>
      <li>Command line, UID, FD count, detailed memory breakdown</li>
      <li>Reads <code>/proc/[pid]/cmdline</code>, <code>/proc/[pid]/status</code>, <code>/proc/[pid]/fd</code></li>
      <li>Default: top 100 processes enriched</li>
    </ul>

    <h3>Process pinning</h3>
    <p>
      Pinned processes always receive Phase 2 enrichment regardless of ranking. Useful for monitoring
      low-resource but critical services.
    </p>
    <CodeBlock title="bewitch.toml">
{`[collectors.process]
max_processes = 100
pinned = ["nginx*", "postgres", "redis-server"]`}
    </CodeBlock>
    <p>
      Pins can also be set interactively in the TUI with the <code>*</code> key. TUI pins persist in the daemon's
      preferences database across restarts.
    </p>

    <h2>Collector Backoff</h2>
    <p>
      When a collector's <code>Collect()</code> returns an error, consecutive failures trigger exponential backoff.
      The collector skips <code>2^(n-1)</code> intervals (capped at 64x) before retrying. On success, the failure
      count resets immediately. First error is always logged; subsequent errors include attempt count and backoff duration.
    </p>

    <h2>Parallel Collection</h2>
    <p>
      Collectors due on each tick run concurrently via goroutines, reducing total cycle time. Results are gathered
      with <code>sync.WaitGroup</code>. The API cache is updated immediately after collection, then samples are
      enqueued to a buffered channel for asynchronous database writing. If the write channel is full, the batch is
      dropped with a warning.
    </p>
  </DocsLayout>
)
