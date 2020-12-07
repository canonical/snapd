#!/bin/sh

exec dbus-send --session --print-reply \
    --dest=io.snapcraft.Launcher /io/snapcraft/Launcher \
    io.snapcraft.PrivilegedDesktopLauncher.OpenDesktopEntry \
    string:"$1"
