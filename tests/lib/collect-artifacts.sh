#!/bin/bash

_prepare_artifacts_path() {
    artifact=$1
    local artifacts_dir task_dir
    artifacts_dir="${SPREAD_PATH}/${artifact}"
    task_dir="${artifacts_dir}/${SPREAD_JOB//\//--}"
    mkdir -p "$task_dir"
    echo "$task_dir"
}

features_after_non_nested_task() {
    # Write to the directory specified in the spread.yaml file for artifacts
    local task_dir
    task_dir="$(_prepare_artifacts_path feature-tags)"
    # On some systems, some log lines get broken into separate entries
    # So for lines with snapd/snap identifiers, search for lines that begin with `{` 
    # but don't end with `}` and have "TRACE", remove their new lines to recompose the entry.
    # Then only grab TRACE-level entries.
    "$TESTSTOOLS"/journal-state get-log --no-pager | grep -oP 'snapd?\[\d+\]: \K.*' | sed -e ':a' -e '/^{.*\"TRACE\".*[^}]$/ { N; s/\n//; ba }' | grep '"TRACE"' > "$task_dir"/journal.txt
    cp /var/lib/snapd/state.json "$task_dir" || true
}

features_after_nested_task() {
    local task_dir
    task_dir="$(_prepare_artifacts_path feature-tags)"

    # When a nested test is skipped, its vm will not be available
    "$TESTSTOOLS"/remote.exec "sudo journalctl --no-pager | grep -oP 'snapd?\[\d+\]: \K.*' | sed -e ':a' -e '/^{.*\\\"TRACE\\\".*[^}]$/ { N; s/\n//; ba }' | grep '\"TRACE\"'" > "$task_dir"/journal.txt || true
    "$TESTSTOOLS"/remote.exec "sudo chmod 777 /var/lib/snapd/state.json" || true
    "$TESTSTOOLS"/remote.pull "/var/lib/snapd/state.json" "$task_dir" || true
}

locks(){
    local task_dir
    task_dir="$(_prepare_artifacts_path state-locks)"

    cp -f "$TESTSTMP"/snapd_lock_traces "$task_dir"
}

coverage_after_non_nested_task() {
    # Copy the coverage files to the artifacts directory
    if [ -d "$TESTSTMP"/coverage ] && [ $(ls "$TESTSTMP"/coverage | wc -l) -gt 0 ]; then
        local task_dir
        task_dir="$(_prepare_artifacts_path coverage-results)"
        pushd "$SPREAD_PATH"
        go run "$SPREAD_PATH"/tests/utils/coverage -results-dir "$TESTSTMP"/coverage -output functions > "$task_dir"/coverage.json || true
        popd
        if ! [ -s "$task_dir"/coverage.json ]; then
            cp "$TESTSTMP"/coverage/* "$task_dir" || true
            chmod 666 "$task_dir"/* || true
            rm -f "$task_dir"/coverage.json
        fi
    fi
}

coverage_after_nested_task() {
    # Copy the coverage files to the artifacts directory
    local task_dir
    task_dir="$(_prepare_artifacts_path coverage-results)"
    "$TESTSTOOLS"/remote.pull "$TESTSTMP"/coverage/* "$task_dir" || true
    if [ $(ls "$task_dir" | wc -l) -eq 0 ]; then
        rm -r "$task_dir"
        return
    fi
    pushd "$SPREAD_PATH"
    go run "$SPREAD_PATH"/tests/utils/coverage -results-dir "$task_dir" -output functions > "$task_dir"/coverage.json
    popd
    if [ -s "$task_dir"/coverage.json ]; then
        find "$task_dir" -not -name coverage.json -type f -exec rm {} \;
    else
        rm "$task_dir"/coverage.json
        chmod 666 "$task_dir"/* || true
    fi
}

if [ "$#" == 0 ]; then
    echo "collect-artifacts: Illegal number of parameters"
    exit 1
fi

artifact=$1
shift
case "$artifact" in
    features)
        if [ -z "$TAG_FEATURES" ]; then
            exit
        fi
        if [ "$#" == 0 ]; then
            echo "collect-artifacts: features parameter missing"
            exit 1
        fi
        case "$1" in
            --after-non-nested-task)
                features_after_non_nested_task
                ;;
            --after-nested-task)
                features_after_nested_task
                ;;
            *)
                echo "collect-artifacts: unsupported action $1" >&2
                exit 1
                ;;
        esac
        ;;
    locks)
        if [ "$SNAPD_STATE_LOCK_TRACE_THRESHOLD_MS" -le 0 ]; then
            exit
        fi
        locks
        ;;
    coverage)
        if [ "$GENERATE_COVERAGE" = "false" ]; then
            exit
        fi
        case "$1" in
            --after-non-nested-task)
                coverage_after_non_nested_task
                ;;
            --after-nested-task)
                coverage_after_nested_task
                ;;
            *)
                echo "collect-artifacts: unsupported action $1" >&2
                exit 1
                ;;
        esac
        ;;
    *)
        echo "collect-artifacts: unsupported argument: $1"
        exit 1
        ;;
esac
