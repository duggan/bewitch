import { Terminal } from '@xterm/xterm'

interface FrameSet {
  cols: number
  rows: number
  views: Record<string, string[]>
}

const VIEW_NAMES = ['dashboard', 'cpu', 'memory', 'disk', 'network', 'hardware', 'process', 'alerts']
const FRAME_INTERVAL = 800 // ms between frame advances
const IDLE_RESUME_DELAY = 2000 // ms before resuming after interaction

export function initTerminalDemo() {
  const mount = document.getElementById('demo-terminal-mount')
  if (!mount) return

  fetch('/demo-frames.json')
    .then(r => r.json())
    .then((data: FrameSet) => setup(mount, data))
    .catch(() => {
      // Fallback: leave the noscript/PNG content visible
    })
}

function setup(mount: HTMLElement, data: FrameSet) {
  const term = new Terminal({
    cols: data.cols,
    rows: data.rows,
    cursorBlink: false,
    disableStdin: true,
    fontFamily: '"Noto Sans Mono", "Noto Sans Symbols 2", monospace',
    fontSize: 14,
    lineHeight: 1.0,
    theme: {
      background: '#1A1A2E',
      foreground: '#F8F8F2',
      cursor: '#1A1A2E', // hide cursor
      selectionBackground: '#BB86FC40',
    },
  })

  // Hide the fallback PNG content
  const fallback = document.getElementById('demo-fallback')
  if (fallback) fallback.style.display = 'none'

  mount.style.display = 'block'
  term.open(mount)

  // Adjust container height when terminal is scaled via CSS transform
  function adjustHeight() {
    const xtermEl = mount.querySelector('.xterm') as HTMLElement | null
    if (!xtermEl) return
    const style = window.getComputedStyle(xtermEl)
    const matrix = new DOMMatrix(style.transform)
    const scale = matrix.a // scaleX from the transform matrix
    if (scale > 0 && scale < 1) {
      mount.style.height = `${xtermEl.offsetHeight * scale}px`
    } else {
      mount.style.height = ''
    }
  }
  adjustHeight()
  window.addEventListener('resize', adjustHeight)

  let currentView = 0
  let currentFrame = 0
  let animTimer: ReturnType<typeof setInterval> | null = null
  let idleTimer: ReturnType<typeof setTimeout> | null = null
  let paused = false

  function getFrames(): string[] {
    return data.views[VIEW_NAMES[currentView]] || []
  }

  function writeFrame(frame: string) {
    term.reset()
    term.write(frame)
  }

  function showView(idx: number) {
    if (idx < 0 || idx >= VIEW_NAMES.length) return
    currentView = idx
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

  // Build tab overlay for click/touch interaction
  const overlay = document.getElementById('demo-tab-overlay')
  const tabButtons = overlay?.querySelectorAll<HTMLElement>('[data-demo-tab]')

  function updateTabOverlay() {
    tabButtons?.forEach((btn, i) => {
      if (i === currentView) {
        btn.className = 'px-3 py-1.5 border-b-2 whitespace-nowrap transition-colors cursor-pointer border-pink text-pink bg-pink/5'
      } else {
        btn.className = 'px-3 py-1.5 border-b-2 whitespace-nowrap transition-colors cursor-pointer border-transparent text-dim hover:text-muted'
      }
    })
  }

  tabButtons?.forEach((btn, i) => {
    btn.addEventListener('click', () => {
      showView(i)
      pauseAnimation()
    })
  })

  // Keyboard handling — listen on the container so it works when focused
  const container = document.getElementById('demo-terminal')
  if (container) {
    container.setAttribute('tabindex', '0')
    container.addEventListener('keydown', (e: KeyboardEvent) => {
      const key = e.key
      // Number keys 1-8
      if (key >= '1' && key <= '8') {
        e.preventDefault()
        showView(parseInt(key) - 1)
        pauseAnimation()
        return
      }
      // Arrow keys
      if (key === 'ArrowLeft') {
        e.preventDefault()
        showView((currentView - 1 + VIEW_NAMES.length) % VIEW_NAMES.length)
        pauseAnimation()
        return
      }
      if (key === 'ArrowRight') {
        e.preventDefault()
        showView((currentView + 1) % VIEW_NAMES.length)
        pauseAnimation()
        return
      }
    })

    // Pause on hover
    container.addEventListener('mouseenter', () => pauseAnimation())
    container.addEventListener('mouseleave', () => {
      if (idleTimer) clearTimeout(idleTimer)
      idleTimer = setTimeout(startAnimation, IDLE_RESUME_DELAY)
    })
  }

  // Show first view and start
  showView(0)
  startAnimation()
}
