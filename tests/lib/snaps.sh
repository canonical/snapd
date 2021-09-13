#!/bin/bash

# mksnap_fast creates a snap using a faster compress algorithm (gzip)
# than the regular snaps (which are lzma)
mksnap_fast() {
    dir="$1"
    snap="$2"

    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*|amazon-*|centos-*)
            # trusty, AMZN2 and CentOS 7 do not support -Xcompression-level 1
            mksquashfs "$dir" "$snap" -comp gzip -no-fragments -no-progress
            ;;
        *)
            mksquashfs "$dir" "$snap" -comp gzip -Xcompression-level 1 -no-fragments -no-progress
            ;;
    esac
}

install_generic_consumer() {
    local INTERFACE_NAME="$1"
    cp -ar "$TESTSLIB/snaps/generic-consumer" .
    sed "s/@INTERFACE@/$INTERFACE_NAME/" generic-consumer/meta/snap.yaml.in > generic-consumer/meta/snap.yaml
    snap pack generic-consumer generic-consumer
    snap install --dangerous generic-consumer/*.snap
    rm -rf generic-consumer
}

