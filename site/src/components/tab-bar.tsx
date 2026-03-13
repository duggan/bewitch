import type { FC } from 'hono/jsx'

const tabs = ['Dashboard', 'CPU', 'Memory', 'Disk', 'Network', 'Temp', 'Power', 'Process', 'Alerts']

export const TabBar: FC<{ active?: number }> = ({ active = 0 }) => (
  <div class="flex items-center gap-0 font-mono text-xs border-b border-deep-purple/30 overflow-x-auto">
    {tabs.map((tab, i) => (
      <span
        class={`px-3 py-1.5 border-b-2 whitespace-nowrap ${
          i === active
            ? 'border-pink text-pink bg-pink/5'
            : 'border-transparent text-dim hover:text-muted'
        }`}
      >
        {i + 1} {tab}
      </span>
    ))}
  </div>
)
