#!/usr/bin/python3

import os
import sys

import dbus.service
import dbus.mainloop.glib
from gi.repository import GLib

BUS_NAME = "org.freedesktop.portal.Desktop"
OBJECT_PATH = "/org/freedesktop/portal/desktop"

NETWORK_MONITOR_IFACE = "org.freedesktop.portal.NetworkMonitor"


class Portal(dbus.service.Object):
    def __init__(self, connection, object_path, config):
        super(Portal, self).__init__(connection, object_path)
        self._config = config

    @dbus.service.method(
        dbus_interface=NETWORK_MONITOR_IFACE,
        in_signature="",
        out_signature="b",
    )
    def GetAvailable(self):
        return True

    @dbus.service.method(
        dbus_interface=NETWORK_MONITOR_IFACE,
        in_signature="",
        out_signature="b",
    )
    def GetMetered(self):
        return False

    @dbus.service.method(
        dbus_interface=NETWORK_MONITOR_IFACE,
        in_signature="",
        out_signature="u",
    )
    def GetConnectivity(self):
        return dbus.UInt32(4) # Full Network

    @dbus.service.method(
        dbus_interface=NETWORK_MONITOR_IFACE,
        in_signature="",
        out_signature="a{sv}",
    )
    def GetStatus(self):
        return dict(
            available=self.GetAvailable(),
            metered=self.GetMetered(),
            connectivity=self.GetConnectivity(),
        )

    @dbus.service.method(
        dbus_interface=NETWORK_MONITOR_IFACE,
        in_signature="su",
        out_signature="b",
    )
    def CanReach(self, hostname, port):
        return True


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

    portal = Portal(bus, OBJECT_PATH, None)

    # Allow other services to assume our bus name
    bus_name = dbus.service.BusName(
        BUS_NAME, bus, allow_replacement=True, replace_existing=True, do_not_queue=True
    )

    main_loop.run()


if __name__ == "__main__":
    sys.exit(main(sys.argv))
