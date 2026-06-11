// docs-version.js — namespace awareness for the docs.
//
// Three docs trees share one built HTML (see functions/docs/[[path]].js):
//   /docs/**        the canonical CURRENT STABLE release (served from R2
//                   docs/stable/). Its absolute links already point at /docs/<page>/,
//                   so nothing here needs to run — this script no-ops.
//   /docs/dev/**    the development docs (built from main, relocated by
//                   deploy-site.yml). Keep the dev banner; rewrite in-page nav to
//                   stay under /docs/dev/; ask robots not to index (canonical is
//                   /docs/).
//   /docs/v<X>/**   a frozen per-release snapshot. Hide the dev banner (released
//                   docs); rewrite in-page nav to stay in-version.
//
// Links in the built HTML are absolute ("https://bewitch.dev/docs/<page>/") — but
// getAttribute returns the decoded value, so we match on the "/docs/" substring
// rather than a leading "/docs/", which also repairs in-version nav on snapshots.
(function () {
  var m = window.location.pathname.match(/^\/docs\/(v[0-9][^\/]*|dev)(\/|$)/);
  if (!m) return; // canonical stable /docs/ — links already correct, nothing to do
  var seg = m[1]; // "dev" or "v0.7.0"
  var isDev = seg === "dev";

  var banner = document.getElementById("docs-dev-banner");
  if (banner && !isDev) banner.hidden = true; // snapshots are released docs
  // dev: keep the banner — these ARE the development docs.

  if (isDev) {
    // Discourage indexing of the dev mirror; the canonical copy lives at /docs/.
    var meta = document.createElement("meta");
    meta.name = "robots";
    meta.content = "noindex";
    document.head.appendChild(meta);
  }

  // Keep in-page doc links within this namespace. Skip links already in-namespace,
  // links to another version, and any link explicitly opting out (the banner's
  // pointer to the stable install docs carries data-docs-stable).
  document.querySelectorAll('a[href*="/docs/"]').forEach(function (a) {
    if (a.hasAttribute("data-docs-stable")) return;
    var href = a.getAttribute("href");
    var i = href.indexOf("/docs/");
    if (i === -1) return;
    var tail = href.slice(i + 5); // "/installation/" (leading slash kept)
    if (tail === "/" + seg || tail.indexOf("/" + seg + "/") === 0) return; // already in-namespace
    if (/^\/(v[0-9]|dev\b)/.test(tail)) return; // already a versioned / dev link
    a.setAttribute("href", "/docs/" + seg + tail);
  });
})();
