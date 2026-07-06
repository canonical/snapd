#!/usr/bin/env python3

import os
import sys

import gi

gi.require_version("Gio", "2.0")
from gi.repository import Gio, GLib


LOGIN1_BUS_NAME = "org.freedesktop.login1"
LOGIN1_MANAGER_PATH = "/org/freedesktop/login1"
LOGIN1_MANAGER_IFACE = "org.freedesktop.login1.Manager"
LOGIN1_SESSION_IFACE = "org.freedesktop.login1.Session"
DBUS_PROPERTIES_IFACE = "org.freedesktop.DBus.Properties"

TARGET_SESSION_CLASS = "user"

def get_session_class(bus, session_path):
    result = bus.call_sync(
        LOGIN1_BUS_NAME,
        session_path,
        DBUS_PROPERTIES_IFACE,
        "Get",
        GLib.Variant("(ss)", (LOGIN1_SESSION_IFACE, "Class")),
        GLib.VariantType.new("(v)"),
        Gio.DBusCallFlags.NONE,
        -1,
        None,
    )

    return result.unpack()[0]


def main():
    current_uid = os.getuid()

    try:
        bus = Gio.bus_get_sync(Gio.BusType.SYSTEM, None)

        result = bus.call_sync(
            LOGIN1_BUS_NAME,
            LOGIN1_MANAGER_PATH,
            LOGIN1_MANAGER_IFACE,
            "ListSessions",
            None,
            GLib.VariantType.new("(a(susso))"),
            Gio.DBusCallFlags.NONE,
            -1,
            None,
        )

        sessions = result.unpack()[0]
        for session_id, uid, username, seat, session_path in sessions:
            if uid != current_uid:
                continue

            session_class = get_session_class(bus, session_path)
            if session_class == TARGET_SESSION_CLASS:
                print(f"found {TARGET_SESSION_CLASS} session: {session_id} ({username})")
                return 0

        print(f"no current-user sessions with class '{TARGET_SESSION_CLASS}' found")
        return 77

    except GLib.Error as error:
        print(f"failed to query logind: {error.message}", file=sys.stderr)
        return -1


if __name__ == "__main__":
    sys.exit(main())