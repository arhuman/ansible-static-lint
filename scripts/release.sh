#!/usr/bin/env bash
# release.sh: cut a release with minimal ceremony. Language-agnostic; the only
# project hooks are the quality gate and, where a version manifest exists, the
# file it lives in, so it drops into any standard repo.
#
# Derives the next semantic version from the Conventional Commits made since the
# last v* tag (feat -> minor, fix/other -> patch, ! or BREAKING CHANGE -> major,
# capped to minor while still on 0.x), lets the caller confirm or override it,
# runs the gate (`make ci` when a Makefile exists; customize this line otherwise,
# e.g. `uv run pytest && uv run ruff check`), stamps CHANGELOG.md and, for a
# Python package, the pyproject.toml `version` field (Go has no such manifest:
# its version comes from the tag via ldflags at build time), then commits, tags
# and pushes. Pushing the tag is what triggers .github/workflows/release.yml.
#
# Invoked by the Makefile `release` target (`make release`) when one exists,
# otherwise directly: `scripts/release.sh`.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
changelog="CHANGELOG.md"

die() { echo "release: $*" >&2; exit 1; }

# --- preconditions -----------------------------------------------------------
[ -z "$(git status --porcelain)" ] || die "working tree not clean; commit or stash first"
branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = main ] || [ "$branch" = master ] || die "not on main/master (on $branch)"
[ -f "$changelog" ] || die "$changelog not found"

# --- last tag + bump detection ----------------------------------------------
last=$(git tag --list 'v*' --sort=-v:refname | head -n1)
if [ -z "$last" ]; then
  last="v0.0.0"; range="HEAD"
else
  range="${last}..HEAD"
fi
IFS=. read -r major minor patch <<<"${last#v}"

subjects=$(git log "$range" --no-merges --format='%s')
[ -n "$subjects" ] || die "no commits since $last; nothing to release"
bodies=$(git log "$range" --no-merges --format='%B')

bump=patch
if printf '%s\n' "$subjects" | grep -qE '^[a-z]+(\([^)]+\))?!:' \
   || printf '%s\n' "$bodies" | grep -qE '^BREAKING CHANGE'; then
  bump=major
elif printf '%s\n' "$subjects" | grep -qE '^feat(\([^)]+\))?:'; then
  bump=minor
fi
# SemVer 0.x: a breaking change bumps minor, not major, until the first 1.0.0.
[ "$major" -eq 0 ] && [ "$bump" = major ] && bump=minor

case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac
suggested="v${major}.${minor}.${patch}"

# --- confirm / override ------------------------------------------------------
n_all=$(git rev-list --count --no-merges "$range")
n_feat=$(printf '%s\n' "$subjects" | grep -cE '^feat' || true)
n_fix=$(printf '%s\n' "$subjects" | grep -cE '^fix' || true)
echo "Last tag       : $last"
echo "Commits since  : $n_all ($n_feat feat, $n_fix fix)  ->  bump = $bump"
printf 'Version [%s]: ' "$suggested"
read -r chosen </dev/tty || true
version=${chosen:-$suggested}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid version '$version' (want vMAJOR.MINOR.PATCH)"
git rev-parse "$version" >/dev/null 2>&1 && die "tag $version already exists"

# --- gate --------------------------------------------------------------------
if [ -f Makefile ]; then
  echo "Running make ci ..."
  make ci
else
  echo "release: no Makefile found; customize this gate for your ecosystem" >&2
  die "add this repo's test+lint command here (e.g. 'uv run pytest && uv run ruff check .'), then remove this line"
fi
[ -z "$(git status --porcelain)" ] || die "gate left changes (fmt/tidy?); commit them and re-run"

# --- stamp pyproject.toml version (Python only; Go has no version manifest) --
pyproject="pyproject.toml"
if [ -f "$pyproject" ]; then
  tmp=$(mktemp)
  awk -v ver="${version#v}" '
    !stamped && /^version = "/ { print "version = \"" ver "\""; stamped = 1; next }
    { print }
  ' "$pyproject" > "$tmp" && mv "$tmp" "$pyproject"
  grep -qF "version = \"${version#v}\"" "$pyproject" || die "failed to stamp $pyproject (no 'version = \"...\"' line under [project]?)"
  git add "$pyproject"
fi

# --- stamp CHANGELOG ---------------------------------------------------------
# Promote the [Unreleased] section: keep an empty [Unreleased] on top and open
# a new dated version heading beneath it, over the accumulated changes.
today=$(date +%F)
tmp=$(mktemp)
awk -v ver="${version#v}" -v date="$today" '
  !stamped && /^## \[Unreleased\]/ {
    print "## [Unreleased]"; print "";
    print "## [" ver "] - " date;
    stamped = 1; next
  }
  { print }
' "$changelog" > "$tmp" && mv "$tmp" "$changelog"
grep -qF "## [${version#v}] - $today" "$changelog" || die "failed to stamp $changelog (no '## [Unreleased]' heading?)"

# --- commit, tag, push -------------------------------------------------------
git add "$changelog"
git commit -m "chore(release): $version"
git tag -a "$version" -m "$version"
git push origin "$branch"
git push origin "$version"

echo "Pushed $version. .github/workflows/release.yml is now building."
