#!/usr/bin/env bash
# release-preflight.sh: astl-specific release preconditions, invoked by
# scripts/release.sh before anything is stamped or tagged.
#
# The one condition that a green local gate cannot see: the CI parity job
# clones arhuman/astl-compatibility-check at its remote default branch, while
# `make parity` reads the sibling working copy on disk. A ratchet change that
# only exists locally makes CI red right after the release, so releasing
# requires the sibling to be committed and pushed.
set -euo pipefail

sibling="$(git rev-parse --show-toplevel)/../astl-compatibility-check"

die() { echo "preflight: $*" >&2; exit 1; }

[ -d "$sibling/.git" ] || die "sibling repo not found at $sibling (parity needs it)"
[ -z "$(git -C "$sibling" status --porcelain)" ] \
  || die "sibling has uncommitted changes; commit and push them first"
git -C "$sibling" fetch -q origin main \
  || die "cannot fetch sibling origin/main to compare"
local_main=$(git -C "$sibling" rev-parse main)
remote_main=$(git -C "$sibling" rev-parse origin/main)
[ "$local_main" = "$remote_main" ] \
  || die "sibling main ($local_main) differs from origin/main ($remote_main); push it first, the CI parity job clones the remote"

echo "preflight: sibling corpus clean and pushed ($local_main)"
