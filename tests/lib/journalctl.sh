#!/bin/bash

# get the current time formatted to be used by journalctl
get_journalctl_since() {
    echo $(date +"%Y-%m-%d %H:%M:%S")
}

# check if a pattern is in journalctl since a specific time
check_journalctl_since() {
    local PATTERN="$1"
    local SINCE="$2"

    for i in $(seq 3); do
        if journalctl --since="$SINCE" | grep -q "$PATTERN"; then
            break
        fi
        sleep 1
    done
    journalctl --since="$SINCE" | MATCH "$PATTERN"
}

# check if a pattern is in journalctl
check_journalctl() {
    local PATTERN="$1"

    for i in $(seq 3); do
        if journalctl | grep -q "$PATTERN"; then
            break
        fi
        sleep 1
    done
    journalctl | MATCH "$PATTERN"
}
