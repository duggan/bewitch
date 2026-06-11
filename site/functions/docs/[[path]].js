// Pages Function: route the /docs/ namespace across three sources.
//
//   /docs/dev/**       → context.next(): the static Pages deploy (built from main),
//                        relocated under /docs/dev/ by deploy-site.yml. The
//                        bleeding-edge development docs (dev banner shown).
//   /docs/v<X.Y.Z>/**  → R2 object docs/v<X.Y.Z>/…  : frozen per-release snapshots
//                        (release.yml uploads the built tree on each tag).
//   /docs/** (anything → R2 object docs/stable/…    : the CURRENT STABLE release,
//   else, incl. root)    mirrored to docs/stable/ on each release. This is the
//                        canonical /docs/ ordinary users see; URLs stay clean (no
//                        redirect) and the stable build's own absolute links already
//                        point at /docs/<page>/, so navigation is correct with no
//                        rewriting.
//
// Only the production host (bewitch.dev) is routed through R2 — on *.pages.dev PR
// previews the Function steps aside so the preview's own static docs are served
// (otherwise a docs PR could never be previewed; it'd show production stable).
//
// Binding required: R2 bucket "bewitch-apt" bound as "BUCKET".
//
// Zola emits clean-URL pages as <slug>/index.html, so a request for /docs/foo/ must
// resolve to the …/foo/index.html object — hence the candidate-key fallbacks.

async function serveFromR2(bucket, key) {
  const noTrailing = key.replace(/\/$/, "");
  const candidates = [
    key, // exact object
    key + ".html", // clean-url page stored without a directory
    noTrailing + "/index.html", // Zola clean-url directory index
  ];
  for (const k of candidates) {
    const object = await bucket.get(k);
    if (object) {
      return new Response(object.body, {
        headers: {
          "Content-Type": "text/html; charset=utf-8",
          "Cache-Control": "public, max-age=86400",
          "ETag": object.httpEtag,
        },
      });
    }
  }
  return null;
}

export async function onRequestGet(context) {
  const url = new URL(context.request.url);
  const path = url.pathname;

  // PR previews / non-production hosts: serve the static deploy as-is.
  if (!url.hostname.endsWith("bewitch.dev")) {
    return context.next();
  }

  // Development docs: served statically by the Pages deploy under /docs/dev/.
  if (path === "/docs/dev" || path.startsWith("/docs/dev/")) {
    return context.next();
  }

  // Frozen per-release snapshots: /docs/v0.7.0/…  → R2 docs/v0.7.0/…
  if (/^\/docs\/v\d/.test(path)) {
    const resp = await serveFromR2(context.env.BUCKET, path.slice(1));
    return resp || new Response("Not Found", { status: 404 });
  }

  // Canonical /docs/** → the current stable release, mirrored to R2 docs/stable/.
  const within = path.replace(/^\/docs\/?/, ""); // "" or "installation/"
  const resp = await serveFromR2(context.env.BUCKET, "docs/stable/" + within);
  if (resp) return resp;

  // Bootstrap fallback (no stable snapshot uploaded yet): show the dev docs.
  return Response.redirect(new URL("/docs/dev/" + within, url).toString(), 302);
}
