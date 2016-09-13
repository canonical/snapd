#!/bin/sh

execute(){
    # FIXME: currently the --list from channel doesn't work
    # echo "Then the new version is available for the snap to be refreshed"
    # expected="$SNAP_NAME +$SNAP_VERSION_PATTERN"
    # snap refresh --list | grep -Pzq "$expected"
    #
    # echo "================================="

    echo "When the snap is refreshed"
    snap refresh --devmode --channel=edge $SNAP_NAME

    echo "Then the new version is listed"
    expected="$SNAP_NAME +$SNAP_VERSION_PATTERN .*devmode"
    snap list | grep -Pzq "$expected"
}
