#!/bin/bash

TEST_USER="test"
#shellcheck disable=SC2034
TEST_USER_HOME=/home/test
TEST_UID="$(id -u "$TEST_USER")"
USER_RUNTIME_DIR="/run/user/${TEST_UID}"

start_user_session() {
    # Make sure the test user's XDG_RUNTIME_DIR exists
    mkdir -p "$USER_RUNTIME_DIR"
    chmod u=rwX,go= "$USER_RUNTIME_DIR"
    chown "${TEST_USER}:${TEST_USER}" "$USER_RUNTIME_DIR"

    if ! has_user_session_support ; then
        # no session manager to start
        return 0
    fi

    systemctl start "user@${TEST_UID}.service"
    USER_DBUS_SESSION_BUS_ADDRESS="$(user_session_dbus_address)"
}

stop_user_session() {
    if ! has_user_session_support ; then
        # no session manager to kill
        return 0
    fi

    systemctl stop "user@${TEST_UID}.service"
}

purge_user_session_data() {
    if [ -n "$USER_RUNTIME_DIR" ] && [ -d "${USER_RUNTIME_DIR}" ]; then
        umount --lazy "${USER_RUNTIME_DIR}/doc" || :
        rm -rf "${USER_RUNTIME_DIR:?}"/* "${USER_RUNTIME_DIR:?}"/.[!.]*
    fi
}

has_user_session_support() {
    # assuming user sessions support exists if the current user has a
    # user-session systemd running
    test -S "/run/user/$(id -u)/systemd/private"
}

user_session_dbus_address() {
    as_user_simple "XDG_RUNTIME_DIR=\"${USER_RUNTIME_DIR}\" systemctl --user show-environment" | \
        grep ^DBUS_SESSION_BUS_ADDRESS | \
        cut -f2- -d=
}

USER_DBUS_SESSION_BUS_ADDRESS=
as_user() {
    if has_user_session_support && [ -z "$USER_DBUS_SESSION_BUS_ADDRESS" ]; then
        USER_DBUS_SESSION_BUS_ADDRESS="$(user_session_dbus_address)"
    fi
    as_user_simple "XDG_RUNTIME_DIR=\"${USER_RUNTIME_DIR}\" DBUS_SESSION_BUS_ADDRESS=\"${USER_DBUS_SESSION_BUS_ADDRESS:-}\" $*"
}

as_user_simple() {
    su -l -c "$*" "$TEST_USER"
}
