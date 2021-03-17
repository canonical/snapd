#!/bin/sh

exec dbus-send "$@" --print-reply \
    --dest=io.snapcraft.SnapDbusService /io/snapcraft/SnapDbusService  \
    io.snapcraft.ExampleInterface.ExampleMethod
