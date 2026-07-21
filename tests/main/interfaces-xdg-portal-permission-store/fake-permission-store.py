#!/usr/bin/env python3

import os
import sys

import dbus
import dbus.mainloop.glib
import dbus.service
from gi.repository import GLib

BUS_NAME = os.environ["DBUS_BUS_NAME"]
OBJECT_PATH = os.environ["DBUS_OBJECT_PATH"]
PERMISSION_STORE_IFACE = os.environ["DBUS_IFACE_NAME"]

INTROSPECTION_XML_TEMPLATE = """<!DOCTYPE node PUBLIC '-//freedesktop//DTD D-Bus Object Introspection 1.0//EN'
 'http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd'>
<node>
  <interface name='{iface_name}'>
    <method name='Test'>
      <arg type='s' direction='out'/>
    </method>
  </interface>
  <interface name='org.freedesktop.DBus.Peer'>
    <method name='Ping'/>
  </interface>
  <interface name='org.freedesktop.DBus.Introspectable'>
    <method name='Introspect'>
      <arg type='s' direction='out'/>
    </method>
  </interface>
  <interface name='org.freedesktop.DBus.Properties'>
    <method name='GetAll'>
      <arg type='s' direction='in'/>
      <arg type='a{{sv}}' direction='out'/>
    </method>
  </interface>
</node>
"""


class PermissionStore(dbus.service.Object):
    def __init__(self, connection, object_path):
        super().__init__(connection, object_path)
        self._introspection_xml = INTROSPECTION_XML_TEMPLATE.format(
            iface_name=PERMISSION_STORE_IFACE
        )

    @dbus.service.method(
        dbus_interface=PERMISSION_STORE_IFACE,
        in_signature="",
        out_signature="s",
    )
    def Test(self):
        return "ok"

    @dbus.service.method(
        dbus_interface="org.freedesktop.DBus.Peer",
        in_signature="",
        out_signature="",
    )
    def Ping(self):
        return None

    @dbus.service.method(
        dbus_interface="org.freedesktop.DBus.Introspectable",
        in_signature="",
        out_signature="s",
    )
    def Introspect(self):
        return self._introspection_xml

    @dbus.service.method(
        dbus_interface="org.freedesktop.DBus.Properties",
        in_signature="s",
        out_signature="a{sv}",
    )
    def GetAll(self, iface):
        if iface != PERMISSION_STORE_IFACE:
            return dbus.Dictionary({}, signature="sv")
        return dbus.Dictionary({"version": dbus.UInt32(1)}, signature="sv")


def main(argv):
    dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
    main_loop = GLib.MainLoop()

    bus = dbus.SessionBus()
    bus.add_signal_receiver(
        main_loop.quit,
        signal_name="Disconnected",
        path="/org/freedesktop/DBus/Local",
        dbus_interface="org.freedesktop.DBus.Local",
    )

    bus_name = dbus.service.BusName(
        BUS_NAME,
        bus,
        allow_replacement=True,
        replace_existing=True,
        do_not_queue=True,
    )
    PermissionStore(bus, OBJECT_PATH)

    main_loop.run()
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
