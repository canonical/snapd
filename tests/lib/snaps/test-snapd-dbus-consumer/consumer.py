#!/usr/bin/env python3
import dbus
import sys

def run():
    obj = dbus.SessionBus().get_object("com.dbustest.HelloWorld", "/com/dbustest/HelloWorld")
    print(obj.SayHello(dbus_interface="com.dbustest.HelloWorld"))

if __name__ == "__main__":
    sys.exit(run())
