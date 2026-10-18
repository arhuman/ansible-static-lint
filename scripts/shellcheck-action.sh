#!/usr/bin/env bash
# Lints the bash steps of action.yml with shellcheck.
#
# actionlint shellchecks workflow steps but not a composite action's, and these
# run in every consumer's CI. Each `run: |` block is extracted, dedented and
# checked as a bash script. A run step in any other shape (inline, folded)
# fails the check rather than being skipped, so the gate cannot go hollow.
set -euo pipefail

action=${1:-action.yml}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

awk -v out="$tmp" '
  function indent(s) { match(s, /^ */); return RLENGTH }
  in_block && /[^ ]/ && indent($0) <= key { in_block = 0; close(file) }
  in_block {
    if (cut < 0 && /[^ ]/) cut = indent($0)
    print (cut < 0 ? "" : substr($0, cut + 1)) > file
    next
  }
  /^ *(- )?run: \|/ {
    n++; key = indent($0); cut = -1; in_block = 1
    file = sprintf("%s/step%d.sh", out, n)
    print "#!/usr/bin/env bash" > file
  }
' "$action"

steps=$(grep -cE '^ *(- )?run:' "$action" || true)
blocks=$(find "$tmp" -name 'step*.sh' | wc -l | tr -d ' ')
if [ "$blocks" -eq 0 ] || [ "$blocks" -ne "$steps" ]; then
  echo "shellcheck-action: extracted $blocks of $steps run steps from $action" >&2
  exit 1
fi
shellcheck "$tmp"/step*.sh
echo "OK: $blocks run steps of $action pass shellcheck"
