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
