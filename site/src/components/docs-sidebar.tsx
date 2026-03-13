import type { FC } from 'hono/jsx'

const sections = [
  { href: '/docs', label: 'Overview' },
  { href: '/docs/installation', label: 'Installation' },
  { href: '/docs/configuration', label: 'Configuration' },
  { href: '/docs/collectors', label: 'Collectors' },
  { href: '/docs/tui', label: 'TUI Guide' },
  { href: '/docs/alerts', label: 'Alerts' },
  { href: '/docs/repl', label: 'SQL REPL' },
  { href: '/docs/remote-access', label: 'Remote Access' },
  { href: '/docs/api', label: 'API Reference' },
  { href: '/docs/archival', label: 'Storage & Archival' },
]

export const DocsSidebar: FC<{ active?: string }> = ({ active }) => (
  <aside class="w-56 shrink-0 hidden lg:block">
    <div class="sticky top-18 space-y-0.5">
      <p class="font-mono text-xs text-dim uppercase tracking-wider mb-3 px-3">Documentation</p>
      {sections.map(s => (
        <a
          href={s.href}
          class={`block px-3 py-1.5 text-sm font-mono rounded border-l-2 transition-colors ${
            active === s.href
              ? 'sidebar-link-active'
              : 'border-transparent text-muted hover:text-text hover:bg-surface/50'
          }`}
        >
          {s.label}
        </a>
      ))}
    </div>
  </aside>
)

export const DocsMobileNav: FC<{ active?: string }> = ({ active }) => (
  <div class="lg:hidden mb-6 overflow-x-auto">
    <div class="flex gap-2 pb-2">
      {sections.map(s => (
        <a
          href={s.href}
          class={`shrink-0 px-3 py-1.5 text-xs font-mono rounded-full border transition-colors ${
            active === s.href
              ? 'border-pink/50 text-pink bg-pink/5'
              : 'border-deep-purple/30 text-muted hover:text-text'
          }`}
        >
          {s.label}
        </a>
      ))}
    </div>
  </div>
)
