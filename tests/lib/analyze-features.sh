#!/bin/bash

before_non_nested_task() {
    # Add a recognizable unique string to the journal logs.
    # This will be the marker for the journal-analyzer.py script to start parsing the logs.
    echo "end-of-prepare-suite-each for $SPREAD_JOB" | systemd-cat
}

before_nested_task() {
    # Add a recognizable unique string to the journal logs.
    # This will be the marker for the journal-analyzer.py script to start parsing the logs.
    "$TESTSTOOLS"/remote.exec "echo \"end-of-prepare-suite-each for $SPREAD_JOB\" | systemd-cat"
}

after_non_nested_task() {
    # Write to the directory specified in the spread.yaml file for artifacts
    local write_dir="${SPREAD_PATH}/feature-tags"
    mkdir -p "$write_dir"
    "$TESTSTOOLS"/journal-analyzer.py -t "end-of-prepare-suite-each for $SPREAD_JOB" -f "$TAG_FEATURES" -o "${write_dir}/${SPREAD_JOB//\//_}"
}

after_nested_task() {
    rm -rf tmp-logs
    mkdir tmp-logs

    # The VM will get destroyed after this function, so no harm in changing log permissions
    "$TESTSTOOLS"/remote.exec "sudo chmod -R 777 /var/log/journal/"
    # Save the VM's journal logs in a local folder for analysis
    "$TESTSTOOLS"/remote.pull "/var/log/journal/*/*" tmp-logs/

    local write_dir="${SPREAD_PATH}/feature-tags"
    mkdir -p "$write_dir"
    "$TESTSTOOLS"/journal-analyzer.py -t "end-of-prepare-suite-each for $SPREAD_JOB" -d tmp-logs -f "$TAG_FEATURES" -o "${write_dir}/${SPREAD_JOB//\//_}"
    
    rm -rf tmp-logs
}


case "$1" in
    --before-non-nested-task)
        if [ -n "$TAG_FEATURES" ]; then
            before_non_nested_task
        fi
        ;;
    --before-nested-task)
        if [ -n "$TAG_FEATURES" ]; then
            before_nested_task
        fi
        ;;
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