#!/usr/bin/env python3

from gi.repository import GLib
import dbus
import dbus.service

from dbus.mainloop.glib import DBusGMainLoop

DBusGMainLoop(set_as_default=True)

class DBusProvider(dbus.service.Object):
    def __init__(self):
        bus = dbus.SystemBus()
        bus_name = dbus.service.BusName("com.ubuntu.connectivity1.NetworkingStatus", bus=bus)
        dbus.service.Object.__init__(self, bus_name, "/com/ubuntu/connectivity1/NetworkingStatus")

    @dbus.service.method(dbus_interface="com.ubuntu.connectivity1.NetworkingStatus",
                         out_signature="s")
    def GetVersion(self):
        return "my-ap-version"

    @dbus.service.method(dbus_interface="com.ubuntu.connectivity1.NetworkingStatus",
                         out_signature="s")
    def GetState(self):
        return "my-ap-state"

if __name__ == "__main__":
    DBusProvider()
    loop = GLib.MainLoop()
    loop.run()
