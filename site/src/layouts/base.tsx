import type { FC, PropsWithChildren } from 'hono/jsx'

export const Base: FC<PropsWithChildren<{ title?: string; description?: string }>> = ({
  title = 'bewitch — a charming system monitor for Linux',
  description = 'Beautiful TUI system monitoring with DuckDB storage, multi-channel alerting, interactive SQL REPL, and remote access with TLS.',
  children,
}) => (
  <html lang="en">
    <head>
      <meta charset="utf-8" />
      <meta name="viewport" content="width=device-width, initial-scale=1" />
      <title>{title}</title>
      <meta name="description" content={description} />
      <meta property="og:title" content={title} />
      <meta property="og:description" content={description} />
      <meta property="og:type" content="website" />
      <meta property="og:url" content="https://bewitch.dev" />
      <meta property="og:image" content="https://bewitch.dev/og.png" />
      <meta name="twitter:card" content="summary_large_image" />
      <link rel="icon" href="/favicon.png" type="image/png" />
      <link rel="preconnect" href="https://fonts.bunny.net" />
      <link rel="stylesheet" href="https://fonts.bunny.net/css?family=jetbrains-mono:400,500,600,700&display=swap" />
      <link rel="stylesheet" href="/src/global.css" />
    </head>
    <body class="min-h-screen bg-body-bg text-text antialiased">
      {children}
      <script dangerouslySetInnerHTML={{
        __html: `
document.addEventListener('click', function(e) {
  var btn = e.target.closest('.copy-btn');
  if (!btn) return;
  var text = btn.dataset.copy;
  if (!text) return;
  navigator.clipboard.writeText(text).then(function() {
    var orig = btn.textContent;
    btn.textContent = 'copied!';
    btn.classList.add('text-pink');
    setTimeout(function() { btn.textContent = orig; btn.classList.remove('text-pink'); }, 1500);
  });
});

(function() {
  var tabs = document.getElementById('demo-tabs');
  var slides = document.getElementById('demo-slides');
  if (!tabs || !slides) return;
  var btns = tabs.querySelectorAll('button[data-slide]');
  var imgs = slides.querySelectorAll('img[data-slide-img]');
  var current = 0;
  var timer;
  function show(n) {
    imgs[current].classList.remove('opacity-100');
    imgs[current].classList.add('opacity-0', 'absolute', 'inset-0');
    btns[current].className = 'px-3 py-1.5 border-b-2 whitespace-nowrap transition-colors cursor-pointer border-transparent text-dim hover:text-muted';
    current = n;
    imgs[current].classList.remove('opacity-0', 'absolute', 'inset-0');
    imgs[current].classList.add('opacity-100');
    btns[current].className = 'px-3 py-1.5 border-b-2 whitespace-nowrap transition-colors cursor-pointer border-pink text-pink bg-pink/5';
  }
  function advance() { show((current + 1) % imgs.length); }
  function startTimer() { timer = setInterval(advance, 3000); }
  function resetTimer() { clearInterval(timer); startTimer(); }
  btns.forEach(function(btn, i) {
    btn.addEventListener('click', function() { show(i); resetTimer(); });
  });
  startTimer();
})();
`
      }} />
    </body>
  </html>
)
