#!/bin/bash

after_non_nested_task() {
    # Write to the directory specified in the spread.yaml file for artifacts
    local write_dir="${SPREAD_PATH}/feature-tags"
    local task_dir="${write_dir}/${SPREAD_JOB//\//--}"
    mkdir -p "$write_dir"
    mkdir -p "$task_dir"
    "$TESTSTOOLS"/journal-state get-log -u snapd --no-pager --output cat > "$task_dir"/journal.txt
    cp /var/lib/snapd/state.json "$task_dir"
}

after_nested_task() {
    local write_dir="${SPREAD_PATH}/feature-tags"
    local task_dir="${write_dir}/${SPREAD_JOB//\//--}"
    mkdir -p "$write_dir"
    mkdir -p "$task_dir"

    "$TESTSTOOLS"/remote.exec "sudo journalctl -u snapd --no-pager --output cat" > "$task_dir"/journal.txt
    "$TESTSTOOLS"/remote.exec "sudo chmod 777 /var/lib/snapd/state.json"
    "$TESTSTOOLS"/remote.pull "/var/lib/snapd/state.json" "$task_dir"
}


case "$1" in
    --after-non-nested-task)
        if [ -n "$TAG_FEATURES" ]; then
            after_non_nested_task
        fi
        ;;
    --after-nested-task)
        if [ -n "$TAG_FEATURES" ]; then
            after_nested_task
        fi
        ;;
    *)
        echo "unsupported argument: $1"
        exit 1
        ;;
esac