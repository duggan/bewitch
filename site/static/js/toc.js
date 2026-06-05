// Scroll-spy for the right-hand "On this page" TOC. Highlights the link whose
// heading is currently the topmost one in (or just above) the viewport.
// Progressive enhancement: if there's no .toc or no IntersectionObserver, it
// does nothing and the TOC stays a plain list of anchor links.
(function () {
  "use strict";

  var toc = document.querySelector(".toc");
  if (!toc || !("IntersectionObserver" in window)) return;

  // Map each heading element -> its TOC link, in document order.
  var links = Array.prototype.slice.call(toc.querySelectorAll('a[href^="#"]'));
  var entries = [];
  links.forEach(function (link) {
    var id = decodeURIComponent(link.getAttribute("href").slice(1));
    var heading = document.getElementById(id);
    if (heading) entries.push({ heading: heading, link: link });
  });
  if (!entries.length) return;

  var visible = {}; // id -> true while its heading is intersecting

  function update() {
    // The active heading is the last one at or above the top of the viewport;
    // if any are intersecting, prefer the first intersecting one.
    var activeIdx = -1;
    for (var i = 0; i < entries.length; i++) {
      if (visible[entries[i].heading.id]) { activeIdx = i; break; }
    }
    if (activeIdx === -1) {
      // None intersecting (e.g. mid-section): pick the last heading scrolled past.
      for (var j = 0; j < entries.length; j++) {
        if (entries[j].heading.getBoundingClientRect().top <= 80) activeIdx = j;
        else break;
      }
    }
    entries.forEach(function (e, idx) {
      e.link.classList.toggle("active", idx === activeIdx);
    });
  }

  var observer = new IntersectionObserver(function (obs) {
    obs.forEach(function (o) {
      visible[o.target.id] = o.isIntersecting;
    });
    update();
  }, {
    // Trigger as a heading crosses the upper portion of the viewport.
    rootMargin: "-70px 0px -70% 0px",
    threshold: 0,
  });

  entries.forEach(function (e) { observer.observe(e.heading); });
  update();
})();
