#!/bin/sh
if [ "$(find /snap/snapd/ -maxdepth 1 -type d 2>/dev/null | wc -l)" -gt 2 ]; then
    snap revert snapd
fi
EOF
