// docs-version.js — version awareness for the docs.
//
// The default deploy (built from `main`) serves docs at /docs/<page>/ and is the
// bleeding-edge "dev" docs, marked by the dev banner. Tagged releases snapshot the
// same HTML into R2, served at /docs/v<X.Y.Z>/<page>/ by functions/docs/[[path]].js.
// Because both are the same built HTML, this adapts behaviour from the URL: on a
// versioned snapshot it hides the dev banner (snapshots are released docs) and
// rewrites in-page /docs/ links so navigation stays in-version.
//
// (A version switcher can be layered on later once snapshots have accrued; the
// banner + per-version URLs work without it.)
(function () {
  var m = window.location.pathname.match(/^\/docs\/(v[0-9][^\/]*)(\/|$)/);
  var version = m ? m[1] : ""; // "" = latest (dev)
  if (!version) return;

  // On a snapshot: drop the dev banner (these are released docs).
  var banner = document.getElementById("docs-dev-banner");
  if (banner) banner.hidden = true;

  // Keep in-page doc links within this version — the snapshot HTML ships with
  // absolute /docs/ links pointing at latest.
  document.querySelectorAll('a[href^="/docs/"]').forEach(function (a) {
    var href = a.getAttribute("href");
    if (/^\/docs\/v[0-9]/.test(href)) return; // already versioned
    a.setAttribute("href", href.replace(/^\/docs/, "/docs/" + version));
  });
})();
