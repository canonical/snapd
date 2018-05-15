#!/bin/sh

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
