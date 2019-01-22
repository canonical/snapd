#!/usr/bin/env python3
import sys
from gi.repository import GLib
import dbus
import dbus.service

from dbus.mainloop.glib import DBusGMainLoop

DBusGMainLoop(set_as_default=True)

class DBusProvider(dbus.service.Object):
    def __init__(self, bus):
        bus_name = dbus.service.BusName("com.dbustest.HelloWorld", bus=bus)
        dbus.service.Object.__init__(self, bus_name, "/com/dbustest/HelloWorld")

    @dbus.service.method(dbus_interface="com.dbustest.HelloWorld",
                         out_signature="s")
    def SayHello(self):
        return "hello world"

if __name__ == "__main__":
    if sys.argv[1] == "system":
        bus = dbus.SystemBus()
    else:
        bus = dbus.SessionBus()
    DBusProvider(bus)
    loop = GLib.MainLoop()
    loop.run()
