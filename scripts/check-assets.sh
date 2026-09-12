#!/usr/bin/env bash
# Asset measurements for engineering awareness, never product-size ceilings.
# Missing assets still fail; larger assets do not. Review waste and duplication
# alongside browser loading/rendering measurements (docs/product/ui-ux.md).
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
measure() { # name files...
  local name="$1"; shift
  local total=0 raw=0
  for f in "$@"; do
    [ -f "$f" ] || { echo "MISSING asset: $f"; fail=1; return; }
    local sz
    sz=$(gzip -n -c "$f" | wc -c)
    total=$((total + sz))
    raw=$((raw + $(wc -c < "$f")))
  done
  echo "$name: ${raw} B raw, ${total} B gzip, $# files"
}

measure "javascript total" web/static/js/*.js
measure "css total" web/static/css/*.css
measure "fonts total" web/static/fonts/*.woff2
mapfile -d '' svg_files < <(find web/static -type f -name '*.svg' -print0)
if [ "${#svg_files[@]}" -gt 0 ]; then
  measure "SVG total" "${svg_files[@]}"
fi

exit $fail
