import type { FC } from 'hono/jsx'
import { versions } from '../versions'

export const VersionDropdown: FC<{ current: string }> = ({ current }) => {
  const currentVersion = versions.find(v => v.path === current) || versions[0]

  return (
    <div class="relative mb-4" id="version-dropdown-wrap">
      <button
        data-version-toggle
        class="w-full flex items-center justify-between px-3 py-1.5 text-sm font-mono rounded border border-deep-purple/30 bg-surface/50 text-muted hover:text-text hover:border-deep-purple/50 transition-colors cursor-pointer"
      >
        <span>{currentVersion.label}</span>
        <svg class="w-3.5 h-3.5 ml-2 opacity-50" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M3 5l3 3 3-3" />
        </svg>
      </button>
      <div id="version-dropdown" class="hidden absolute z-50 mt-1 w-full rounded border border-deep-purple/30 bg-surface shadow-lg shadow-black/30 overflow-hidden">
        {versions.map(v => (
          <a
            href={v.path}
            class={`block px-3 py-1.5 text-sm font-mono transition-colors ${
              v.path === current
                ? 'text-pink bg-pink/10'
                : 'text-muted hover:text-text hover:bg-surface-light'
            }`}
          >
            {v.label}
          </a>
        ))}
      </div>
    </div>
  )
}

export const VersionDropdownMobile: FC<{ current: string }> = ({ current }) => {
  const currentVersion = versions.find(v => v.path === current) || versions[0]

  return (
    <div class="relative inline-block mb-4" id="version-dropdown-mobile-wrap">
      <button
        data-version-toggle
        class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-mono rounded-full border border-deep-purple/30 text-muted hover:text-text transition-colors cursor-pointer"
      >
        <span>{currentVersion.label}</span>
        <svg class="w-3 h-3 opacity-50" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5">
          <path d="M3 5l3 3 3-3" />
        </svg>
      </button>
      <div id="version-dropdown-mobile" class="hidden absolute z-50 mt-1 min-w-28 rounded border border-deep-purple/30 bg-surface shadow-lg shadow-black/30 overflow-hidden">
        {versions.map(v => (
          <a
            href={v.path}
            class={`block px-3 py-1.5 text-xs font-mono transition-colors ${
              v.path === current
                ? 'text-pink bg-pink/10'
                : 'text-muted hover:text-text hover:bg-surface-light'
            }`}
          >
            {v.label}
          </a>
        ))}
      </div>
    </div>
  )
}
