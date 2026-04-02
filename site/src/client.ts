import './global.css'
import '@xterm/xterm/css/xterm.css'
import { initTerminalDemo } from './components/terminal-demo'

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initTerminalDemo)
} else {
  initTerminalDemo()
}
