// Pages Function: proxy /apt/dists/* requests to R2 bucket
//
// Binding required: R2 bucket "bewitch-apt" bound as "BUCKET"

const CONTENT_TYPES = {
  "Packages": "text/plain; charset=utf-8",
  "Packages.gz": "application/gzip",
  "Release": "text/plain; charset=utf-8",
  "Release.gpg": "application/pgp-signature",
  "InRelease": "text/plain; charset=utf-8",
};

export async function onRequestGet(context) {
  const url = new URL(context.request.url);
  const key = url.pathname.slice(1); // strip leading slash: "apt/dists/stable/..."

  const object = await context.env.BUCKET.get(key);
  if (!object) {
    return new Response("Not Found", { status: 404 });
  }

  const filename = key.split("/").pop();
  const contentType = CONTENT_TYPES[filename] || "application/octet-stream";

  return new Response(object.body, {
    headers: {
      "Content-Type": contentType,
      "Cache-Control": "no-cache",
      "ETag": object.httpEtag,
    },
  });
}
