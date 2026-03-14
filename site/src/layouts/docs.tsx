import type { FC, PropsWithChildren } from 'hono/jsx'
import { docsBase } from '../docs-base'
import { Base } from './base'
import { Nav } from '../components/nav'
import { Footer } from '../components/footer'
import { DocsSidebar, DocsMobileNav } from '../components/docs-sidebar'

const isVersioned = docsBase !== '/docs'
const versionLabel = isVersioned ? docsBase.replace('/docs/', '') : null

export const DocsLayout: FC<PropsWithChildren<{ title: string; active: string }>> = ({ title, active, children }) => (
  <Base title={`${title} — bewitch docs`}>
    <Nav active="docs" />
    <div class="max-w-6xl mx-auto px-6 py-10">
      {isVersioned && (
        <div class="mb-6 px-4 py-3 rounded border border-deep-purple/30 bg-surface/30 font-mono text-sm">
          <span class="text-muted">Viewing docs for </span>
          <span class="text-pink font-semibold">{versionLabel}</span>
          <span class="text-muted"> · </span>
          <a href="/docs" class="text-soft-purple hover:text-pink transition-colors">view latest docs</a>
        </div>
      )}
      <DocsMobileNav active={active} base={docsBase} />
      <div class="flex gap-10">
        <DocsSidebar active={active} base={docsBase} />
        <main class="min-w-0 flex-1 docs-content">
          <h1 class="font-mono font-bold text-2xl text-pink glow-pink mb-8">{title}</h1>
          {children}
        </main>
      </div>
    </div>
    <Footer />
  </Base>
)
