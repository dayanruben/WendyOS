#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWIFT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DEFAULT_OUTPUT_DIR="${WENDY_E2E_OUTPUT_DIR:-/tmp/wendy}"

OUTPUT_DIR="$DEFAULT_OUTPUT_DIR"
OPEN_REPORT="true"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--output-dir DIR] [--open|--no-open]

Analyze all raw Swift E2E runs found in an output directory.

The script deletes matching aggregate directories, aggregates raw runs,
reviews the aggregate results, renders HTML reports, and opens the newest
report on macOS.

Options:
  --output-dir DIR  Directory containing raw Swift E2E run directories;
                    defaults to $DEFAULT_OUTPUT_DIR.
  --open            Open the newest generated report when supported; default.
  --no-open         Do not open a report.
  --help            Show this help message.
EOF
}

expand_local_path() {
  local path="$1"
  case "$path" in
    '~')
      printf "%s" "${HOME:?}"
      ;;
    '~/'*)
      printf "%s/%s" "${HOME:?}" "${path#~/}"
      ;;
    *)
      printf "%s" "$path"
      ;;
  esac
}

absolute_dir_path() {
  local path
  path="$(expand_local_path "$1")"
  mkdir -p "$path"
  (cd "$path" && pwd)
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir)
      OUTPUT_DIR="$2"
      shift 2
      ;;
    --open)
      OPEN_REPORT="true"
      shift
      ;;
    --no-open)
      OPEN_REPORT="false"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

OUTPUT_DIR="$(absolute_dir_path "$OUTPUT_DIR")"

is_raw_run_dir() {
  local dir="$1"
  local base="${dir##*/}"
  [[ -d "$dir" ]] || return 1
  [[ "$base" =~ \.[0-9][0-9][0-9][0-9]$ ]] || return 1
  [[ -f "$dir/info.json" ]] || return 1
  ! grep -q '"kind"[[:space:]]*:[[:space:]]*"swift-e2e-aggregate"' "$dir/info.json"
}

aggregate_dir_for_run() {
  local run_id="$1"
  local run_base aggregate_name
  run_base="${run_id%.*}"
  aggregate_name="${run_base%.*}"
  printf "%s/%s" "$OUTPUT_DIR" "$aggregate_name"
}

mapfile -t RUN_DIRS < <(
  find "$OUTPUT_DIR" -mindepth 1 -maxdepth 1 -type d | sort | while IFS= read -r dir; do
    if is_raw_run_dir "$dir"; then
      printf '%s\n' "$dir"
    fi
  done
)

if [[ ${#RUN_DIRS[@]} -eq 0 ]]; then
  echo "ERROR: no raw Swift E2E run directories found in $OUTPUT_DIR." >&2
  exit 64
fi

mapfile -t AGGREGATE_DIRS < <(
  for run_dir in "${RUN_DIRS[@]}"; do
    aggregate_dir_for_run "${run_dir##*/}"
  done | sort -u
)

echo "==> Analyzing Swift E2E runs"
echo "    Output dir: $OUTPUT_DIR"
for run_dir in "${RUN_DIRS[@]}"; do
  echo "    Run:        $run_dir"
done
for aggregate_dir in "${AGGREGATE_DIRS[@]}"; do
  echo "    Aggregate:  $aggregate_dir"
done

for aggregate_dir in "${AGGREGATE_DIRS[@]}"; do
  rm -rf "$aggregate_dir"
done

status=0
bash "$SCRIPT_DIR/E2EAggregate.sh" \
  --output-dir "$OUTPUT_DIR" \
  "${RUN_DIRS[@]}" || status=$?

for aggregate_dir in "${AGGREGATE_DIRS[@]}"; do
  bash "$SCRIPT_DIR/E2EReview.sh" --run-dir "$aggregate_dir" || {
    step_status=$?
    [[ $status -eq 0 ]] && status=$step_status
  }
  bash "$SCRIPT_DIR/E2EReport.sh" --run-dir "$aggregate_dir" || {
    step_status=$?
    [[ $status -eq 0 ]] && status=$step_status
  }
done

latest_report=""
latest_mtime=0
for aggregate_dir in "${AGGREGATE_DIRS[@]}"; do
  report_path="$aggregate_dir/index.html"
  [[ -f "$report_path" ]] || continue
  mtime="$(stat -f %m "$report_path" 2>/dev/null || stat -c %Y "$report_path" 2>/dev/null || echo 0)"
  if [[ "$mtime" -ge "$latest_mtime" ]]; then
    latest_mtime="$mtime"
    latest_report="$report_path"
  fi
done

if [[ -n "$latest_report" ]]; then
  if [[ "$OPEN_REPORT" == "true" && "$(uname -s)" == "Darwin" ]]; then
    open "$latest_report" || echo "HTML report: $latest_report"
  else
    echo "HTML report: $latest_report"
  fi
else
  echo "HTML report not found in analyzed aggregate directories." >&2
  [[ $status -eq 0 ]] && status=1
fi

exit "$status"
