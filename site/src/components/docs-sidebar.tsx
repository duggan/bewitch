import type { FC } from 'hono/jsx'
import { VersionDropdown, VersionDropdownMobile } from './version-dropdown'

const sections = [
  { slug: '', label: 'Overview' },
  { slug: '/installation', label: 'Installation' },
  { slug: '/configuration', label: 'Configuration' },
  { slug: '/collectors', label: 'Collectors' },
  { slug: '/tui', label: 'TUI Guide' },
  { slug: '/alerts', label: 'Alerts' },
  { slug: '/repl', label: 'SQL REPL' },
  { slug: '/remote-access', label: 'Remote Access' },
  { slug: '/api', label: 'API Reference' },
  { slug: '/archival', label: 'Storage & Archival' },
  { slug: '/changelog', label: 'Changelog' },
]

export const DocsSidebar: FC<{ active?: string; base?: string }> = ({ active, base = '/docs' }) => (
  <aside class="w-56 shrink-0 hidden lg:block">
    <div class="sticky top-18 space-y-0.5">
      <p class="font-mono text-xs text-dim uppercase tracking-wider mb-3 px-3">Documentation</p>
      <div class="px-3">
        <VersionDropdown current={base} />
      </div>
      {sections.map(s => {
        const href = `${base}${s.slug}`
        return (
          <a
            href={href}
            class={`block px-3 py-1.5 text-sm font-mono rounded border-l-2 transition-colors ${
              active === href
                ? 'sidebar-link-active'
                : 'border-transparent text-muted hover:text-text hover:bg-surface/50'
            }`}
          >
            {s.label}
          </a>
        )
      })}
    </div>
  </aside>
)

export const DocsMobileNav: FC<{ active?: string; base?: string }> = ({ active, base = '/docs' }) => (
  <div class="lg:hidden mb-6 overflow-x-auto">
    <VersionDropdownMobile current={base} />
    <div class="flex gap-2 pb-2">
      {sections.map(s => {
        const href = `${base}${s.slug}`
        return (
          <a
            href={href}
            class={`shrink-0 px-3 py-1.5 text-xs font-mono rounded-full border transition-colors ${
              active === href
                ? 'border-pink/50 text-pink bg-pink/5'
                : 'border-deep-purple/30 text-muted hover:text-text'
            }`}
          >
            {s.label}
          </a>
        )
      })}
    </div>
  </div>
)
