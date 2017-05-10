#!/bin/bash

apt_install_local() {
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        # relying on dpkg as apt(-get) does not support installation from local files in trusty.
        dpkg -i --force-depends --auto-deconfigure --force-depends-version "$@"
        apt-get -f install -y
    else
        apt install -y "$@"
    fi
}

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
        apt_install_local ${GOPATH}/snapd_*.deb
    fi
}
