#!/bin/bash

#shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB"/pkgdb.sh

TEST_UID="$(id -u test)"
USER_RUNTIME_DIR="/run/user/${TEST_UID}"

setup_portals() {
    # Configure xdg-desktop-portal service activation for fake portal UI.
    cat << EOF > /usr/share/dbus-1/services/org.freedesktop.impl.portal.spread.service
[D-BUS Service]
Name=org.freedesktop.impl.portal.spread
Exec=/usr/bin/python3 $TESTSLIB/fakeportalui/portalui.py
SystemdService=spread-portal-ui.service
EOF
    cat << EOF > /usr/lib/systemd/user/spread-portal-ui.service
[Unit]
Description=Fake portal UI
[Service]
Type=dbus
BusName=org.freedesktop.impl.portal.spread
ExecStart=/usr/bin/python3 $TESTSLIB/fakeportalui/portalui.py
EOF
    mkdir -p /usr/share/xdg-desktop-portal/portals
    cat << EOF > /usr/share/xdg-desktop-portal/portals/spread.portal
[portal]
DBusName=org.freedesktop.impl.portal.spread
Interfaces=org.freedesktop.impl.portal.FileChooser;org.freedesktop.impl.portal.Screenshot;org.freedesktop.impl.portal.AppChooser;
UseIn=spread
EOF

    # Make sure the test user's XDG_RUNTIME_DIR exists
    mkdir -p "$USER_RUNTIME_DIR"
    chmod u=rwX,go= "$USER_RUNTIME_DIR"
    chown test:test "$USER_RUNTIME_DIR"

    systemctl start "user@${TEST_UID}.service"
    as_user systemctl --user set-environment XDG_CURRENT_DESKTOP=spread
}

teardown_portals() {
    # Stop the user service and revert the test user environment
    as_user systemctl --user unset-environment XDG_CURRENT_DESKTOP=spread
    systemctl stop "user@${TEST_UID}.service"

    # Make sure there are not any fakeportalui process running for the test user
    if ps -ef | grep -E '^test .*/fakeportalui/.*'; then
        echo "test users should be not running any fakeportalui process"
        exit 1
    fi

    # Clean up the service files and other user files
    rm -f /usr/share/dbus-1/services/org.freedesktop.impl.portal.spread.service
    rm -f /usr/lib/systemd/user/spread-portal-ui.service
    rm -f /usr/share/xdg-desktop-portal/portals/spread.portal

    # Make sure the ${USER_RUNTIME_DIR}/doc is umounted
    if mount | grep "${USER_RUNTIME_DIR}/doc"; then
        umount "${USER_RUNTIME_DIR}/doc"
    fi
    rm -rf "${USER_RUNTIME_DIR:?}"/* "${USER_RUNTIME_DIR:?}"/.[!.]*
}

DBUS_SESSION_BUS_ADDRESS=
as_user() {
    if [ -z "$DBUS_SESSION_BUS_ADDRESS" ]; then
        eval "$(su -l -c "XDG_RUNTIME_DIR=\"${USER_RUNTIME_DIR}\" systemctl --user show-environment" test | grep ^DBUS_SESSION_BUS_ADDRESS)"
    fi
    su -l -c "XDG_RUNTIME_DIR=\"${USER_RUNTIME_DIR}\" DBUS_SESSION_BUS_ADDRESS=\"${DBUS_SESSION_BUS_ADDRESS}\" $*" test
}
