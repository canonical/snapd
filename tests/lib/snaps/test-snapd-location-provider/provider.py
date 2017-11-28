#!/usr/bin/env python3

from gi.repository import GLib
import dbus
import dbus.service

from dbus.mainloop.glib import DBusGMainLoop

DBusGMainLoop(set_as_default=True)


class DBusProvider(dbus.service.Object):
    def __init__(self):
        bus = dbus.SystemBus()
        bus_name = dbus.service.BusName("com.ubuntu.location.Service", bus=bus)
        dbus.service.Object.__init__(self, bus_name, "/com/ubuntu/location/Service")

    @dbus.service.method(dbus_interface="com.ubuntu.location.Service",
                         out_signature="s")
    def Get(self):
        return "location-get"

    @dbus.service.method(dbus_interface="com.ubuntu.location.Service",
                         out_signature="s")
    def Set(self):
        return "location-set"


if __name__ == "__main__":
    DBusProvider()
    loop = GLib.MainLoop()
    loop.run()
