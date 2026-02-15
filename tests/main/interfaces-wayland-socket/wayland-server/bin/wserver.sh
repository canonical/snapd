#!/bin/sh

mkdir -p "$XDG_RUNTIME_DIR"

SOCKET="$XDG_RUNTIME_DIR/wayland-9"
rm -f "$SOCKET"
echo "Hello from wayland-server" | nc -l -w 1 -U "$SOCKET"
