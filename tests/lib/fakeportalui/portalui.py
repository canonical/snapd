#!/usr/bin/python3

import os
import sys

import dbus.service
import dbus.mainloop.glib
from gi.repository import GLib

BUS_NAME = "org.freedesktop.impl.portal.spread"
OBJECT_PATH = "/org/freedesktop/portal/desktop"

APP_CHOOSER_IFACE = "org.freedesktop.impl.portal.AppChooser"
FILE_CHOOSER_IFACE = "org.freedesktop.impl.portal.FileChooser"
SCREENSHOT_IFACE = "org.freedesktop.impl.portal.Screenshot"


class PortalImpl(dbus.service.Object):
    def __init__(self, connection, object_path, config):
        super(PortalImpl, self).__init__(connection, object_path)
        self._config = config

    @dbus.service.method(
        dbus_interface=FILE_CHOOSER_IFACE,
        in_signature="osssa{sv}",
        out_signature="ua{sv}",
    )
    def OpenFile(self, handle, app_id, parent_window, title, options):
        return (
            0,
            dict(
                uris=dbus.Array(["file:///tmp/file-to-read.txt"], signature="s"),
                writable=False,
            ),
        )

    @dbus.service.method(
        dbus_interface=FILE_CHOOSER_IFACE,
        in_signature="osssa{sv}",
        out_signature="ua{sv}",
    )
    def SaveFile(self, handle, app_id, parent_window, title, options):
        return (
            0,
            dict(
                uris=dbus.Array(["file:///tmp/file-to-write.txt"], signature="s"),
                writable=True,
            ),
        )

    @dbus.service.method(
        dbus_interface=SCREENSHOT_IFACE, in_signature="ossa{sv}", out_signature="ua{sv}"
    )
    def Screenshot(self, handle, app_id, parent_window, options):
        return (0, dict(uri="file:///tmp/screenshot.txt"))

    @dbus.service.method(
        dbus_interface=APP_CHOOSER_IFACE,
        in_signature="ossasa{sv}",
        out_signature="ua{sv}",
    )
    def ChooseApplication(self, handle, app_id, parent_window, choices, options):
        if options["content_type"] == "text/plain":
            return (0, dict(choice="test-editor"))
        else:
            return (1, {})


def main(argv):
    dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
    main_loop = GLib.MainLoop()

    bus = dbus.SessionBus()
    # Make sure we quit when the bus shuts down
    bus.add_signal_receiver(
        main_loop.quit,
        signal_name="Disconnected",
        path="/org/freedesktop/DBus/Local",
        dbus_interface="org.freedesktop.DBus.Local",
    )

    portal = PortalImpl(bus, OBJECT_PATH, None)

    # Allow other services to assume our bus name
    bus_name = dbus.service.BusName(
        BUS_NAME, bus, allow_replacement=True, replace_existing=True, do_not_queue=True
    )

    main_loop.run()


if __name__ == "__main__":
    sys.exit(main(sys.argv))
