#!/bin/sh
set -eu

command="${1:-all}"
bus_name="${DBUS_BUS_NAME}"
object_path="${DBUS_OBJECT_PATH}"
iface_name="${DBUS_IFACE_NAME}"

cmd_ping() {
    # Use --print-reply so dbus-send performs a method call and waits for the
    # reply. We discard the reply body because for Ping we only care about the
    # success or failure of the round trip.
    dbus-send --session --print-reply \
        --dest="$bus_name" \
        "$object_path" \
        org.freedesktop.DBus.Peer.Ping >/dev/null
    echo ok
}

cmd_introspect() {
    dbus-send --session --print-reply \
        --dest="$bus_name" \
        "$object_path" \
        org.freedesktop.DBus.Introspectable.Introspect | grep -q "$iface_name"
    echo ok
}

cmd_get_all() {
    dbus-send --session --print-reply \
        --dest="$bus_name" \
        "$object_path" \
        org.freedesktop.DBus.Properties.GetAll \
        string:"$iface_name" | grep -q 'uint32 1'
    echo ok
}

cmd_test() {
    dbus-send --session --print-reply \
        --dest="$bus_name" \
        "$object_path" \
        "$iface_name".Test | grep -q 'string "ok"'
    echo ok
}

case "$command" in
    ping)
        cmd_ping
        ;;
    introspect)
        cmd_introspect
        ;;
    get-all)
        cmd_get_all
        ;;
    test)
        cmd_test
        ;;
    all)
        cmd_ping
        cmd_introspect
        cmd_get_all
        cmd_test
        ;;
    *)
        echo "unknown command: $command" >&2
        exit 1
        ;;
esac
