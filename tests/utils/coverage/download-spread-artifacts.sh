#!/usr/bin/env bash

set -euo pipefail
shopt -s nullglob

usage() {
    cat <<'EOF'
Usage:
    download-spread-artifacts.sh --run-id <id> [--out-dir <dir>] [--download-dir <dir>] [--repo <owner/repo>]

Description:
    Downloads all artifacts from a GitHub Actions run that start with:
    - spread-artifacts-
    - spread-results-

    Then creates a coverage directory structure like:
        <out-dir>/spread-artifacts-*.tar.gz
        <out-dir>/spread-results-*/results.json

    This layout is used by tests/utils/coverage/write-conclusions.sh.

Options:
  --run-id <id>        GitHub Actions run id (required)
    --out-dir <dir>      Output directory (default: ./coverage)
    --download-dir <dir> Temporary download directory (default: temporary directory)
  --repo <owner/repo>  Repository for gh run download (default: current gh repo)
  -h, --help           Show this help

Examples:
    ./tests/utils/coverage/download-spread-artifacts.sh --run-id 26581205754
    ./tests/utils/coverage/download-spread-artifacts.sh --run-id 26581205754 --out-dir /tmp/coverage
EOF
}

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo "cannot continue: required command not found: $1" >&2
        exit 1
    fi
}

run_id=""
out_dir="coverage"
download_dir=""
repo=""

while [[ $# -gt 0 ]]; do
    case "$1" in
    --run-id)
        run_id="${2:-}"
        shift 2
        ;;
    --out-dir)
        out_dir="${2:-}"
        shift 2
        ;;
    --download-dir)
        download_dir="${2:-}"
        shift 2
        ;;
    --repo)
        repo="${2:-}"
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

if [[ -z "$run_id" ]]; then
    echo "cannot continue: provide --run-id" >&2
    usage >&2
    exit 1
fi

require_cmd gh
require_cmd cp
require_cmd mktemp
require_cmd find

if [[ -z "$repo" ]]; then
    repo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
fi

cleanup_download_dir=0
if [[ -z "$download_dir" ]]; then
    download_dir="$(mktemp -d)"
    cleanup_download_dir=1
else
    mkdir -p "$download_dir"
fi

mkdir -p "$out_dir"

if [[ "$cleanup_download_dir" -eq 1 ]]; then
    trap 'rm -rf "$download_dir"' EXIT
fi

echo "Downloading spread-artifacts for run $run_id from $repo"
gh run download "$run_id" --repo "$repo" --pattern 'spread-artifacts-*' --dir "$download_dir"

echo "Downloading spread-results for run $run_id from $repo"
gh run download "$run_id" --repo "$repo" --pattern 'spread-results-*' --dir "$download_dir"

artifacts_count=0
results_count=0

# Flatten spread-artifacts payloads (tar files) to <out-dir>/
for artifact_dir in "$download_dir"/spread-artifacts-*; do
    [[ -d "$artifact_dir" ]] || continue
    tar_found=0
    while IFS= read -r -d '' tar_file; do
        tar_found=1
        cp -f "$tar_file" "$out_dir/"
        artifacts_count=$((artifacts_count + 1))
    done < <(find "$artifact_dir" -maxdepth 1 -type f \( -name '*.tar' -o -name '*.tar.gz' -o -name '*.tgz' -o -name '*.tar.xz' -o -name '*.tar.bz2' \) -print0)

    if [[ "$tar_found" -eq 0 ]]; then
        echo "Warning: no tar payload found in $(basename "$artifact_dir")" >&2
    fi
done

# Copy spread-results-* directories to <out-dir>/
for result_dir in "$download_dir"/spread-results-*; do
    [[ -d "$result_dir" ]] || continue
    base="$(basename "$result_dir")"
    rm -rf "$out_dir/$base"
    cp -a "$result_dir" "$out_dir/"
    results_count=$((results_count + 1))
done

if [[ "$artifacts_count" -eq 0 ]]; then
    echo "No spread-artifacts tarballs were downloaded for run $run_id" >&2
    exit 1
fi

if [[ "$results_count" -eq 0 ]]; then
    echo "No spread-results-* artifacts were downloaded for run $run_id" >&2
    exit 1
fi

echo "Done. Wrote $artifacts_count tarballs and $results_count spread-results directories into $out_dir"