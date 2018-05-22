#!/bin/sh

is_ubuntu_core_system(){
    if [[ "$SPREAD_SYSTEM" == ubuntu-core-16-* ]]; then
        return 0
    fi
    return 1
}

is_classic_system(){
    if [[ "$SPREAD_SYSTEM" != ubuntu-core-16-* ]]; then
        return 0
    fi
    return 1
}
