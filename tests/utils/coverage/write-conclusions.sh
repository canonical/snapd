#!/usr/bin/env bash

set -euo pipefail
shopt -s nullglob

usage() {
    cat <<'EOF'
Usage:
  write-conclusions.sh [--coverage-dir <dir>]

Description:
  Processes all spread-artifacts tarballs in a coverage directory.
  For each tarball, extracts it and runs process-coverage.py with:
    --coverage-dir <extracted>/spread-artifacts/coverage-results
    --results-path <matching spread-results-*/results.json>
    --output-dir <matching spread-results-* directory>

Default coverage directory: ./coverage
EOF
}

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "cannot continue: required command not found: $1" >&2
        exit 1
    fi
}

escape_regex() {
    sed 's/[][(){}.^$+*?|\\-]/\\&/g' <<<"$1"
}

coverage_dir="coverage"

while [[ $# -gt 0 ]]; do
    case "$1" in
    --coverage-dir)
        coverage_dir="${2:-}"
        shift 2
        ;;
    -h|--help)
        usage
        exit 0
        ;;
    *)
        echo "unknown argument: $1" >&2
        usage >&2
        exit 1
        ;;
    esac
done

if [[ -z "$coverage_dir" ]]; then
    echo "cannot continue: provide a non-empty coverage directory" >&2
    exit 1
fi

if [[ ! -d "$coverage_dir" ]]; then
    echo "cannot continue: coverage directory not found: $coverage_dir" >&2
    exit 1
fi

require_cmd tar
require_cmd sed
require_cmd python3

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
processor="$script_dir/process-coverage.py"

if [[ ! -f "$processor" ]]; then
    echo "cannot continue: process-coverage.py not found: $processor" >&2
    exit 1
fi

declare -A results_dir_by_key
declare -A systems_by_run_attempt

for path in "$coverage_dir"/spread-results-*; do
    [[ -d "$path" ]] || continue
    base="$(basename "$path")"
    if [[ "$base" =~ ^spread-results-([0-9]+)-([0-9]+)-(.+)$ ]]; then
        run_id="${BASH_REMATCH[1]}"
        attempt="${BASH_REMATCH[2]}"
        system="${BASH_REMATCH[3]}"
        key="$run_id|$attempt|$system"
        run_attempt_key="$run_id|$attempt"
        results_dir_by_key["$key"]="$path"
        if [[ -z "${systems_by_run_attempt[$run_attempt_key]:-}" ]]; then
            systems_by_run_attempt["$run_attempt_key"]="$system"
        else
            systems_by_run_attempt["$run_attempt_key"]+=$'\n'"$system"
        fi
    fi
done

if [[ "${#results_dir_by_key[@]}" -eq 0 ]]; then
    echo "cannot continue: no spread-results-* directories found in $coverage_dir" >&2
    exit 1
fi

tmp_root="$(mktemp -d)"
trap 'rm -rf "$tmp_root"' EXIT

processed=0
skipped=0

for tar_file in "$coverage_dir"/*.tar "$coverage_dir"/*.tar.gz "$coverage_dir"/*.tgz "$coverage_dir"/*.tar.xz "$coverage_dir"/*.tar.bz2; do
    [[ -f "$tar_file" ]] || continue

    tar_base="$(basename "$tar_file")"
    name="$tar_base"
    name="${name%.tar.gz}"
    name="${name%.tgz}"
    name="${name%.tar.xz}"
    name="${name%.tar.bz2}"
    name="${name%.tar}"

    if [[ ! "$name" =~ ^spread-artifacts-(.+)_([0-9]+)_([0-9]+)$ ]]; then
        echo "Skipping unrecognized tarball name: $tar_base" >&2
        skipped=$((skipped + 1))
        continue
    fi

    descriptor="${BASH_REMATCH[1]}"
    run_id="${BASH_REMATCH[2]}"
    attempt="${BASH_REMATCH[3]}"
    run_attempt_key="$run_id|$attempt"

    systems_block="${systems_by_run_attempt[$run_attempt_key]:-}"
    if [[ -z "$systems_block" ]]; then
        echo "Skipping $tar_base: no spread-results candidates for run=$run_id attempt=$attempt" >&2
        skipped=$((skipped + 1))
        continue
    fi

    best_system=""
    best_len=0
    while IFS= read -r system; do
        [[ -n "$system" ]] || continue
        system_re="$(escape_regex "$system")"
        if [[ "$descriptor" =~ (^|-)${system_re}(-|$) ]]; then
            system_len=${#system}
            if (( system_len > best_len )); then
                best_system="$system"
                best_len=$system_len
            fi
        fi
    done <<<"$systems_block"

    if [[ -z "$best_system" ]]; then
        echo "Skipping $tar_base: could not infer matching spread-results system" >&2
        skipped=$((skipped + 1))
        continue
    fi

    results_key="$run_id|$attempt|$best_system"
    results_dir="${results_dir_by_key[$results_key]:-}"
    if [[ -z "$results_dir" || ! -f "$results_dir/results.json" ]]; then
        echo "Skipping $tar_base: missing results.json for key $results_key" >&2
        skipped=$((skipped + 1))
        continue
    fi

    extract_dir="$tmp_root/$name"
    mkdir -p "$extract_dir"
    tar -xf "$tar_file" -C "$extract_dir"

    cov_dir="$extract_dir/spread-artifacts/coverage-results"
    if [[ ! -d "$cov_dir" ]]; then
        cov_dir="$(find "$extract_dir" -type d -path '*/spread-artifacts/coverage-results' -print -quit)"
    fi
    if [[ -z "$cov_dir" || ! -d "$cov_dir" ]]; then
        echo "Skipping $tar_base: could not find spread-artifacts/coverage-results after extraction" >&2
        skipped=$((skipped + 1))
        continue
    fi

    echo "Processing $tar_base with results $(basename "$results_dir")"
    python3 "$processor" \
        --coverage-dir "$cov_dir" \
        --results-path "$results_dir/results.json" \
        --output-dir "$results_dir"

    processed=$((processed + 1))
done

if (( processed == 0 )); then
    echo "cannot continue: no tarball was successfully processed" >&2
    exit 1
fi

echo "Done. Processed $processed tarball(s), skipped $skipped." >&2
