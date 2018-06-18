#!/bin/bash

JOURNALCTL_CURSOR_FILE="${SPREAD_PATH}"/journalctl_cursor

get_last_journalctl_cursor(){
    journalctl --output=export -n1 | grep --binary-files=text -o '__CURSOR=.*' | sed -e 's/^__CURSOR=//'
}

start_new_journalctl_log(){
    cursor=$(get_last_journalctl_cursor)
    if [ -z "$cursor" ]; then
        echo "Empty journalctl cursor, exiting..."
        exit 1
    else
        echo "$cursor" > "$JOURNALCTL_CURSOR_FILE"
    fi

    test_id="test-$RANDOM"
    echo "$test_id" | systemd-cat
    for _ in $(seq 20); do
        if [ get_journalctl_log | grep -q "$test_id" ]; then
            return
        fi
        sleep 0.5
    done
    echo "Test is not found in journalctl, exiting..."
    exit 1
}

get_journalctl_log(){
    cursor=$(cat "$JOURNALCTL_CURSOR_FILE")
    get_journalctl_log_from_cursor "$cursor" "$@"
}

get_journalctl_log_from_cursor(){
    cursor=$1
    shift
    journalctl "$@" --cursor "$cursor"
}
