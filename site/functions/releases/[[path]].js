// Pages Function: proxy /releases/* requests to R2 bucket
//
// Binding required: R2 bucket "bewitch-apt" bound as "BUCKET"
// Configure in Cloudflare Pages dashboard → Settings → Functions → R2 bucket bindings

export async function onRequestGet(context) {
  const url = new URL(context.request.url);
  const key = url.pathname.slice(1); // strip leading slash: "releases/bewitch-0.1.0-linux-amd64.tar.gz"

  const object = await context.env.BUCKET.get(key);
  if (!object) {
    return new Response("Not Found", { status: 404 });
  }

  const name = key.split("/").pop();

  const headers = {
    "ETag": object.httpEtag,
  };

  if (name.endsWith(".txt")) {
    headers["Content-Type"] = "text/plain; charset=utf-8";
    headers["Cache-Control"] = "no-cache";
  } else {
    headers["Content-Type"] = "application/gzip";
    headers["Content-Disposition"] = `attachment; filename="${name}"`;
    headers["Cache-Control"] = "public, max-age=86400";
  }

  return new Response(object.body, { headers });
}
