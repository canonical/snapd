#!/bin/bash

# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"

# This file contains all the cursors used for the different tests
JOURNALCTL_CURSOR_FILE="$RUNTIME_STATE_PATH"/journalctl_cursor

get_last_journalctl_cursor(){
    journalctl --output=export -n1 | grep --binary-files=text -o '__CURSOR=.*' | sed -e 's/^__CURSOR=//'
}

start_new_journalctl_log(){
    echo "New test starts here - $SPREAD_JOB" | systemd-cat -t snapd-test
    cursor=$(get_last_journalctl_cursor)
    if [ -z "$cursor" ]; then
        echo "Empty journalctl cursor, exiting..."
        exit 1
    else
        echo "$SPREAD_JOB " >> "$JOURNALCTL_CURSOR_FILE"
        echo "$cursor" >> "$JOURNALCTL_CURSOR_FILE"
    fi
}

check_journalctl_ready(){
    marker="test-${RANDOM}${RANDOM}"
    echo "Running test: $marker" | systemd-cat -t snapd-test
    if check_journalctl_log "$marker"; then
        return 0
    fi
    echo "Test id not found in journalctl, exiting..."
    exit 1
}

check_journalctl_log(){
    expression=$1
    shift
    for _ in $(seq 20); do
        log=$(get_journalctl_log "$@")
        if echo "$log" | grep -q -E "$expression"; then
            return 0
        fi
        echo "Match failed, retrying"
        sleep .5
    done
    return 1
}

get_journalctl_log(){
    cursor=""
    if [ -f "$JOURNALCTL_CURSOR_FILE" ]; then
        cursor=$(tail -n1 "$JOURNALCTL_CURSOR_FILE")
    fi
    get_journalctl_log_from_cursor "$cursor" "$@"
}

get_journalctl_log_from_cursor(){
    cursor=$1
    shift
    if [ -z "$cursor" ]; then
        journalctl "$@"
    else
        journalctl "$@" --cursor "$cursor"
    fi
}
