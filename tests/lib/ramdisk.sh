#!/bin/bash

setup_ramdisk(){
    if [ ! -e /dev/ram0 ]; then
        mknod -m 660 /dev/ram0 b 1 0
        chown root:disk /dev/ram0
    fi
}
