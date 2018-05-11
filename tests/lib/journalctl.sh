#!/bin/sh

JOURNALCTL_CURSOR_FILE="${SPREAD_PATH}"/journalctl_cursor

get_last_journalctl_cursor(){
    # It is not being used journalctl json output because jq is not installed on
    # core systems
    if [ -z "$(which jq)" ]; then
        cursor=$(journalctl --output=export -n1 | grep -e '__CURSOR=')
        echo ${cursor#__CURSOR=}
    else
        journalctl --output=json -n1 | jq -r '.__CURSOR'
    fi
}

start_new_journalctl_log(){
    cursor=$(get_last_journalctl_cursor)
    if [ -z "$cursor" ]; then
        echo "Empty journalctl cursor, exiting..."
        exit 1
    else
        echo "$cursor" > "$JOURNALCTL_CURSOR_FILE"
    fi
}

get_journalctl_log(){
    cursor=$(cat "$JOURNALCTL_CURSOR_FILE")
    journalctl "$@" --cursor "$cursor"
}

get_journalctl_log_from_cursor(){
    cursor=$1
    journalctl "$@" --cursor "$cursor"
}
