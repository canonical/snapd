#!/usr/bin/env python3
import dbus
import sys


def run(bus):
    obj = bus.get_object("com.dbustest.HelloWorld", "/com/dbustest/HelloWorld")
    print(obj.SayHello(dbus_interface="com.dbustest.HelloWorld"))


if __name__ == "__main__":
    if sys.argv[1] == "system":
        bus = dbus.SystemBus()
    else:
        bus = dbus.SessionBus()
    run(bus)
