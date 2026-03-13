import type { FC } from 'hono/jsx'

export const InstallCommand: FC = () => (
  <div class="glow-box rounded-lg overflow-hidden border border-deep-purple/50 max-w-2xl mx-auto">
    <div class="flex items-center gap-2 px-4 py-2 bg-surface border-b border-deep-purple/30">
      <span class="w-3 h-3 rounded-full bg-[#ff5f57] opacity-60" />
      <span class="w-3 h-3 rounded-full bg-[#ffbd2e] opacity-60" />
      <span class="w-3 h-3 rounded-full bg-[#28c840] opacity-60" />
      <span class="ml-2 text-xs text-muted font-mono">install</span>
    </div>
    <div class="bg-body-bg px-4 py-3 flex items-center justify-between gap-4">
      <code class="font-mono text-sm text-text">
        <span class="text-purple">$</span>{' '}
        <span class="text-lavender">curl -sSL</span>{' '}
        <span class="text-pink">https://bewitch.dev/install.sh</span>{' '}
        <span class="text-lavender">| sh</span>
      </code>
      <button
        class="copy-btn shrink-0 px-2.5 py-1 rounded text-xs font-mono text-muted border border-deep-purple/30 hover:text-pink hover:border-pink/30 transition-colors"
        data-copy="curl -sSL https://bewitch.dev/install.sh | sh"
      >
        copy
      </button>
    </div>
  </div>
)
