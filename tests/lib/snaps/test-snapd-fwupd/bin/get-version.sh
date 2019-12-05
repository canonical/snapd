#!/bin/sh

exec dbus-send --print-reply --system \
     --dest=org.freedesktop.fwupd / \
     org.freedesktop.DBus.Properties.Get \
     string:org.freedesktop.fwupd string:DaemonVersion
