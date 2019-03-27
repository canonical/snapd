#!/usr/bin/python3

import os
import sys

import dbus.service
import dbus.mainloop.glib
from gi.repository import GLib

BUS_NAME = "org.freedesktop.portal.Documents"
OBJECT_PATH = "/org/freedesktop/portal/documents"
DOC_IFACE = "org.freedesktop.portal.Documents"

class DocPortal(dbus.service.Object):
    def __init__(self, connection, object_path, logfile, errorfile):
        super(DocPortal, self).__init__(connection, object_path)
        self._logfile = logfile
        self._errorfile = errorfile

    @property
    def _report_error(self):
        with open(self._errorfile, 'r') as fp:
            return 'error' in fp.read()

    @dbus.service.method(dbus_interface=DOC_IFACE, in_signature="",
                         out_signature="ay")
    def GetMountPoint(self):
        with open(self._logfile, "a") as fp:
            fp.write("GetMountPoint called\n")
        if self._report_error:
            raise dbus.DBusException("failure", name=DOC_IFACE + ".Error.Failed")
        return '/run/user/{}/doc\x00'.format(os.getuid()).encode('ASCII')

def main(argv):
    logfile, errorfile = argv[1:]
    dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
    main_loop = GLib.MainLoop()

    bus = dbus.SessionBus()
    # Make sure we quit when the bus shuts down
    bus.add_signal_receiver(
        main_loop.quit, signal_name="Disconnected",
        path="/org/freedesktop/DBus/Local",
        dbus_interface="org.freedesktop.DBus.Local")

    portal = DocPortal(bus, OBJECT_PATH, logfile, errorfile)

    # Allow other services to assume our bus name
    bus_name = dbus.service.BusName(
        BUS_NAME, bus, allow_replacement=True, replace_existing=True,
        do_not_queue=True)

    main_loop.run()

if __name__ == '__main__':
    sys.exit(main(sys.argv))
