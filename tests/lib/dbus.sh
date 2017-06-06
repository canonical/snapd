#!/bin/bash
DISPLAY=:0

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

init_dbus_env(){
    export $(cat dbus.env | xargs)
}

start_dbus_loop_unit(){
    local executable="$1"

    distro_install_package dbus-x11

    dbus-launch > dbus.env
    init_dbus_env
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        cat <<EOF > /etc/init/dbus-provider.conf
env DISPLAY="$DISPLAY"
env DBUS_SESSION_BUS_ADDRESS="$DBUS_SESSION_BUS_ADDRESS"
env DBUS_SESSION_BUS_PID="$DBUS_SESSION_BUS_PID"
script
    $executable
end script
EOF
        initctl reload-configuration
        start dbus-provider
    else
        systemd-run --unit dbus-provider \
                    --setenv=DISPLAY=$DISPLAY \
                    --setenv=DBUS_SESSION_BUS_ADDRESS=$DBUS_SESSION_BUS_ADDRESS \
                    --setenv=DBUS_SESSION_BUS_PID=$DBUS_SESSION_BUS_PID \
                    $executable
    fi
}

stop_dbus_loop_unit(){
    rm -f dbus.env
    distro_purge_package dbus-x11
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        stop dbus-provider
        rm -f /etc/init/dbus-provider.conf
    else
        systemctl stop dbus-provider
    fi
}
