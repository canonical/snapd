#!/usr/bin/python3

import sys
from urllib.parse import urlparse, unquote

import dbus
import dbus.mainloop.glib
from gi.repository import GLib


class Client:
    def __init__(self, bus, main_loop):
        self._bus = bus
        self._main_loop = main_loop
        self._request_path = None
        self._response = None
        self._results = None
        bus.add_signal_receiver(
            self._response_cb,
            signal_name="Response",
            dbus_interface="org.freedesktop.portal.Request",
            bus_name="org.freedesktop.portal.Desktop",
            path_keyword="object_path",
        )
        self._portal = self._bus.get_object(
            "org.freedesktop.portal.Desktop",
            "/org/freedesktop/portal/desktop",
            introspect=False,
        )

    def _response_cb(self, response, results, object_path):
        if object_path == self._request_path:
            self._response = response
            self._results = results
            self._main_loop.quit()

    def run(self, args):
        command_name = args.pop(0).replace("-", "_")
        command = getattr(self, "cmd_{}".format(command_name), None)
        if command is None:
            sys.stderr.write("Unknown command: {}\n".format(command_name))
            sys.exit(1)
        return command(*args)

    def cmd_open_file(self):
        iface = dbus.Interface(self._portal, "org.freedesktop.portal.FileChooser")
        self._request_path = iface.OpenFile(
            "", "test open file", dbus.Dictionary(signature="sv")
        )
        self._main_loop.run()
        if self._response == 0:
            for uri in self._results["uris"]:
                parsed = urlparse(uri)
                assert parsed.scheme == "file"
                with open(unquote(parsed.path), "r") as fp:
                    sys.stdout.write(fp.read())
        elif self._response == 1:
            # user cancelled
            sys.stderr.write("request cancelled\n")
            return 1
        else:
            sys.stderr.write("request failed\n")
            return 1

    def cmd_save_file(self, content):
        iface = dbus.Interface(self._portal, "org.freedesktop.portal.FileChooser")
        self._request_path = iface.SaveFile(
            "", "test save file", dbus.Dictionary(signature="sv")
        )
        self._main_loop.run()
        if self._response == 0:
            uris = self._results["uris"]
            assert len(uris) == 1
            parsed = urlparse(uris[0])
            assert parsed.scheme == "file"
            with open(unquote(parsed.path), "w") as fp:
                fp.write(content)
        elif self._response == 1:
            # user cancelled
            sys.stderr.write("request cancelled\n")
            return 1
        else:
            sys.stderr.write("request failed\n")
            return 1

    def cmd_open_uri(self, uri):
        iface = dbus.Interface(self._portal, "org.freedesktop.portal.OpenURI")
        self._request_path = iface.OpenURI("", uri, dbus.Dictionary(signature="sv"))
        self._main_loop.run()
        if self._response == 0:
            pass
        elif self._response == 1:
            # user cancelled
            sys.stderr.write("request cancelled\n")
            return 1
        else:
            sys.stderr.write("request failed\n")
            return 1

    def cmd_launch_file(self, filename):
        iface = dbus.Interface(self._portal, "org.freedesktop.portal.OpenURI")
        with open(filename, "rb") as fp:
            self._request_path = iface.OpenFile(
                "", dbus.types.UnixFd(fp), dbus.Dictionary(signature="sv")
            )
        self._main_loop.run()
        if self._response == 0:
            pass
        elif self._response == 1:
            # user cancelled
            sys.stderr.write("request cancelled\n")
            return 1
        else:
            sys.stderr.write("request failed\n")
            return 1

    def cmd_screenshot(self):
        iface = dbus.Interface(self._portal, "org.freedesktop.portal.Screenshot")
        self._request_path = iface.Screenshot("", dbus.Dictionary(signature="sv"))
        self._main_loop.run()
        if self._response == 0:
            uri = self._results["uri"]
            parsed = urlparse(uri)
            assert parsed.scheme == "file"
            with open(unquote(parsed.path), "rb") as fp:
                sys.stdout.buffer.write(fp.read())
        elif self._response == 1:
            # user cancelled
            sys.stderr.write("request cancelled\n")
            return 1
        else:
            sys.stderr.write("request failed\n")
            return 1


def main(argv):
    dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
    main_loop = GLib.MainLoop()
    bus = dbus.SessionBus()

    client = Client(bus, main_loop)
    return client.run(argv[1:])


if __name__ == "__main__":
    sys.exit(main(sys.argv))
