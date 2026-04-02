import './global.css'
import { initTerminalDemo } from './components/terminal-demo'

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initTerminalDemo)
} else {
  initTerminalDemo()
}
