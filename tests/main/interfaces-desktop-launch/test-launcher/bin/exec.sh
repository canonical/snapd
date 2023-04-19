#!/bin/sh

set -e

# Extract command line from desktop file
desktop_file="/var/lib/snapd/desktop/applications/$1"
cmdline="$(sed -n 's/^Exec=\(.*\)$/\1/p' "$desktop_file")"
# filter out the file argument
cmdline="$(echo "$cmdline" | sed 's/%[uUfF]//g')"

exec $cmdline
