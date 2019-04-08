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

get_architecture(){
    local curr_arch
    curr_arch="$(uname -m)"

    case "$curr_arch" in
    x86_64)
        echo 'amd64'
        ;;
    i386)
        echo 'i386'
        ;;
    armv7l)
        echo 'armhf'
        ;;
    aarch64*)
        echo 'arm64'
        ;;
    ppc64*)
        echo 'ppc64el'
        ;;
    s390*)
        echo 's390x'
        ;;
    *)
        echo "architecture not supported"
        exit 1
        ;;
    esac
}

