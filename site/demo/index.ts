// Entry for the prebuilt homepage terminal-demo bundle.
// Bundled by esbuild to ../static/js/demo-bundle.js (committed). Rebuild with
// `make site-demo` after changing terminal-demo.ts or demo-frames.json.
import { initTerminalDemo } from './terminal-demo'

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', initTerminalDemo)
} else {
  initTerminalDemo()
}
