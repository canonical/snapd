#!/bin/bash

get_snap_for_system(){
    local snap=$1

    case "$SPREAD_SYSTEM" in
        ubuntu-core-18-*)
            echo "${snap}-core18"
            ;;
        ubuntu-core-20-*)
            echo "${snap}-core20"
            ;;
        ubuntu-core-22-*)
            echo "${snap}-core22"
            ;;
        *)
            echo "$snap"
            ;;
    esac
}

get_core_for_system(){
    case "$SPREAD_SYSTEM" in
        ubuntu-core-18-*)
            echo "core18"
            ;;
        ubuntu-core-20-*)
            echo "core20"
            ;;
        ubuntu-core-22-*)
            echo "core22"
            ;;
        *)
            echo "core"
            ;;
    esac
}

is_cgroupv2() {
    test "$(stat -f -c '%T' /sys/fs/cgroup)" = "cgroup2fs"
}
