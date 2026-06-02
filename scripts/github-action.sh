#!/usr/bin/env bash
set -euo pipefail

marker='<!-- aib-report -->'
output_dir=${AIB_INPUT_OUTPUT_DIR:-.aib}
db_path="${output_dir}/aib.db"
md_report="${output_dir}/aib-report.md"
json_report="${output_dir}/aib-report.json"

mkdir -p "$output_dir"

split_values() {
  python3 - "$@" <<'PY'
import os, sys
value = sys.argv[1]
for part in value.replace(',', '\n').splitlines():
    part = part.strip()
    if part:
        print(part)
PY
}

mapfile -t paths < <(split_values "${AIB_INPUT_PATHS:-.}")
mapfile -t sources < <(split_values "${AIB_INPUT_SOURCES:-auto}")

if [ "${#paths[@]}" -eq 0 ]; then
  paths=(.)
fi
if [ "${#sources[@]}" -eq 0 ]; then
  sources=(auto)
fi

printf 'AIB version: '
aib version || true

for source in "${sources[@]}"; do
  case "$source" in
    auto)
      aib --db "$db_path" scan auto "${paths[@]}"
      ;;
    terraform|terraform-plan|kubernetes|k8s|compose|cloudformation|pulumi|ansible)
      aib --db "$db_path" scan "$source" "${paths[@]}"
      ;;
    *)
      echo "Unsupported AIB source: $source" >&2
      exit 2
      ;;
  esac
done

aib --db "$db_path" report --format markdown --out "$md_report"
aib --db "$db_path" report --format json --out "$json_report"

stats=$(python3 - "$json_report" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    data = json.load(f)
summary = data.get('audit', {}).get('summary', {})
print(summary.get('total', 0))
print(summary.get('critical', 0))
print(summary.get('warning', 0))
print(summary.get('info', 0))
print(data.get('total_nodes', 0))
print(data.get('total_edges', 0))
PY
)
mapfile -t stat_lines <<< "$stats"
findings=${stat_lines[0]:-0}
critical=${stat_lines[1]:-0}
warning=${stat_lines[2]:-0}
info=${stat_lines[3]:-0}
nodes=${stat_lines[4]:-0}
edges=${stat_lines[5]:-0}

{
  echo "findings-count=$findings"
  echo "critical-count=$critical"
  echo "warning-count=$warning"
  echo "info-count=$info"
  echo "nodes-count=$nodes"
  echo "edges-count=$edges"
  echo "markdown-report-path=$md_report"
  echo "json-report-path=$json_report"
} >> "$GITHUB_OUTPUT"

if [ "${GITHUB_STEP_SUMMARY:-}" ]; then
  cat "$md_report" >> "$GITHUB_STEP_SUMMARY"
fi

is_pr_event() {
  [ -n "${GITHUB_EVENT_PATH:-}" ] && python3 - "$GITHUB_EVENT_PATH" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    event = json.load(f)
print('yes' if event.get('pull_request') else 'no')
PY
}

comment_pr=${AIB_INPUT_COMMENT_PR:-true}
if [ "$comment_pr" = "true" ] && [ "$(is_pr_event)" = "yes" ] && command -v gh >/dev/null 2>&1; then
  python3 - "$GITHUB_EVENT_PATH" "$md_report" "$marker" <<'PY' > "${output_dir}/comment.md"
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    event = json.load(f)
with open(sys.argv[2], encoding='utf-8') as f:
    report = f.read()
print(sys.argv[3])
print(report)
PY
  repo=${GITHUB_REPOSITORY:?}
  issue=$(python3 - "$GITHUB_EVENT_PATH" <<'PY'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    event = json.load(f)
print(event['pull_request']['number'])
PY
)
  existing=$(gh api "repos/${repo}/issues/${issue}/comments" --paginate --jq ".[] | select(.body | contains(\"${marker}\")) | .id" | head -n1 || true)
  if [ -n "$existing" ]; then
    gh api --method PATCH "repos/${repo}/issues/comments/${existing}" --field body="$(cat "${output_dir}/comment.md")" >/dev/null
  else
    gh api --method POST "repos/${repo}/issues/${issue}/comments" --field body="$(cat "${output_dir}/comment.md")" >/dev/null
  fi
fi

fail_on=${AIB_INPUT_FAIL_ON:-critical}
case "$fail_on" in
  none|"") exit 0 ;;
  critical)
    threshold=$critical
    ;;
  warning)
    threshold=$((critical + warning))
    ;;
  info)
    threshold=$findings
    ;;
  *)
    echo "Unsupported fail-on value: $fail_on" >&2
    exit 2
    ;;
esac

if [ "$threshold" -gt 0 ]; then
  echo "AIB found $threshold finding(s) at fail-on threshold '$fail_on'." >&2
  exit 1
fi
