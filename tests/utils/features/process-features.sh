#!/bin/bash

shopt -s nullglob 

work_dir="${WORK_DIR:-$(mktemp -d)}"
run_id="${RUN_ID:-}"
features="${FEATURES:-cmd,task,change,ensure,endpoint,interface}"

if ! [ -d "$work_dir/feature-tags-artifacts" ]; then
    if [ -z "$run_id" ]; then
        echo "RUN_ID is not set. Please set it to the ID of the GitHub Actions run to download artifacts from."
        exit 1
    fi
    gh run download "$run_id" --pattern "spread-artifacts-*" --dir "$work_dir/feature-tags-artifacts"
    mkdir "$work_dir/tmp"
    find "$work_dir"/feature-tags-artifacts/ -type f -exec mv {} "$work_dir"/tmp/ \;
    rm -r "$work_dir/feature-tags-artifacts/"
    mv "$work_dir/tmp" "$work_dir/feature-tags-artifacts"
fi
if ! [ -f "$work_dir/all-features.json" ]; then
    if [ -z "$run_id" ]; then
        echo "RUN_ID is not set. Please set it to the ID of the GitHub Actions run to download artifacts from."
        exit 1
    fi
    gh run download "$run_id" --name "all-features" --dir "$work_dir/all-features"
    mv "$work_dir/all-features/all-features.json" "$work_dir/all-features.json"
    rm -r "$work_dir/all-features"
fi
          
composedir="$work_dir/composed-tags"
mkdir -p "$composedir"
IFS=',' read -ra features <<< "$features"
for file in "$work_dir/feature-tags-artifacts"/*; do
    echo "Processing artifact $file"
    mkdir "$work_dir/working"
    tar -xzf "$file" -C "$work_dir/working"
    featdir="$work_dir/working/extracted-tags"
    mkdir -p "$featdir"
    if ! [ -d "$work_dir/working/spread-artifacts/feature-tags" ]; then
        echo "Artifact $file has no artifact data"
        rm -r "$work_dir/working"
        continue
    fi
    for dir in "$work_dir/working/spread-artifacts/feature-tags"/*/; do
        if [ -f "${dir}/journal.txt" ] && [ -f "${dir}/state.json" ]; then
            ./tests/utils/features/featextractor.py \
                -f "${features[@]}" \
                --journal "${dir}/journal.txt" \
                --state "${dir}/state.json" \
                --output "$featdir/$(basename "${dir}")"
        elif [ -f "${dir}/journal.txt" ] && [ ! -f "${dir}/state.json" ]; then
            ./tests/utils/features/featextractor.py \
                -f "${features[@]}" \
                --journal "${dir}/journal.txt" \
                --output "$featdir/$(basename "${dir}")"
        elif [ ! -f "${dir}/journal.txt" ]; then
            echo "No journal.txt present in $dir"
            exit 1
        fi
    done
    ./tests/utils/features/featcomposer.py \
        --dir "$featdir" \
        --output "$composedir" \
        --failed-tests "$work_dir/working/spread-artifacts/failed-tests.txt" \
        --run-attempt "$(cat "$work_dir/working/spread-artifacts/run-attempt.txt")" \
        --env-variables "$(cat "$work_dir/working/spread-artifacts/env-variables.txt")"
    rm -r "$work_dir/working"
done
./tests/utils/features/featcomposer.py \
    --dir "$composedir" \
    --output "$work_dir/final-feature-tags" \
    --replace-old-runs

./tests/utils/features/feattranslator.py -f "$work_dir/all-features.json" -o "$work_dir/final-feature-tags/all-features.json"
