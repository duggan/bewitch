import type { FC, PropsWithChildren } from 'hono/jsx'
import { Base } from './base'
import { Nav } from '../components/nav'
import { Footer } from '../components/footer'
import { DocsSidebar, DocsMobileNav } from '../components/docs-sidebar'

export const DocsLayout: FC<PropsWithChildren<{ title: string; active: string }>> = ({ title, active, children }) => (
  <Base title={`${title} — bewitch docs`}>
    <Nav active="docs" />
    <div class="max-w-6xl mx-auto px-6 py-10">
      <DocsMobileNav active={active} />
      <div class="flex gap-10">
        <DocsSidebar active={active} />
        <main class="min-w-0 flex-1 docs-content">
          <h1 class="font-mono font-bold text-2xl text-pink glow-pink mb-8">{title}</h1>
          {children}
        </main>
      </div>
    </div>
    <Footer />
  </Base>
)
