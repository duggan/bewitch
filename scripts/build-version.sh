#!/bin/sh
# Prints the package version for a non-release build: scripts/build-version.sh <label>
# e.g. "dev.20261028171314.7dcf115" -> 0.11.0~dev.20261028171314.7dcf115
#
# Normally VERSION is the next, unreleased version (bumped right after each
# release), so "~" makes the build sort before that release. If VERSION still
# equals LATEST_STABLE (the bump was missed), the build comes after the release,
# so it uses "+" instead: "0.10.1~dev..." sorts below the stable 0.10.1-1, and apt
# then won't upgrade dev-channel hosts or the VM test's stable install to it.
set -eu
label=$1
root=$(dirname "$0")/..
version=$(cat "$root/VERSION")
stable=$(cat "$root/LATEST_STABLE")
if [ "$version" = "$stable" ]; then
  echo "${version}+${label}"
else
  echo "${version}~${label}"
fi
