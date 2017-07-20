#!/bin/bash

install_local() {
    local SNAP_NAME="$1"
    shift;
    local SNAP_FILE="$TESTSLIB/snaps/${SNAP_NAME}/${SNAP_NAME}_1.0_all.snap"
    local SNAP_DIR
    # assigned in a separate step to avoid hiding a failure
    SNAP_DIR="$(dirname "$SNAP_FILE")"
    if [ ! -f "$SNAP_FILE" ]; then
        snapbuild "$SNAP_DIR" "$SNAP_DIR"
    fi
    snap install --dangerous "$@" "$SNAP_FILE"
}

install_local_devmode() {
    install_local "$1" --devmode
}

install_local_classic() {
    install_local "$1" --classic
}

# mksnap_fast creates a snap using a faster compress algorithm (gzip)
# than the regular snaps (which are lzma)
mksnap_fast() {
    dir="$1"
    snap="$2"

    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        # trusty does not support  -Xcompression-level 1
        mksquashfs "$dir" "$snap" -comp gzip
    else
        mksquashfs "$dir" "$snap" -comp gzip -Xcompression-level 1
    fi
}

install_generic_consumer() {
    local INTERFACE_NAME="$1"
    cp -ar "$TESTSLIB/snaps/generic-consumer" .
    sed "s/@INTERFACE@/$INTERFACE_NAME/" generic-consumer/meta/snap.yaml.in > generic-consumer/meta/snap.yaml
    snapbuild generic-consumer generic-consumer
    snap install --dangerous generic-consumer/*.snap
    rm -rf generic-consumer
}
