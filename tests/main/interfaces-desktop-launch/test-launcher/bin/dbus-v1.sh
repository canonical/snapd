#!/bin/sh

exec busctl --user call \
    io.snapcraft.Launcher /io/snapcraft/PrivilegedDesktopLauncher \
    io.snapcraft.PrivilegedDesktopLauncher OpenDesktopEntry "s" \
    "$1"
