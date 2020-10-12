#!/bin/bash

make_snap() {
    local SNAP_NAME="$1"
    local SNAP_DIR="${2:-$TESTSLIB/snaps/${SNAP_NAME}}"
    local SNAP_VERSION="${3:-1.0}"

    local META_FILE META_NAME SNAP_FILE
    META_FILE="$SNAP_DIR/meta/snap.yaml"
    if [ ! -f "$META_FILE" ]; then
        echo "snap.yaml file not found for $SNAP_NAME snap"
        return 1
    fi
    META_NAME="$(grep '^name:' "$META_FILE" | awk '{ print $2 }' | tr -d ' ')"
    SNAP_FILE="${SNAP_DIR}/${META_NAME}_${SNAP_VERSION}_all.snap"
    # assigned in a separate step to avoid hiding a failure
    if [ ! -f "$SNAP_FILE" ]; then
        snap pack "$SNAP_DIR" "$SNAP_DIR" >/dev/null
    fi
    # echo the snap name
    if [ -f "$SNAP_FILE" ]; then
        echo "$SNAP_FILE"
    else
        find "$SNAP_DIR" -name "${META_NAME}_*.snap"| head -n1
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

# repack_snapd_deb_into_snapd_snap will re-pack a snapd snap using the assets 
# from the snapd deb installed on the system
repack_snapd_deb_into_snapd_snap() {
    # use snapd from edge as a recent snap that should be close to what we will
    # have in the snapd deb
    snap download snapd --basename=snapd --edge
    unsquashfs -d ./snapd-unpacked snapd.snap
    
    # extract all the files from the snapd deb
    dpkg-deb -x "$SPREAD_PATH"/../snapd_*.deb ./snapd-unpacked

    # repack into the target dir specified
    snap pack --filename=snapd-from-deb.snap  snapd-unpacked "$1"

    # cleanup
    rm -rf snapd-unpacked
}

# repack_snapd_deb_into_core_snap will re-pack a core snap using the assets 
# from the snapd deb installed on the system
repack_snapd_deb_into_core_snap() {
    # use snapd from edge as a recent snap that should be close to what we will
    # have in the snapd deb
    snap download core --basename=core --edge
    unsquashfs -d ./core-unpacked core.snap
    
    # extract all the files from the snapd deb
    dpkg-deb -x "$SPREAD_PATH"/../snapd_*.deb ./core-unpacked

    # repack into the target dir specified
    snap pack --filename=core-from-snapd-deb.snap  core-unpacked "$1"

    # cleanup
    rm -rf core-unpacked
}

# repack_installed_core_snap_into_snapd_snap will re-pack the core snap as the snapd snap,
# using the snapd snap from edge as the set of files to use from the core snap.
# This is primarily meant to be used in UC16 tests that need to use the snapd
# snap because neither the snapd snap, nor the snapd deb built for the spread
# run are seeded on the image
# The build snap is located in the current working directory at with the 
# filename snapd-from-core.snap.
repack_installed_core_snap_into_snapd_snap() {
  # FIXME: maybe build the snapd snap from the deb in prepare_ubuntu_core /
  # setup_reflash_magic and include it somewhere in the image so we don't need
  # to do this hack here?

  # get the snap.yaml and a list of all the snapd snap files using edge
  # NOTE: this may break if a spread run adds files to the snapd snap that
  # don't exist in the snapd snap on edge and those files are necessary
  # for snapd to run or revert, etc.
  snap download snapd --basename=snapd-upstream --edge
  unsquashfs -d ./snapd-upstream snapd-upstream.snap
  ( 
    cd snapd-upstream || exit 1
    # find all files and symlinks - not directories because when unsquashfs
    # is provided a directory it will extract all the files in that directory
    find . \( -type l -o -type f \) | cut -c3- > ../files.txt 
  )

  current=$(readlink /snap/core/current)
  CORE_SNAP=/var/lib/snapd/snaps/core_"$current".snap

  # only unpack files from the core snap that are in the snapd snap - this
  # is kosher because the set of files in the core snap is a superset of 
  # all the files in the snapd snap
  #shellcheck disable=2046
  unsquashfs -d ./snapd-local "$CORE_SNAP" $(cat files.txt)

  # replace snap.yaml from the core snap with the snapd snap, and pack the snap
  cp snapd-upstream/meta/snap.yaml snapd-local/meta/snap.yaml
  snap pack snapd-local --filename=snapd-from-core.snap

  # cleanup the snaps we downloaded and built
  rm -rf snapd-local snapd-upstream* files.txt
}
