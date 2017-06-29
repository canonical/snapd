#!/usr/bin/env python3
import dbus
import sys

def run():
    obj = dbus.SystemBus().get_object("org.freedesktop.hostname1", "/org/freedesktop/hostname1")
    print(obj.Introspect(dbus_interface="org.freedesktop.DBus.Introspectable"))

if __name__ == "__main__":
    sys.exit(run())
