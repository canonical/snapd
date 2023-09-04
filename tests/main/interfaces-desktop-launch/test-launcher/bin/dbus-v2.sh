#!/bin/bash

action=""
while getopts "a:" arg; do
    case "$arg" in
        a)
            action="$OPTARG"
            ;;
        *)
            echo "Unexpected option $arg" >&2
            exit 1
    esac
done
shift $((OPTIND-1))

desktop="$1"
shift

exec busctl --user call \
    io.snapcraft.Launcher /io/snapcraft/PrivilegedDesktopLauncher \
    io.snapcraft.PrivilegedDesktopLauncher OpenDesktopEntry2 "ssasa{ss}" \
    "$desktop" "$action" $# "$@" \
    2 DESKTOP_STARTUP_ID "$DESKTOP_STARTUP_ID" \
      XDG_ACTIVATION_TOKEN "$XDG_ACTIVATION_TOKEN"
