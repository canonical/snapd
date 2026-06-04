#!/usr/bin/env bash

set -euo pipefail
shopt -s nullglob

usage() {
    cat <<'EOF'
Usage:
  write-conclusions.sh [--coverage-dir <dir> --output-dir <dir>]

Description:
  Processes all spread-artifacts tarballs in a coverage directory.
  For each tarball, extracts it and runs process-coverage.py with:
    --coverage-dir <extracted>/spread-artifacts/coverage-results
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
output_dir="results"

while [[ $# -gt 0 ]]; do
    case "$1" in
    --coverage-dir)
        coverage_dir="${2:-}"
        shift 2
        ;;
    --output-dir)
        output_dir="${2:-}"
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

if [[ -z "$output_dir" ]]; then
    echo "cannot continue: provide a non-empty output directory" >&2
    exit 1
fi

mkdir -p "$output_dir"

require_cmd tar
require_cmd sed
require_cmd python3

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
processor="$script_dir/process-coverage.py"

if [[ ! -f "$processor" ]]; then
    echo "cannot continue: process-coverage.py not found: $processor" >&2
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

    echo "Processing $tar_base with results"
    python3 "$processor" \
        --coverage-dir "$cov_dir" > "$output_dir"/"$descriptor".json

    processed=$((processed + 1))
done

if (( processed == 0 )); then
    echo "cannot continue: no tarball was successfully processed" >&2
    exit 1
fi

echo "Done. Processed $processed tarball(s), skipped $skipped." >&2
