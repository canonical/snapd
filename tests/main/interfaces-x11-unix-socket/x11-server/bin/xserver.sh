#!/bin/sh

mkdir -p /tmp/.X11-unix

SOCKET=/tmp/.X11-unix/X0
rm -f $SOCKET
echo "Hello from xserver" | nc -l -w 1 -U $SOCKET
