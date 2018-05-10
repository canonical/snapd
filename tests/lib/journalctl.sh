#!/bin/sh

JORNALCTL_CURSOR_FILE="${SPREAD_PATH}"/journalctl_cursor

get_last_journalctl_cursor(){
    journalctl --output=json -n1 | jq -r '.__CURSOR'
}

start_new_journalctl_log(){
    cursor=$(get_last_journalctl_cursor)
    if [ -z "$cursor" ]; then
        echo "Empty journalctl cursor, exiting..."
        exit 1
    else
        echo "$cursor" > "$JORNALCTL_CURSOR_FILE"
    fi
}

get_journalctl_log(){
    cursor=$(cat "$JORNALCTL_CURSOR_FILE")
    journalctl "$@" --cursor "$cursor"
}

get_journalctl_log_from_cursor(){
    cursor=$1
    journalctl "$@" --cursor "$cursor"
}
