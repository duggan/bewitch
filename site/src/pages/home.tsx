import type { FC } from 'hono/jsx'
import { Base } from '../layouts/base'
import { Nav } from '../components/nav'
import { Footer } from '../components/footer'
import { FeatureCard } from '../components/feature-card'
import { InstallCommand } from '../components/install-command'
import { TerminalBlock } from '../components/terminal-block'

const features = [
  { icon: 'cpu', title: 'CPU Monitoring', description: 'Per-core usage tracking with aggregate metrics, historical charts, and automatic delta computation.', href: '/docs/collectors#cpu' },
  { icon: 'memory-stick', title: 'Memory + ECC', description: 'Real-time memory usage, available/free breakdown, ECC error tracking for server reliability.', href: '/docs/collectors#memory' },
  { icon: 'hard-drive', title: 'Disk + SMART', description: 'Space usage, I/O rates, and SMART health per physical device. NVMe and SATA supported.', href: '/docs/collectors#disk' },
  { icon: 'network', title: 'Network', description: 'Per-interface RX/TX throughput with bits/bytes toggle and historical bandwidth charts.', href: '/docs/collectors#network' },
  { icon: 'thermometer', title: 'Hardware', description: 'Temperature sensors, GPU monitoring (Intel and NVIDIA), power consumption (RAPL), and ECC memory errors in one unified view with sub-tab navigation.', href: '/docs/collectors#temperature' },
  { icon: 'list-tree', title: 'Process Tracking', description: 'All processes visible, top N enriched. Glob-pattern pinning for critical services. Sortable, searchable.', href: '/docs/collectors#process' },
  { icon: 'bell-ring', title: 'Multi-Channel Alerts', description: 'Threshold, predictive, and variance rules. Notify via email or shell command.', href: '/docs/alerts' },
]

export const Home: FC = () => (
  <Base>
    <Nav />

    {/* Hero */}
    <section class="hero-bg min-h-[85vh] flex flex-col items-center justify-center px-6 py-20">
      <div class="mascot-float mascot-glow mb-8">
        <img
          src="/witch.png"
          alt="bewitch mascot"
          class="w-40 h-40 md:w-52 md:h-52"
          width="208"
          height="208"
        />
      </div>
      <h1 class="font-mono font-bold text-4xl md:text-6xl text-pink glow-pink mb-4 text-center">
        bewitch
      </h1>
      <p class="text-lg md:text-xl text-purple font-mono mb-2 text-center">
        A charming system monitor for Linux
      </p>
      <p class="text-muted text-sm md:text-base max-w-xl text-center mb-10">
        Real-time TUI dashboard with DuckDB storage, interactive SQL REPL, multi-channel alerting, and remote access over TLS.
      </p>
      <InstallCommand />
    </section>

    {/* Features */}
    <section class="max-w-6xl mx-auto px-6 py-20">
      <h2 class="font-mono font-bold text-xl text-purple glow-purple mb-10 text-center">
        Everything you need to monitor your server
      </h2>
      <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {features.map(f => (
          <FeatureCard icon={f.icon} title={f.title} description={f.description} href={f.href} />
        ))}
      </div>
    </section>

    {/* Demo */}
    <section class="max-w-6xl mx-auto px-6 py-16">
      <h2 class="font-mono font-bold text-xl text-purple glow-purple mb-8 text-center">
        See it in action
      </h2>
      <div class="glow-box rounded-lg overflow-hidden border border-deep-purple/50" id="demo-terminal">
        <div class="flex items-center gap-0 font-mono text-xs border-b border-deep-purple/30 overflow-x-auto" id="demo-tab-overlay">
          {['Dashboard', 'CPU', 'Memory', 'Disk', 'Network', 'Hardware', 'Process', 'Alerts'].map((tab, i) => (
            <button
              type="button"
              data-demo-tab={i}
              class={`px-3 py-1.5 border-b-2 whitespace-nowrap transition-colors cursor-pointer ${
                i === 0
                  ? 'border-pink text-pink bg-pink/5'
                  : 'border-transparent text-dim hover:text-muted'
              }`}
            >
              {i + 1} {tab}
            </button>
          ))}
        </div>
        <div id="demo-terminal-mount" class="demo-terminal-mount" style="display:none"></div>
        {/* Fallback: static PNG for no-JS / before xterm loads */}
        <div id="demo-fallback" class="relative bg-body-bg">
          <img
            src="/screenshots/dashboard.png"
            alt="bewitch dashboard view"
            class="w-full block"
            loading="eager"
            width="2072"
            height="1280"
          />
        </div>
        <div class="text-center text-dim text-xs py-2 font-mono">
          Press 1–8 to switch views · &lt;/&gt; change time range · Tab cycles hardware sections
        </div>
      </div>
    </section>

    {/* Highlights */}
    <section class="max-w-5xl mx-auto px-6 py-16 space-y-16">

      {/* SQL REPL */}
      <div class="flex flex-col lg:flex-row items-start gap-8">
        <div class="lg:w-1/3">
          <h3 class="font-mono font-semibold text-lg text-pink mb-2">Your metrics in SQL</h3>
          <p class="text-muted text-sm leading-relaxed">
            Query your metrics directly with DuckDB SQL. Interactive REPL with tab completion, multi-line editing, dot-commands, and data export to CSV, Parquet, or JSON.
          </p>
        </div>
        <div class="lg:w-2/3 w-full">
          <TerminalBlock title="bewitch repl">
            <div>
              <span class="text-purple">bewitch&gt;</span>{' '}
              <span class="text-text">SELECT d.value AS mount,</span>
            </div>
            <div>
              <span class="text-purple">{'    '}...&gt;</span>{' '}
              <span class="text-text">AVG(m.used_bytes * 100.0 / m.total_bytes) AS pct</span>
            </div>
            <div>
              <span class="text-purple">{'    '}...&gt;</span>{' '}
              <span class="text-text">FROM disk_metrics m JOIN dimension_values d</span>
            </div>
            <div>
              <span class="text-purple">{'    '}...&gt;</span>{' '}
              <span class="text-text">ON d.id = m.mount_id</span>
            </div>
            <div>
              <span class="text-purple">{'    '}...&gt;</span>{' '}
              <span class="text-text">WHERE m.ts &gt; now() - INTERVAL '1 hour'</span>
            </div>
            <div>
              <span class="text-purple">{'    '}...&gt;</span>{' '}
              <span class="text-text">GROUP BY d.value;</span>
            </div>
            <div class="mt-2 text-dim">
              {' '}mount | pct{'\n'}
            </div>
            <div class="text-dim">-------+------</div>
            <div class="text-text">
              {' '}/{'     '}| 62.34{'\n'}
            </div>
            <div class="text-text">
              {' '}/home | 41.17
            </div>
            <div class="text-dim mt-1">(2 rows)</div>
          </TerminalBlock>
        </div>
      </div>

      {/* Notifications */}
      <div class="flex flex-col lg:flex-row-reverse items-start gap-8">
        <div class="lg:w-1/3">
          <h3 class="font-mono font-semibold text-lg text-pink mb-2">Email and command alerts</h3>
          <p class="text-muted text-sm leading-relaxed">
            Route alerts via email (local mail command or SMTP) or arbitrary shell commands. All config-driven, no code required.
          </p>
        </div>
        <div class="lg:w-2/3 w-full">
          <TerminalBlock title="bewitch.toml">
            <div><span class="text-dim">[[alerts.email]]</span></div>
            <div><span class="text-lavender">use_mail_cmd</span> <span class="text-dim">=</span> <span class="text-pink">true</span></div>
            <div><span class="text-lavender">to</span> <span class="text-dim">=</span> <span class="text-pink">["ops@example.com"]</span></div>
            <div class="mt-2"><span class="text-dim">[[alerts.commands]]</span></div>
            <div><span class="text-lavender">cmd</span> <span class="text-dim">=</span> <span class="text-pink">"/usr/local/bin/my-handler"</span></div>
          </TerminalBlock>
        </div>
      </div>

      {/* TLS TOFU */}
      <div class="flex flex-col lg:flex-row items-start gap-8">
        <div class="lg:w-1/3">
          <h3 class="font-mono font-semibold text-lg text-pink mb-2">SSH-style trust on first use</h3>
          <p class="text-muted text-sm leading-relaxed">
            Connect to remote daemons over TLS with auto-generated certificates and SSH-like fingerprint pinning. No CA infrastructure needed.
          </p>
        </div>
        <div class="lg:w-2/3 w-full">
          <TerminalBlock title="terminal">
            <div><span class="text-purple">$</span> <span class="text-text">bewitch -addr myserver:9119</span></div>
            <div class="mt-2 text-muted">TLS fingerprint for myserver:9119:</div>
            <div class="text-lavender">  sha256:a1b2c3d4e5f6...</div>
            <div class="mt-1">
              <span class="text-muted">Trust this server?</span>{' '}
              <span class="text-dim">[y/N]:</span>{' '}
              <span class="text-pink">y</span>
            </div>
            <div class="mt-1 text-muted">Fingerprint saved to ~/.config/bewitch/known_hosts</div>
          </TerminalBlock>
        </div>
      </div>
    </section>

    {/* Quick Start */}
    <section class="max-w-3xl mx-auto px-6 py-16">
      <h2 class="font-mono font-bold text-xl text-purple glow-purple mb-10 text-center">
        Quick start
      </h2>
      <div class="space-y-4">
        <div>
          <p class="font-mono text-xs text-dim mb-2">1. Install</p>
          <TerminalBlock>
            <span class="text-purple">$</span> <span class="text-text">curl -sSL https://bewitch.dev/install.sh | sh</span>
          </TerminalBlock>
        </div>
        <div>
          <p class="font-mono text-xs text-dim mb-2">2. Configure</p>
          <TerminalBlock>
            <span class="text-purple">$</span> <span class="text-text">sudo vim /etc/bewitch.toml</span>
          </TerminalBlock>
        </div>
        <div>
          <p class="font-mono text-xs text-dim mb-2">3. Start the daemon</p>
          <TerminalBlock>
            <span class="text-purple">$</span> <span class="text-text">sudo systemctl enable --now bewitchd</span>
          </TerminalBlock>
        </div>
        <div>
          <p class="font-mono text-xs text-dim mb-2">4. Launch the TUI</p>
          <TerminalBlock>
            <span class="text-purple">$</span> <span class="text-text">bewitch</span>
          </TerminalBlock>
        </div>
      </div>
    </section>

    <Footer />
  </Base>
)
