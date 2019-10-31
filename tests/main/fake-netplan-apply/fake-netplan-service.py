#!/usr/bin/python3

from gi.repository import GLib
import dbus.mainloop.glib
import dbus.service
import sys

BUS_NAME = "io.netplan.Netplan"
OBJECT_PATH = "/io/netplan/Netplan"
DOC_IFACE = "io.netplan.Netplan"


class NetplanApplyService(dbus.service.Object):
    def __init__(self, connection, object_path, logfile):
        super().__init__(connection, object_path)
        self._logfile = logfile

    @dbus.service.method(dbus_interface=DOC_IFACE, in_signature="",
                         out_signature="b")
    def Apply(self):
        # log that we were called and always return True
        with open(self._logfile, "a+") as fp:
            fp.write("Apply called\n")
        return True

    @dbus.service.method(dbus_interface=DOC_IFACE, in_signature="",
                         out_signature="a(sv)")
    def Info(self):
        # log that we were called and always return a dbus struct (i.e. python
        # tuple) with Features in it
        with open(self._logfile, "a+") as fp:
            fp.write("Info called\n")
        return [("Features", ["dhcp-use-domains", "ipv6-mtu"])]


def main(argv):
    logfile = argv[1]
    dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
    main_loop = GLib.MainLoop()

    bus = dbus.SystemBus()
    # Make sure we quit when the bus shuts down
    bus.add_signal_receiver(
        main_loop.quit, signal_name="Disconnected",
        path="/org/freedesktop/DBus/Local",
        dbus_interface="org.freedesktop.DBus.Local")

    NetplanApplyService(bus, OBJECT_PATH, logfile)

    # Allow other services to assume our bus name
    dbus.service.BusName(
        BUS_NAME, bus, allow_replacement=True, replace_existing=True,
        do_not_queue=True)

    main_loop.run()


if __name__ == '__main__':
    sys.exit(main(sys.argv))
