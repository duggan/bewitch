// Client-side docs search. Reads the Zola-generated elasticlunr index
// (window.searchIndex) and renders a live dropdown of title + matching snippet.
// Progressive enhancement: if anything is missing, the box just does nothing.
(function () {
  "use strict";

  // The index bakes absolute permalinks using the production base_url, which
  // point at the deployed host even on localhost. Derive the real base this page
  // is served from via our own <script src> (Zola resolves it per environment)
  // and re-home each result link onto it.
  var thisScript = document.currentScript;
  var siteBase = thisScript
    ? thisScript.src.replace(/\/js\/search\.js(?:\?.*)?$/, "")
    : "";
  // The index only ever holds canonical /docs/<page>/ refs (built once). When this
  // page is served under a docs namespace (/docs/dev/ or /docs/v<X.Y.Z>/), keep
  // results within it — mirrors docs-version.js so search doesn't bounce the reader
  // out to stable. Stable pages (no namespace) re-home unchanged.
  var nsMatch = window.location.pathname.match(/^\/docs\/(v[0-9][^\/]*|dev)(\/|$)/);
  var nsSeg = nsMatch ? nsMatch[1] : "";
  function localHref(ref) {
    var at = ref.indexOf("/docs/");
    if (at === -1) return ref;
    var path = ref.slice(at); // "/docs/installation/"
    if (nsSeg) path = "/docs/" + nsSeg + path.slice(5); // "/docs/dev" + "/installation/"
    return siteBase + path;
  }

  var input = document.getElementById("docs-search");
  var list = document.getElementById("docs-search-results");
  if (!input || !list || typeof elasticlunr === "undefined" || !window.searchIndex) {
    return;
  }

  var index = elasticlunr.Index.load(window.searchIndex);
  var MAX_RESULTS = 8;
  var SNIPPET_LEN = 140;
  var active = -1; // highlighted result row, -1 = none

  function escapeHtml(s) {
    return s.replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }

  // A ~SNIPPET_LEN window of body text centred on the first query-term hit.
  function snippet(body, terms) {
    if (!body) return "";
    var lower = body.toLowerCase();
    var at = -1;
    for (var i = 0; i < terms.length; i++) {
      var p = lower.indexOf(terms[i]);
      if (p !== -1 && (at === -1 || p < at)) at = p;
    }
    if (at === -1) at = 0;
    var start = Math.max(0, at - Math.floor(SNIPPET_LEN / 3));
    var text = body.slice(start, start + SNIPPET_LEN).trim();
    return (start > 0 ? "…" : "") + escapeHtml(text) + "…";
  }

  function close() {
    list.hidden = true;
    list.innerHTML = "";
    active = -1;
  }

  function render(query) {
    var q = query.trim();
    if (!q) { close(); return; }

    var terms = q.toLowerCase().split(/\s+/);
    // Only surface docs pages; the landing page (root section) is indexed by Zola
    // but has no body and would only ever match on its title.
    var hits = index.search(q, { bool: "AND", expand: true })
      .filter(function (h) { return h.ref.indexOf("/docs/") !== -1; })
      .slice(0, MAX_RESULTS);
    if (!hits.length) {
      list.innerHTML = '<li class="search-empty">No matches</li>';
      list.hidden = false;
      active = -1;
      return;
    }

    list.innerHTML = hits.map(function (hit) {
      var doc = index.documentStore.getDoc(hit.ref) || {};
      return '<li><a href="' + localHref(hit.ref) + '">' +
        "<strong>" + escapeHtml(doc.title || hit.ref) + "</strong>" +
        '<span class="snip">' + snippet(doc.body || "", terms) + "</span>" +
        "</a></li>";
    }).join("");
    list.hidden = false;
    active = -1;
  }

  function rows() {
    return list.querySelectorAll("li > a");
  }

  function highlight(next) {
    var items = rows();
    if (!items.length) return;
    if (active >= 0 && items[active]) items[active].classList.remove("active");
    active = (next + items.length) % items.length;
    items[active].classList.add("active");
    items[active].scrollIntoView({ block: "nearest" });
  }

  input.addEventListener("input", function () { render(input.value); });

  input.addEventListener("keydown", function (e) {
    if (list.hidden) return;
    if (e.key === "ArrowDown") { e.preventDefault(); highlight(active + 1); }
    else if (e.key === "ArrowUp") { e.preventDefault(); highlight(active - 1); }
    else if (e.key === "Enter") {
      var items = rows();
      if (active >= 0 && items[active]) { e.preventDefault(); window.location.href = items[active].href; }
    } else if (e.key === "Escape") { close(); input.blur(); }
  });

  // Close when focus/click leaves the search widget.
  document.addEventListener("click", function (e) {
    if (!e.target.closest(".docs-search")) close();
  });
})();
