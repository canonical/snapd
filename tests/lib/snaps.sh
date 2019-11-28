#!/bin/bash

make_snap() {
    local SNAP_NAME="$1"
    local SNAP_DIR="$TESTSLIB/snaps/${SNAP_NAME}"
    if [ $# -gt 1 ]; then
        SNAP_DIR="$2"
    fi
    local SNAP_FILE="${SNAP_DIR}/${SNAP_NAME}_1.0_all.snap"
    # assigned in a separate step to avoid hiding a failure
    if [ ! -f "$SNAP_FILE" ]; then
        snap pack "$SNAP_DIR" "$SNAP_DIR" >/dev/null || return 1
    fi
    # echo the snap name
    if [ -f "$SNAP_FILE" ]; then
        echo "$SNAP_FILE"
    else
        find "$SNAP_DIR" -name '*.snap' | head -n1
    fi
}

install_local() {
    local SNAP_NAME="$1"
    local SNAP_DIR="$TESTSLIB/snaps/${SNAP_NAME}"
    shift

    if [ -d "$SNAP_NAME" ]; then
        SNAP_DIR="$PWD/$SNAP_NAME"
    fi
    SNAP_FILE=$(make_snap "$SNAP_NAME" "$SNAP_DIR")

    snap install --dangerous "$@" "$SNAP_FILE"
}

install_local_as() {
    local snap="$1"
    local name="$2"
    shift 2
    install_local "$snap" --name "$name" "$@"
}

install_local_devmode() {
    install_local "$1" --devmode
}

install_local_classic() {
    install_local "$1" --classic
}

install_local_jailmode() {
    install_local "$1" --jailmode
}

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

is_classic_confinement_supported() {
    if snap debug sandbox-features --required=confinement-options:classic; then
        return 0
    fi
    return 1
}
