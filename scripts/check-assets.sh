#!/usr/bin/env bash
# Asset budget enforcement (docs/product/ui-ux.md §Performance budgets).
# Runs in CI and locally: gzip every committed frontend asset and fail if a
# budget is exceeded. Budgets are gzip sizes in bytes.
set -euo pipefail
cd "$(dirname "$0")/.."

budget_js=30720        # 30 KB — JavaScript total (HTMX + app)
budget_css=25600       # 25 KB — CSS total
budget_fonts=153600    # 150 KB — fonts total
budget_html=61440      # 60 KB — typical list-page HTML (checked on rendered pages in QA)

fail=0
check() { # name budget files...
  local name="$1" budget="$2"; shift 2
  local total=0
  for f in "$@"; do
    [ -f "$f" ] || { echo "MISSING asset: $f"; fail=1; return; }
    local sz
    sz=$(gzip -c "$f" | wc -c)
    total=$((total + sz))
  done
  local kb=$((total / 1024)) cap=$((budget / 1024))
  if [ "$total" -gt "$budget" ]; then
    echo "OVER BUDGET: $name ${total}B gz > ${budget}B (${kb} KiB > ${cap} KiB)"
    fail=1
  else
    echo "ok: $name ${total}B gz / ${budget}B (${kb}/${cap} KiB)"
  fi
}

check "javascript total" "$budget_js" web/static/js/*.js
check "css total"        "$budget_css" web/static/css/*.css
check "fonts total"      "$budget_fonts" web/static/fonts/*.woff2

exit $fail
