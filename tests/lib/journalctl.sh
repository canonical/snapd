#!/bin/sh

JORNALCTL_CURSOR_FILE="${SPREAD_PATH}"/journalctl_cursor

get_last_journalctl_cursor(){
    # It is not being used journalctl json output because jq is not installed on
    # core systems
    cursor=$(journalctl --output=export -n1 | grep -e '__CURSOR=')
    echo ${cursor#*=}
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
