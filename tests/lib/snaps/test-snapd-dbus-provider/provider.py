#!/usr/bin/env python3
from gi.repository import GLib
import dbus
import dbus.service

from dbus.mainloop.glib import DBusGMainLoop

DBusGMainLoop(set_as_default=True)

class DBusProvider(dbus.service.Object):
    def __init__(self):
        bus = dbus.SessionBus()
        bus_name = dbus.service.BusName("com.dbustest.HelloWorld", bus=bus)
        dbus.service.Object.__init__(self, bus_name, "/com/dbustest/HelloWorld")

    @dbus.service.method(dbus_interface="com.dbustest.HelloWorld",
                         out_signature="s")
    def SayHello(self):
        return "hello world"

if __name__ == "__main__":
    DBusProvider()
    loop = GLib.MainLoop()
    loop.run()
