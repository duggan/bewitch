// Pages Function: proxy /apt/pool/* requests to R2 bucket
//
// Binding required: R2 bucket "bewitch-apt" bound as "BUCKET"
// Configure in Cloudflare Pages dashboard → Settings → Functions → R2 bucket bindings

export async function onRequestGet(context) {
  const url = new URL(context.request.url);
  const key = decodeURIComponent(url.pathname.slice(1)); // strip leading slash and decode: "apt/pool/main/b/bewitch/..."

  const object = await context.env.BUCKET.get(key);
  if (!object) {
    return new Response("Not Found", { status: 404 });
  }

  return new Response(object.body, {
    headers: {
      "Content-Type": "application/vnd.debian.binary-package",
      "Cache-Control": "public, max-age=86400",
      "ETag": object.httpEtag,
    },
  });
}
