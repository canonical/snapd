#!/bin/bash

. $TESTSLIB/pkgdb.sh

install_build_snapd(){
    if [ "$SRU_VALIDATION" = "1" ]; then
        apt install -y snapd
        cp /etc/apt/sources.list sources.list.back
        echo "deb http://archive.ubuntu.com/ubuntu/ $(lsb_release -c -s)-proposed restricted main multiverse universe" | tee /etc/apt/sources.list -a
        apt update
        apt install -y --only-upgrade snapd
        mv sources.list.back /etc/apt/sources.list
        apt update
    else
        packages=
        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                packages="${GOPATH}/snapd_*.deb"
                ;;
            fedora-*)
                packages="${GOPATH}/snap-confine*.rpm ${GOPATH}/snapd*.rpm"
                ;;
            *)
                exit 1
                ;;
        esac
        distro_install_local_package $packages
    fi
}
