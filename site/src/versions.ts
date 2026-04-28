declare const __BEWITCH_VERSION__: string
declare const __BEWITCH_LATEST_STABLE__: string

const current = `v${__BEWITCH_VERSION__}`
const latestStable = `v${__BEWITCH_LATEST_STABLE__}`
const isDev = current !== latestStable

const archived = [
  { label: 'v0.5.0', path: '/docs/v0.5.0' },
  { label: 'v0.4.0', path: '/docs/v0.4.0' },
  { label: 'v0.3.1', path: '/docs/v0.3.1' },
  { label: 'v0.3.0', path: '/docs/v0.3.0' },
  { label: 'v0.2.0', path: '/docs/v0.2.0' },
]

export const versions = [
  ...(isDev ? [{ label: `${current}-dev`, path: '/docs/dev' }] : []),
  { label: latestStable, path: '/docs' },
  ...archived.filter(v => v.label !== latestStable),
]
