// Dynamic import for code splitting — ghostty-web WASM loads only when needed
type GhosttyModule = typeof import('ghostty-web')

interface DemoStateMap {
  cols: number
  rows: number
  states: Record<string, string[]>
  transitions: Record<string, Record<string, string>>
  initial: string
}

const VIEW_NAMES = ['dashboard', 'cpu', 'memory', 'disk', 'network', 'hardware', 'process', 'alerts']
const FRAME_INTERVAL = 800 // ms between frame advances
const IDLE_RESUME_DELAY = 2000 // ms before resuming after interaction

export async function initTerminalDemo() {
  const mount = document.getElementById('demo-terminal-mount')
  if (!mount) return

  // On small screens, keep the static PNG fallback instead of the terminal
  if (window.innerWidth <= 768) return

  let data: DemoStateMap
  let ghostty: GhosttyModule
  try {
    const [r, mod] = await Promise.all([
      fetch('/demo-frames.json'),
      import('ghostty-web'),
    ])
    data = await r.json()
    ghostty = mod
  } catch {
    return // Fallback: leave the noscript/PNG content visible
  }

  await ghostty.init()
  setup(mount, data, ghostty)
}

function setup(mount: HTMLElement, data: DemoStateMap, ghostty: GhosttyModule) {
  const term = new ghostty.Terminal({
    cols: data.cols,
    rows: data.rows,
    cursorBlink: false,
    disableStdin: true,
    fontFamily: '"Noto Sans Mono", "Noto Sans Symbols 2", monospace',
    fontSize: 14,
    theme: {
      background: '#1A1A2E',
      foreground: '#F8F8F2',
      cursor: '#1A1A2E', // hide cursor
      selectionBackground: '#BB86FC40',
    },
  })

  const fallback = document.getElementById('demo-fallback')
  if (fallback) fallback.style.display = 'none'
  mount.style.display = 'block'
  term.open(mount)

  // Adjust container height when terminal is scaled via CSS transform
  function adjustHeight() {
    const canvas = mount.querySelector('canvas') as HTMLElement | null
    if (!canvas) return
    const style = window.getComputedStyle(mount)
    const matrix = new DOMMatrix(style.transform)
    const scale = matrix.a // scaleX from the transform matrix
    if (scale > 0 && scale < 1) {
      const parent = mount.parentElement
      if (parent) parent.style.height = `${mount.offsetHeight * scale}px`
    } else {
      const parent = mount.parentElement
      if (parent) parent.style.height = ''
    }
  }
  adjustHeight()
  window.addEventListener('resize', adjustHeight)

  let currentState = data.initial
  let currentFrame = 0
  let animTimer: ReturnType<typeof setInterval> | null = null
  let idleTimer: ReturnType<typeof setTimeout> | null = null
  let paused = false

  function getFrames(): string[] {
    return data.states[currentState] || []
  }

  function writeFrame(frame: string) {
    // Write each line at its explicit row position to avoid scrolling.
    const lines = frame.split('\n')
    let buf = '\x1b[H' // cursor to row 1, col 1
    for (let i = 0; i < lines.length && i < data.rows; i++) {
      if (i > 0) buf += `\x1b[${i + 1};1H` // move to row i+1, col 1
      buf += '\x1b[2K' // clear entire line
      buf += lines[i]
    }
    term.write(buf)
  }

  function switchState(newState: string) {
    if (!data.states[newState]) return
    currentState = newState
    currentFrame = 0
    const frames = getFrames()
    if (frames.length > 0) {
      writeFrame(frames[0])
    }
    updateTabOverlay()
  }

  function advanceFrame() {
    const frames = getFrames()
    if (frames.length <= 1) return
    currentFrame = (currentFrame + 1) % frames.length
    writeFrame(frames[currentFrame])
  }

  function startAnimation() {
    stopAnimation()
    paused = false
    animTimer = setInterval(advanceFrame, FRAME_INTERVAL)
  }

  function stopAnimation() {
    if (animTimer !== null) {
      clearInterval(animTimer)
      animTimer = null
    }
  }

  function pauseAnimation() {
    paused = true
    stopAnimation()
    if (idleTimer) clearTimeout(idleTimer)
    idleTimer = setTimeout(() => {
      if (paused) startAnimation()
    }, IDLE_RESUME_DELAY)
  }

  // Determine which view is active from the current state key
  function currentViewIndex(): number {
    const base = currentState.split('/')[0]
    return VIEW_NAMES.indexOf(base)
  }

  // Build tab overlay for click/touch interaction
  const overlay = document.getElementById('demo-tab-overlay')
  const tabButtons = overlay?.querySelectorAll<HTMLElement>('[data-demo-tab]')

  function updateTabOverlay() {
    const active = currentViewIndex()
    tabButtons?.forEach((btn, i) => {
      if (i === active) {
        btn.className = 'px-3 py-1.5 border-b-2 whitespace-nowrap transition-colors cursor-pointer border-pink text-pink bg-pink/5'
      } else {
        btn.className = 'px-3 py-1.5 border-b-2 whitespace-nowrap transition-colors cursor-pointer border-transparent text-dim hover:text-muted'
      }
    })
  }

  tabButtons?.forEach((btn, i) => {
    btn.addEventListener('click', () => {
      // Use number key transition to switch to this view's tab
      const key = String(i + 1)
      const target = data.transitions[currentState]?.[key]
      if (target) {
        switchState(target)
      } else if (i === currentViewIndex()) {
        // Already on this view, ignore
      } else {
        // Fallback: try the base view name
        const base = VIEW_NAMES[i]
        if (data.states[base]) switchState(base)
      }
      pauseAnimation()
    })
  })

  // Keyboard handling
  const container = document.getElementById('demo-terminal')
  if (container) {
    container.setAttribute('tabindex', '0')
    container.addEventListener('keydown', (e: KeyboardEvent) => {
      let key = e.key

      // Normalize key names to match our transition table
      if (e.shiftKey && key === 'Tab') key = 'Shift+Tab'

      // Check transition table
      const target = data.transitions[currentState]?.[key]
      if (target) {
        e.preventDefault()
        switchState(target)
        pauseAnimation()
        return
      }

      // Arrow keys for view switching (handled in transitions, but prevent default)
      if (key === 'ArrowLeft' || key === 'ArrowRight') {
        e.preventDefault()
      }
    })

    // Pause on hover
    container.addEventListener('mouseenter', () => pauseAnimation())
    container.addEventListener('mouseleave', () => {
      if (idleTimer) clearTimeout(idleTimer)
      idleTimer = setTimeout(startAnimation, IDLE_RESUME_DELAY)
    })
  }

  // Show initial state and start
  switchState(data.initial)
  startAnimation()
}
