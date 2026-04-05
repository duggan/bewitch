import type { FC } from 'hono/jsx'

export const Footer: FC = () => (
  <footer class="border-t border-deep-purple/20 py-12 mt-20">
    <div class="max-w-6xl mx-auto px-6 flex flex-col items-center gap-4 text-center">
      <img src="/favicon.png" alt="" class="w-10 h-10 opacity-60" width="40" height="40" />
      <p class="text-muted text-sm font-mono">
        A charming system monitor
      </p>
      <div class="flex items-center gap-6 text-xs font-mono text-dim">
        <a
          href="https://github.com/duggan/bewitch"
          class="hover:text-pink transition-colors"
        >
          GitHub
        </a>
        <span class="text-deep-purple/50">|</span>
        <a
          href="/docs"
          class="hover:text-pink transition-colors"
        >
          Documentation
        </a>
      </div>
    </div>
  </footer>
)
