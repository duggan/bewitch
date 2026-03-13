import type { FC } from 'hono/jsx'

export const Nav: FC<{ active?: string }> = ({ active }) => (
  <nav class="sticky top-0 z-50 backdrop-blur-md bg-body-bg/80 border-b border-deep-purple/30">
    <div class="max-w-6xl mx-auto px-6 h-14 flex items-center justify-between">
      <a href="/" class="flex items-center gap-2.5 group">
        <img src="/favicon.png" alt="" class="w-8 h-8" width="32" height="32" />
        <span class="font-mono font-bold text-lg text-pink glow-pink">bewitch</span>
      </a>
      <div class="flex items-center gap-6 text-sm font-mono">
        <a
          href="/docs"
          class={`transition-colors hover:text-pink ${active === 'docs' ? 'text-pink' : 'text-muted'}`}
        >
          docs
        </a>
        <a
          href="https://github.com/duggan/bewitch"
          class="text-muted transition-colors hover:text-pink"
          target="_blank"
          rel="noopener"
        >
          github
        </a>
      </div>
    </div>
  </nav>
)
