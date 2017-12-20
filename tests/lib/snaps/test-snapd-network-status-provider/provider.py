#!/usr/bin/env python3

from gi.repository import GLib
import dbus
import dbus.service

from dbus.mainloop.glib import DBusGMainLoop

INTERFACE = 'com.ubuntu.connectivity1.NetworkingStatus'
PATH = '/com/ubuntu/connectivity1/NetworkingStatus'

DBusGMainLoop(set_as_default=True)


class DBusProvider(dbus.service.Object):
    def __init__(self):
        bus = dbus.SystemBus()
        bus_name = dbus.service.BusName(INTERFACE, bus=bus)
        dbus.service.Object.__init__(self, bus_name, PATH)

    @dbus.service.method(dbus_interface=INTERFACE, out_signature="s")
    def GetVersion(self):
        return "my-ap-version"

    @dbus.service.method(dbus_interface=INTERFACE, out_signature="s")
    def GetState(self):
        return "my-ap-state"


if __name__ == "__main__":
    DBusProvider()
    loop = GLib.MainLoop()
    loop.run()
