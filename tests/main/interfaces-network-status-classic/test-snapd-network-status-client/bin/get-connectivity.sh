#!/bin/sh

exec dbus-send --print-reply --session \
     --dest=org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop \
     org.freedesktop.portal.NetworkMonitor.GetConnectivity
