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
      <link rel="stylesheet" href="https://fonts.bunny.net/css?family=noto-sans-mono:400,500,600,700|noto-sans-symbols-2:400&display=swap" />
      <link rel="stylesheet" href="/src/global.css" />
      <script type="module" src="/src/client.ts"></script>
    </head>
    <body class="min-h-screen bg-body-bg text-text antialiased">
      {children}
      <script dangerouslySetInnerHTML={{
        __html: `
document.addEventListener('click', function(e) {
  var btn = e.target.closest('.copy-btn');
  if (!btn) return;
  var text = btn.dataset.copy;
  if (!text) { var code = btn.closest('div').parentElement.querySelector('code'); if (code) text = code.innerText; }
  if (!text) return;
  navigator.clipboard.writeText(text).then(function() {
    var orig = btn.textContent;
    btn.textContent = 'copied!';
    btn.classList.add('text-pink');
    setTimeout(function() { btn.textContent = orig; btn.classList.remove('text-pink'); }, 1500);
  });
});

document.addEventListener('click', function(e) {
  var toggle = e.target.closest('[data-version-toggle]');
  var wrap = toggle && toggle.closest('[id$="-wrap"]');
  var dropdowns = document.querySelectorAll('#version-dropdown, #version-dropdown-mobile');
  if (toggle && wrap) {
    var dd = wrap.querySelector('[id^="version-dropdown"]');
    dropdowns.forEach(function(d) { if (d !== dd) d.classList.add('hidden'); });
    if (dd) dd.classList.toggle('hidden');
    return;
  }
  if (!e.target.closest('#version-dropdown') && !e.target.closest('#version-dropdown-mobile')) {
    dropdowns.forEach(function(d) { d.classList.add('hidden'); });
  }
});

`
      }} />
    </body>
  </html>
)
