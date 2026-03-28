#!/bin/sh
# gen-changelog.sh — generate debian/changelog from VERSION file
#
# Uses VERSION as the package version and generates a minimal changelog
# entry. This ensures the .deb version always matches VERSION without
# requiring manual changelog maintenance.
#
# Usage: scripts/gen-changelog.sh

set -eu

VERSION="${BEWITCH_VERSION:-$(cat VERSION)}"
MAINTAINER="Ross Duggan <ross@bewitch.net>"
DATE=$(date -R)

cat > debian/changelog <<EOF
bewitch (${VERSION}-1) unstable; urgency=medium

  * Release ${VERSION}

 -- ${MAINTAINER}  ${DATE}
EOF
