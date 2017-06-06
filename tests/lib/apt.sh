#!/bin/bash

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB"/pkgdb.sh

install_build_snapd(){
    if [ "$SRU_VALIDATION" = "1" ]; then
        if [[ "$SPREAD_SYSTEM" != ubuntu-* ]]; then
            echo "ERROR: SRU validation is only possible for Ubuntu based systems"
            exit 1
        fi
        apt install -y snapd
        cp /etc/apt/sources.list sources.list.back
        echo "deb http://archive.ubuntu.com/ubuntu/ $(lsb_release -c -s)-proposed restricted main multiverse universe" | tee /etc/apt/sources.list -a
        apt update
        apt install -y --only-upgrade snapd
        mv sources.list.back /etc/apt/sources.list
        apt update
    else
        distro_install_local_package "$GOHOME"/snapd_*.deb
    fi
}
