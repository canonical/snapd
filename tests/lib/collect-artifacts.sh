#!/bin/bash

_prepare_task_artifacts_path() {
    artifact=$1
    local artifacts_dir task_dir
    artifacts_dir="${SPREAD_PATH}/${artifact}"
    task_dir="${artifacts_dir}/${SPREAD_JOB//\//--}"
    mkdir -p "$task_dir"
    echo "$task_dir"
}

_prepare_suite_artifacts_path() {
    artifact=$1
    local artifacts_dir task_dir
    artifacts_dir="${SPREAD_PATH}/${artifact}"
    suite_dir="${artifacts_dir}/${SPREAD_BACKEND}:${SPREAD_SYSTEM}:${SPREAD_SUITE//\//--}"
    mkdir -p "$suite_dir"
    echo "$suite_dir"
}

_extract_trace_entries() {
    # On some systems, JSON log entries can be split across lines; join those
    # fragments before filtering for TRACE entries.
    grep -oP 'snap(?:d|-bootstrap)?\[\d+\]: \K.*' | sed -e ':a' -e '/^{.*"TRACE".*[^}]$/ { N; s/\n//; ba }' | grep '"TRACE"'
}

features_after_non_nested_task() {
    # Write to the directory specified in the spread.yaml file for artifacts
    local task_dir
    task_dir="$(_prepare_task_artifacts_path feature-tags)"
    # On some systems, some log lines get broken into separate entries
    # So for lines with snapd/snap identifiers, search for lines that begin with `{` 
    # but don't end with `}` and have "TRACE", remove their new lines to recompose the entry.
    # Then only grab TRACE-level entries.
    systemctl stop snapd || true
    "$TESTSTOOLS"/journal-state get-log --no-pager | _extract_trace_entries > "$task_dir"/journal.txt
    cp /var/lib/snapd/state.json "$task_dir" || true
    systemctl start snapd || true
}

features_after_nested_task() {
    local task_dir
    task_dir="$(_prepare_task_artifacts_path feature-tags)"

    # When a nested test is skipped, its vm will not be available
    "$TESTSTOOLS"/remote.exec "sudo systemctl stop snapd" || true
    "$TESTSTOOLS"/remote.exec "journalctl --sync" || true
    "$TESTSTOOLS"/remote.exec "journalctl --flush" || true
    # Collect TRACE logs from all boots, appending each boot's logs to journal.txt
    "$TESTSTOOLS"/remote.exec "journalctl --list-boots -q | awk '{print \$1}' | while read boot_id; do sudo journalctl -b \"\$boot_id\" --no-pager | grep -oP 'snap(?:d|-bootstrap)?\[\d+\]: \K.*' | sed -e ':a' -e '/^{.*\\\"TRACE\\\".*[^}]$/ { N; s/\n//; ba }' | grep '\"TRACE\"'; done" > "$task_dir"/journal.txt || true

    # install-mode.log.gz may exist on nested systems under /var/log; pull it, extract and append TRACE entries
    "$TESTSTOOLS"/remote.exec "if [ -f /var/log/install-mode.log.gz ]; then sudo chmod 644 /var/log/install-mode.log.gz; fi" || true
    "$TESTSTOOLS"/remote.pull "/var/log/install-mode.log.gz" "$task_dir" || true
    if [ -f "$task_dir"/install-mode.log.gz ]; then
        gzip -dc "$task_dir"/install-mode.log.gz | _extract_trace_entries >> "$task_dir"/journal.txt || true
        rm -f "$task_dir"/install-mode.log.gz
    fi
    "$TESTSTOOLS"/remote.exec "sudo chmod 777 /var/lib/snapd/state.json" || true
    "$TESTSTOOLS"/remote.pull "/var/lib/snapd/state.json" "$task_dir" || true
}

locks(){
    local task_dir
    task_dir="$(_prepare_task_artifacts_path state-locks)"

    cp -f "$TESTSTMP"/snapd_lock_traces "$task_dir"
}

features_after_suite() {
    systemctl stop snapd || true
    # make sure this is only run once per suite
    if ! [ -f "$TESTSTMP/initial-coverage-collected-${SPREAD_SUITE//\//--}" ]; then
        suite_dir="$(_prepare_suite_artifacts_path feature-tags)"
        if [ -f /var/log/install-mode.log.gz ]; then
            gzip -dc /var/log/install-mode.log.gz | _extract_trace_entries >> "$suite_dir"/journal.txt
        fi

        journalctl --sync || true
        journalctl --flush || true
        journalctl --list-boots -q | awk '{print $1}' | while read -r boot_id; do journalctl -b "$boot_id" --no-pager | _extract_trace_entries; done >> "$suite_dir"/journal.txt
        cp /var/lib/snapd/state.json "$suite_dir" || true

        touch "$TESTSTMP/initial-coverage-collected-${SPREAD_SUITE//\//--}"
    fi
    systemctl start snapd || true
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
            --after-suite)
                features_after_suite
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
    *)
        echo "collect-artifacts: unsupported argument: $1"
        exit 1
        ;;
esac
