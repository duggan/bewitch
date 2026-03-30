import type { FC, PropsWithChildren } from 'hono/jsx'

export const TerminalBlock: FC<PropsWithChildren<{ title?: string; class?: string }>> = ({ title, class: className, children }) => (
  <div class={`glow-box rounded-lg overflow-hidden border border-deep-purple/50 ${className ?? ''}`}>
    <div class="flex items-center gap-2 px-4 py-2 bg-surface border-b border-deep-purple/30">
      <span class="w-3 h-3 rounded-full bg-[#ff5f57] opacity-60" />
      <span class="w-3 h-3 rounded-full bg-[#ffbd2e] opacity-60" />
      <span class="w-3 h-3 rounded-full bg-[#28c840] opacity-60" />
      {title && <span class="ml-2 text-xs text-muted font-mono">{title}</span>}
    </div>
    <div class="bg-body-bg p-4 font-mono text-sm leading-relaxed overflow-x-auto">
      {children}
    </div>
  </div>
)

export const CodeBlock: FC<PropsWithChildren<{ title?: string; class?: string }>> = ({ title, class: className, children }) => (
  <div class={`rounded-lg overflow-hidden border border-deep-purple/30 my-4 ${className ?? ''}`}>
    <div class="flex items-center justify-between gap-2 px-4 py-1.5 bg-surface border-b border-deep-purple/20">
      <span class="text-xs text-muted font-mono">{title ?? ''}</span>
      <button
        class="copy-btn shrink-0 px-2 py-0.5 rounded text-xs font-mono text-muted border border-deep-purple/30 hover:text-pink hover:border-pink/30 transition-colors"
      >
        copy
      </button>
    </div>
    <pre class="bg-body-bg p-4 font-mono text-sm leading-relaxed overflow-x-auto">
      <code>{children}</code>
    </pre>
  </div>
)
