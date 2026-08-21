#!/bin/sh


mkdir -p "$XDG_RUNTIME_DIR"
SOCKET="$XDG_RUNTIME_DIR/../wayland-9"

exec nc -w 30 -U "$SOCKET"
