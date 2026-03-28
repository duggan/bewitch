// Pages Function: serve versioned docs from R2 bucket
//
// Handles /docs/v*/** requests by fetching from R2.
// Redirects /docs (bare) to the latest stable version.
// All other /docs/* requests fall through to static assets (dev docs).
//
// Binding required: R2 bucket "bewitch-apt" bound as "BUCKET"

// Update this on each release to point /docs to the latest stable version.
const LATEST_STABLE = 'v0.3.1';

export async function onRequestGet(context) {
  const url = new URL(context.request.url);
  const path = url.pathname;

  // Redirect /docs and /docs/ to latest stable version
  if (path === '/docs' || path === '/docs/') {
    return Response.redirect(new URL(`/docs/${LATEST_STABLE}`, url.origin).toString(), 302);
  }

  // Only handle versioned docs: /docs/v0.2.0/..., /docs/v1.0.0/...
  if (!/^\/docs\/v\d/.test(path)) {
    return context.next();
  }

  // Strip leading slash for R2 key: "docs/v0.2.0/installation"
  const basePath = path.slice(1);

  // Try with .html suffix first (clean URLs), then exact path
  const object =
    (await context.env.BUCKET.get(basePath + ".html")) ||
    (await context.env.BUCKET.get(basePath));

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
