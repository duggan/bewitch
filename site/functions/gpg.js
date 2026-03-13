// Pages Function: proxy /gpg request to R2 bucket
//
// Binding required: R2 bucket "bewitch-apt" bound as "BUCKET"

export async function onRequestGet(context) {
  const object = await context.env.BUCKET.get("gpg");
  if (!object) {
    return new Response("Not Found", { status: 404 });
  }

  return new Response(object.body, {
    headers: {
      "Content-Type": "application/pgp-keys",
      "Cache-Control": "public, max-age=86400",
      "ETag": object.httpEtag,
    },
  });
}
