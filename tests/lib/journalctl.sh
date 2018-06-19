#!/bin/bash

# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"

JOURNALCTL_CURSOR_FILE="$RUNTIME_STATE_PATH"/journalctl_cursor

get_last_journalctl_cursor(){
    journalctl --output=export -n1 | grep --binary-files=text -o '__CURSOR=.*' | sed -e 's/^__CURSOR=//'
}

start_new_journalctl_log(){
    cursor=$(get_last_journalctl_cursor)
    if [ -z "$cursor" ]; then
        echo "Empty journalctl cursor, exiting..."
        exit 1
    else
        echo "$SPREAD_JOB " >> "$JOURNALCTL_CURSOR_FILE"
        echo "$cursor" >> "$JOURNALCTL_CURSOR_FILE"
    fi

    echo "New test starts here - $SPREAD_JOB" | systemd-cat
    test_id="test-${RANDOM}${RANDOM}"
    echo "$test_id" | systemd-cat
    if get_journalctl_log | grep -q "$test_id"; then
        return
    fi
    echo "Test id not found in journalctl, exiting..."
    cat "$JOURNALCTL_CURSOR_FILE"
    exit 1
}

get_journalctl_log(){
    cursor=$(tail -n1 "$JOURNALCTL_CURSOR_FILE")
    get_journalctl_log_from_cursor "$cursor" "$@"
}

get_journalctl_log_from_cursor(){
    cursor=$1
    shift
    journalctl "$@" --cursor "$cursor"
}
