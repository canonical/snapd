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
