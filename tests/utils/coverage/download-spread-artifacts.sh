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

  Then matches each spread-artifacts entry with its corresponding spread-results
  entry using:
    - run id
    - attempt
    - system name

  For each match, creates:
    <out-dir>/<system>-<run-id>-<attempt>/

  and copies extracted files from both artifacts into that directory.

Options:
  --run-id <id>        GitHub Actions run id (required)
  --out-dir <dir>      Output directory (default: ./spread-joined)
  --download-dir <dir> Where to download artifacts (default: temporary directory)
  --repo <owner/repo>  Repository for gh run download (default: current gh repo)
  -h, --help           Show this help

Examples:
    ./tests/utils/coverage/download-spread-artifacts.sh --run-id 26581205754
    ./tests/utils/coverage/download-spread-artifacts.sh --run-id 26581205754 --out-dir /tmp/spread-joined
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

run_id=""
out_dir="spread-joined"
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
require_cmd sed
require_cmd cp
require_cmd mktemp
require_cmd tar

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

declare -A results_dir_by_key
declare -A systems_by_run_attempt
declare -A artifact_dir_by_name

for path in "$download_dir"/spread-results-*; do
    [[ -d "$path" ]] || continue
    base="$(basename "$path")"
    if [[ "$base" =~ ^spread-results-([0-9]+)-([0-9]+)-(.+)$ ]]; then
        res_run_id="${BASH_REMATCH[1]}"
        attempt="${BASH_REMATCH[2]}"
        system="${BASH_REMATCH[3]}"
        key="$res_run_id|$attempt|$system"
        run_attempt_key="$res_run_id|$attempt"
        results_dir_by_key["$key"]="$path"
        if [[ -z "${systems_by_run_attempt[$run_attempt_key]:-}" ]]; then
            systems_by_run_attempt["$run_attempt_key"]="$system"
        else
            systems_by_run_attempt["$run_attempt_key"]+=$'\n'"$system"
        fi
    else
        echo "Skipping unrecognized spread-results artifact name: $base" >&2
    fi
done

for path in "$download_dir"/spread-artifacts-*; do
    [[ -d "$path" ]] || continue
    base="$(basename "$path")"
    artifact_dir_by_name["$base"]="$path"
done

if [[ "${#artifact_dir_by_name[@]}" -eq 0 ]]; then
    echo "No spread-artifacts-* artifacts found for run $run_id" >&2
    exit 1
fi

if [[ "${#results_dir_by_key[@]}" -eq 0 ]]; then
    echo "No spread-results-* artifacts found for run $run_id" >&2
    exit 1
fi

matched=0

for artifact_name in "${!artifact_dir_by_name[@]}"; do
    artifact_path="${artifact_dir_by_name[$artifact_name]}"
    if [[ ! "$artifact_name" =~ ^spread-artifacts-(.+)_([0-9]+)_([0-9]+)$ ]]; then
        echo "Skipping unrecognized spread-artifacts artifact name: $artifact_name" >&2
        continue
    fi

    descriptor="${BASH_REMATCH[1]}"
    art_run_id="${BASH_REMATCH[2]}"
    art_attempt="${BASH_REMATCH[3]}"
    run_attempt_key="$art_run_id|$art_attempt"

    systems_block="${systems_by_run_attempt[$run_attempt_key]:-}"
    if [[ -z "$systems_block" ]]; then
        echo "No spread-results candidate found for $artifact_name (run=$art_run_id attempt=$art_attempt)" >&2
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
        echo "Could not infer system name for $artifact_name from spread-results candidates" >&2
        continue
    fi

    key="$art_run_id|$art_attempt|$best_system"
    result_path="${results_dir_by_key[$key]:-}"
    if [[ -z "$result_path" ]]; then
        echo "No exact spread-results match for $artifact_name and system=$best_system" >&2
        continue
    fi

    target_dir="$out_dir/$best_system-$art_run_id-$art_attempt"
    mkdir -p "$target_dir"

    cp -a "$artifact_path"/. "$target_dir"/
    cp -a "$result_path"/. "$target_dir"/

    # spread-artifacts downloads contain tar payloads; extract them in place.
    tar_found=0
    for tar_file in "$artifact_path"/*.tar "$artifact_path"/*.tar.gz "$artifact_path"/*.tgz "$artifact_path"/*.tar.xz "$artifact_path"/*.tar.bz2; do
        [[ -f "$tar_file" ]] || continue
        tar_found=1
        tar_base="$(basename "$tar_file")"
        echo "Extracting $tar_base into $target_dir"
        tar -xf "$tar_file" -C "$target_dir"
        rm -f "$target_dir/$tar_base"
    done
    if [[ "$tar_found" -eq 0 ]]; then
        echo "Warning: no tar payload found in $artifact_name" >&2
    fi

    echo "Matched: $artifact_name <-> $(basename "$result_path") -> $target_dir"
    matched=$((matched + 1))
done

if [[ "$matched" -eq 0 ]]; then
    echo "No matching spread-artifacts/spread-results pairs were found" >&2
    exit 1
fi

echo "Done. Created $matched merged system directories in $out_dir"