#!/bin/bash

start_dbus_unit(){
    local executable="$1"

    dbus-launch > dbus.env
    # shellcheck disable=SC2046
    export $(cat dbus.env)
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        cat <<EOF > /etc/init/dbus-provider.conf
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
                    --setenv=DBUS_SESSION_BUS_ADDRESS="$DBUS_SESSION_BUS_ADDRESS" \
                    --setenv=DBUS_SESSION_BUS_PID="$DBUS_SESSION_BUS_PID" \
                    "$executable"
    fi
}

stop_dbus_unit(){
    rm -f dbus.env
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        stop dbus-provider
        rm -f /etc/init/dbus-provider.conf
    else
        systemctl stop dbus-provider || true
    fi
}
