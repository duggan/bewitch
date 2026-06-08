// Pages Function: serve versioned docs snapshots from the R2 bucket.
//
// Handles /docs/v*/** requests by fetching from R2 (release.yml uploads the
// built Zola docs tree verbatim under docs/v<X.Y.Z>/ on each tag). All other
// /docs/* requests fall through to static assets (the latest, main-built docs).
//
// Binding required: R2 bucket "bewitch-apt" bound as "BUCKET".
//
// Zola emits clean-URL pages as <slug>/index.html, so a request for
// /docs/v0.7.0/collectors/ must resolve to the docs/v0.7.0/collectors/index.html
// object — hence the candidate-key fallbacks below.

export async function onRequestGet(context) {
  const url = new URL(context.request.url);
  const path = url.pathname;

  // Only handle versioned docs: /docs/v0.2.0/..., /docs/v1.0.0/...
  if (!/^\/docs\/v\d/.test(path)) {
    return context.next();
  }

  // Strip leading slash for R2 key: "docs/v0.7.0/collectors/"
  const basePath = path.slice(1);
  const noTrailing = basePath.replace(/\/$/, "");

  const candidates = [
    basePath, // exact object
    basePath + ".html", // clean-url page stored without a directory
    noTrailing + "/index.html", // Zola clean-url directory index
  ];

  let object = null;
  for (const key of candidates) {
    object = await context.env.BUCKET.get(key);
    if (object) break;
  }

  if (!object) {
    return new Response("Not Found", { status: 404 });
  }

  return new Response(object.body, {
    headers: {
      "Content-Type": "text/html; charset=utf-8",
      "Cache-Control": "public, max-age=86400",
      "ETag": object.httpEtag,
    },
  });
}
