#!/bin/sh
set -eu

bus_name=org.freedesktop.impl.portal.PermissionStore
object_path=/org/freedesktop/impl/portal/PermissionStore
iface_name=org.freedesktop.impl.portal.PermissionStore

# Exercise the DBus.Peer rule.
dbus-send --session --print-reply \
    --dest="$bus_name" \
    "$object_path" \
    org.freedesktop.DBus.Peer.Ping >/dev/null

# Exercise the DBus.Introspectable rule.
dbus-send --session --print-reply \
    --dest="$bus_name" \
    "$object_path" \
    org.freedesktop.DBus.Introspectable.Introspect | grep -q "$iface_name"

# Exercise the DBus.Properties rule.
dbus-send --session --print-reply \
    --dest="$bus_name" \
    "$object_path" \
    org.freedesktop.DBus.Properties.GetAll \
    string:"$iface_name" | grep -q 'uint32 1'

# Exercise the PermissionStore interface rule.
dbus-send --session --print-reply \
    --dest="$bus_name" \
    "$object_path" \
    "$iface_name".Test | grep -q 'string "ok"'

echo ok
