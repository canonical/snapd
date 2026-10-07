#!/bin/bash

# Helpers for exercising user-service management without the session agent.
# Source this file from a spread task; ownership records live in the task directory.

user_service_agent_check() {
    local user="$1" uid unit
    uid="$(id -u "$user")"
    systemctl is-active "user@$uid.service"
    test -S "/run/user/$uid/bus"
    # systemctl --user can use the private manager socket. Explicitly address
    # the regular bus to verify the transport used by snapd is available too.
    tests.session -u "$user" exec busctl --address="unix:path=/run/user/$uid/bus" \
        get-property org.freedesktop.systemd1 /org/freedesktop/systemd1 \
        org.freedesktop.systemd1.Manager Version
    for unit in snapd.session-agent.service snapd.session-agent.socket; do
        test "$(readlink "/run/user/$uid/systemd/user/$unit")" = /dev/null
        tests.session -u "$user" exec systemctl --user show -p LoadState "$unit" | MATCH '^LoadState=masked$'
        tests.session -u "$user" exec systemctl --user show -p ActiveState "$unit" | MATCH '^ActiveState=inactive$'
    done
    if [ "${SESSION_AGENT:-absent}" = stale ]; then
        test -S "/run/user/$uid/snapd-session-agent.socket"
    else
        test ! -e "/run/user/$uid/snapd-session-agent.socket"
        test ! -L "/run/user/$uid/snapd-session-agent.socket"
    fi
}

user_service_agent_mask() {
    local user="$1" uid unit path
    uid="$(id -u "$user")"
    mkdir -p ".user-service-agent-masks/$user"
    tests.session -u "$user" exec mkdir -p "/run/user/$uid/systemd/user"
    for unit in snapd.session-agent.service snapd.session-agent.socket; do
        path="/run/user/$uid/systemd/user/$unit"
        if [ -e "$path" ] || [ -L "$path" ]; then
            # Preserve pre-existing masks; never overwrite another unit file.
            test "$(readlink "$path")" = /dev/null
        else
            tests.session -u "$user" exec ln -s /dev/null "$path"
            touch ".user-service-agent-masks/$user/$unit"
        fi
    done
    tests.session -u "$user" exec systemctl --user daemon-reload
    tests.session -u "$user" exec systemctl --user stop snapd.session-agent.socket snapd.session-agent.service
    path="/run/user/$uid/snapd-session-agent.socket"
    test ! -L "$path"
    if [ "${SESSION_AGENT:-}" = stale ]; then
        # Socket units may leave their inode behind when stopped. Keep that
        # existing stale socket, or create a test-owned one if it was removed.
        if [ ! -e "$path" ]; then
            touch ".user-service-agent-masks/$user/stale-socket"
            tests.session -u "$user" exec python3 -c \
                'import socket, sys; s = socket.socket(socket.AF_UNIX); s.bind(sys.argv[1]); s.close()' "$path"
        fi
        test -S "$path"
    elif [ -e "$path" ]; then
        test -S "$path"
        test ! -e "$path.spread-backup"
        test ! -L "$path.spread-backup"
        mv "$path" "$path.spread-backup"
        touch ".user-service-agent-masks/$user/saved-socket"
    fi
    user_service_agent_check "$user"
}

user_service_agent_restore() {
    local user="$1" uid unit path
    if [ ! -d ".user-service-agent-masks/$user" ]; then
        return
    fi
    uid="$(id -u "$user")"
    if [ -f ".user-service-agent-masks/$user/stale-socket" ]; then
        path="/run/user/$uid/snapd-session-agent.socket"
        if [ -e "$path" ]; then
            test -S "$path"
            rm "$path"
        fi
        rm ".user-service-agent-masks/$user/stale-socket"
    fi
    if [ -f ".user-service-agent-masks/$user/saved-socket" ]; then
        path="/run/user/$uid/snapd-session-agent.socket"
        if [ -e "$path.spread-backup" ]; then
            test -S "$path.spread-backup"
            test ! -e "$path"
            test ! -L "$path"
            mv "$path.spread-backup" "$path"
        fi
        rm ".user-service-agent-masks/$user/saved-socket"
    fi
    for unit in snapd.session-agent.service snapd.session-agent.socket; do
        if [ ! -e ".user-service-agent-masks/$user/$unit" ]; then
            continue
        fi
        path="/run/user/$uid/systemd/user/$unit"
        if [ -e "$path" ] || [ -L "$path" ]; then
            test "$(readlink "$path")" = /dev/null
            rm "$path"
        fi
        rm ".user-service-agent-masks/$user/$unit"
    done
    if systemctl is-active --quiet "user@$uid.service"; then
        tests.session -u "$user" exec systemctl --user daemon-reload
    fi
    rmdir ".user-service-agent-masks/$user"
    rmdir --ignore-fail-on-non-empty .user-service-agent-masks
}
