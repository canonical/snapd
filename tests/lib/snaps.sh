#!/bin/bash

make_snap() {
    local SNAP_NAME="$1"
    shift;
    local SNAP_FILE="$TESTSLIB/snaps/${SNAP_NAME}/${SNAP_NAME}_1.0_all.snap"
    local SNAP_DIR
    # assigned in a separate step to avoid hiding a failure
    SNAP_DIR="$(dirname "$SNAP_FILE")"
    if [ ! -f "$SNAP_FILE" ]; then
        snap pack "$SNAP_DIR" "$SNAP_DIR" >/dev/null
    fi
    # echo the snap name
    if [ -f "$SNAP_FILE" ]; then
        echo "$SNAP_FILE"
    else
        find "$TESTSLIB/snaps/${SNAP_NAME}" -name '*.snap' | head -n1
    fi
}

install_local() {
    local SNAP_NAME="$1"
    shift
    SNAP_FILE=$(make_snap "$SNAP_NAME")

    snap install --dangerous "$@" "$SNAP_FILE"
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

    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        # trusty does not support  -Xcompression-level 1
        mksquashfs "$dir" "$snap" -comp gzip -no-fragments
    else
        mksquashfs "$dir" "$snap" -comp gzip -Xcompression-level 1 -no-fragments
    fi
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
    case "$SPREAD_SYSTEM" in
        ubuntu-core-16-*)
            return 1
            ;;
        ubuntu-*|debian-*)
            return 0
            ;;
        fedora-*)
            return 1
            ;;
        opensuse-*)
            return 0
            ;;
        *)
            return 0
            ;;
    esac
}
