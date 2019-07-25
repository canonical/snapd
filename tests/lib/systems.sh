#!/bin/bash

is_core_system(){
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-* ]]; then
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
    snap=$1
    system=${2:-$SPREAD_SYSTEM}

    case "$system" in
        ubuntu-core-18-*)
            echo "${snap}-core18"
            ;;
        ubuntu-core-20-*)
            echo "${snap}-core20"
            ;;
        *)
            echo "$snap"
    esac
}

get_core_for_system(){
    system=${1:-$SPREAD_SYSTEM}

    case "$system" in
        ubuntu-core-18-*)
            echo "core18"
            ;;
        ubuntu-core-20-*)
            echo "core20"
            ;;
        *)
            echo "core"
    esac
}
