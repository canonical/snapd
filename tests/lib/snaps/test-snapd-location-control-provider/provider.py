#!/usr/bin/env python3

from gi.repository import GLib
import dbus
import dbus.service

from dbus.mainloop.glib import DBusGMainLoop

INTERFACE = 'org.freedesktop.DBus.Properties'
PATH = '/com/ubuntu/location/Service'

DBusGMainLoop(set_as_default=True)


class DBusProvider(dbus.service.Object):
    def __init__(self):
        bus = dbus.SystemBus()
        bus_name = dbus.service.BusName(INTERFACE, bus=bus)
        dbus.service.Object.__init__(self, bus_name, PATH)

    @dbus.service.method(dbus_interface=INTERFACE, out_signature="s")
    def Get(self, interface_name, property_name):
        return 'location-get'


if __name__ == "__main__":
    DBusProvider()
    loop = GLib.MainLoop()
    loop.run()
