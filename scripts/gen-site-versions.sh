#!/bin/sh
# gen-site-versions.sh — write site/data/versions.json from the root VERSION and
# LATEST_STABLE files, so the docs version switcher/banner is driven by those two
# (already bumped during a release) rather than a hand-maintained list.
#
# Run by `make site`/`make site-serve` and by the deploy/release workflows before
# `zola build`. A current copy is committed too, so a bare `zola build` still works.

set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEV="$(cat "$ROOT/VERSION")"
STABLE="$(cat "$ROOT/LATEST_STABLE")"

cat > "$ROOT/site/data/versions.json" <<EOF
{
  "dev": "$DEV",
  "stable": "$STABLE"
}
EOF

echo "site/data/versions.json: dev=$DEV stable=$STABLE"
