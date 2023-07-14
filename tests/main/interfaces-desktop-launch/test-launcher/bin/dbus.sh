#!/bin/sh

exec dbus-send --session --print-reply \
    --dest=io.snapcraft.Launcher /io/snapcraft/PrivilegedDesktopLauncher \
    io.snapcraft.PrivilegedDesktopLauncher.OpenDesktopEntry2 \
    string:"$1" string:"" array:string:"$2" {}
