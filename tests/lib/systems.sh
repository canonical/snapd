#!/bin/bash

is_core_system(){
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-* ]]; then
        return 0
    fi
    return 1
}

is_core16_system(){
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
        return 0
    fi
    return 1
}

is_core18_system(){
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-18-* ]]; then
        return 0
    fi
    return 1
}

is_core20_system(){
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-20-* ]]; then
        return 0
    fi
    return 1
}

is_classic_system(){
    if [[ "$SPREAD_SYSTEM" != ubuntu-core-* ]]; then
        return 0
    fi
    return 1
}


is_ubuntu_14_system(){
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        return 0
    fi
    return 1
}

get_snap_for_system(){
    local snap=$1

    case "$SPREAD_SYSTEM" in
        ubuntu-core-18-*)
            echo "${snap}-core18"
            ;;
        ubuntu-core-20-*)
            echo "${snap}-core20"
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
        *)
            echo "core"
            ;;
    esac
}

is_cgroupv2() {
    test "$(stat -f -c '%T' /sys/fs/cgroup)" = "cgroup2fs"
}
