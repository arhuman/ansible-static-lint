#!/usr/bin/env bash
# release.sh: cut a release with minimal ceremony. Language-agnostic; the only
# project hooks are the quality gate and, where a version manifest exists, the
# file it lives in, so it drops into any standard repo.
#
# Derives the next semantic version from the Conventional Commits made since the
# last v* tag (feat -> minor, fix/other -> patch, ! or BREAKING CHANGE -> major,
# capped to minor while still on 0.x), lets the caller confirm or override it,
# runs the gate (`make ci` when a Makefile exists; customize this line otherwise,
# e.g. `uv run pytest && uv run ruff check`), stamps CHANGELOG.md, the version
# pins in $pin_files (adoption snippets that name the released tag) and, for a
# Python package, the pyproject.toml `version` field (Go has no such manifest:
# its version comes from the tag via ldflags at build time), then commits, tags
# and pushes. Pushing the tag is what triggers .github/workflows/release.yml.
# An executable scripts/release-preflight.sh, when present, runs with the
# preconditions and can veto the release with project-specific checks.
#
# Invoked by the Makefile `release` target (`make release`) when one exists,
# otherwise directly: `scripts/release.sh`.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
changelog="CHANGELOG.md"
# Files whose adoption snippets pin the released version (`vX.Y.Z` and
# `VERSION=X.Y.Z` spellings). Stamped from the previous tag to the new one in
# the release commit; missing files are skipped. Keep the changelog out: its
# past version headings are history, not pins.
pin_files=(README.md .pre-commit-hooks.yaml action.yml docs/ci.md)

die() { echo "release: $*" >&2; exit 1; }

# --- preconditions -----------------------------------------------------------
[ -z "$(git status --porcelain)" ] || die "working tree not clean; commit or stash first"
branch=$(git rev-parse --abbrev-ref HEAD)
[ "$branch" = main ] || [ "$branch" = master ] || die "not on main/master (on $branch)"
[ -f "$changelog" ] || die "$changelog not found"
# Project-specific preconditions live in an optional hook, keeping this script
# generic. astl's checks that the compatibility corpus the CI parity job
# clones is pushed, since a green local gate cannot see a stale remote.
if [ -x scripts/release-preflight.sh ]; then
  ./scripts/release-preflight.sh || die "preflight failed"
fi

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

bump="patch"
if printf '%s\n' "$subjects" | grep -qE '^[a-z]+(\([^)]+\))?!:' \
   || printf '%s\n' "$bodies" | grep -qE '^BREAKING CHANGE'; then
  bump="major"
elif printf '%s\n' "$subjects" | grep -qE '^feat(\([^)]+\))?:'; then
  bump="minor"
fi
# SemVer 0.x: a breaking change bumps minor, not major, until the first 1.0.0.
[ "$major" -eq 0 ] && [ "$bump" = major ] && bump="minor"

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

# --- stamp version pins ------------------------------------------------------
# Rewrites this project's own version pins in $pin_files to the new version,
# in both the `vX.Y.Z` and the bare `VERSION=X.Y.Z` spellings.
#
# Matching is on the pin's shape, not on the previous tag: keying off $last
# rewrote only pins that were exactly one release behind, so any pin that fell
# further behind was skipped at every subsequent release and stayed frozen.
# docs/ci.md sat at v0.2.0 through four releases that way. The shapes below are
# anchored to this repository's own coordinates, so a version-like string
# belonging to anything else (actions/checkout@v7, a Go version, prose) is not
# a candidate.
stamp_pins() {
  local f tmp
  for f in "${pin_files[@]}"; do
    [ -f "$f" ] || continue
    tmp=$(mktemp)
    # `|` as the delimiter, not `#`: a `#` appears inside the commented-out
    # `rev:` pattern below and would close the expression early.
    sed -E \
      -e "s|(ansible-static-lint@)v[0-9]+\.[0-9]+\.[0-9]+|\1${version}|g" \
      -e "s|(arhuman/ansible-static-lint/releases/download/)v[0-9]+\.[0-9]+\.[0-9]+|\1${version}|g" \
      -e "s|^([[:space:]]*(#[[:space:]]*)?rev:[[:space:]]*)v[0-9]+\.[0-9]+\.[0-9]+|\1${version}|g" \
      -e "s|VERSION=[0-9]+\.[0-9]+\.[0-9]+|VERSION=${version#v}|g" \
      -e "s|v[0-9]+\.[0-9]+\.[0-9]+([[:space:]]*# release-pin)|${version}\1|g" \
      -e "s|(\\\$TAG = \")v[0-9]+\.[0-9]+\.[0-9]+|\1${version}|g" \
      "$f" > "$tmp" && mv "$tmp" "$f"
    git add "$f"
  done
}

# A pin left at an older version is invisible in a release log, so the stamping
# is verified rather than trusted: any remaining pin at a version that is not
# the one being released vetoes the release.
#
# The audit covers the shapes stamp_pins knows, which is what guards against a
# pin falling behind. It does not discover a new shape: add one here and in
# stamp_pins together, or end the pinned line with `# release-pin`, which both
# know. A PyPI pin (`ansible-static-lint==X.Y.Z`) is the likely next shape; the
# README deliberately leaves those installs unpinned today.
verify_pins() {
  local f line stale=0
  for f in "${pin_files[@]}"; do
    [ -f "$f" ] || continue
    while IFS= read -r line; do
      stale=1
      echo "release: stale pin in $f: $line" >&2
    done < <(grep -nE "(ansible-static-lint@|releases/download/|^[[:space:]]*#?[[:space:]]*rev:[[:space:]]*)v[0-9]+\.[0-9]+\.[0-9]+|VERSION=[0-9]+\.[0-9]+\.[0-9]+|v[0-9]+\.[0-9]+\.[0-9]+[[:space:]]*# release-pin|\\\$TAG = \"v[0-9]+\.[0-9]+\.[0-9]+" "$f" \
             | grep -vE "(@|/|rev:[[:space:]]*)${version//./\\.}([^0-9]|\$)" \
             | grep -vE "VERSION=${version#v}([^0-9]|\$)" \
             | grep -vE "${version//./\\.}[[:space:]]*# release-pin" \
             | grep -vE "\\\$TAG = \"${version//./\\.}\"")
  done
  [ "$stale" -eq 0 ] || die "version pins not fully stamped to $version"
}

stamp_pins
verify_pins

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
