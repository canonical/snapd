#!/bin/sh

apt_install_local_packages() {
    if [ "$SPREAD_SYSTEM" = "ubuntu-14.04-64" ]; then
        # relying on dpkg as apt(-get) does not support installation from local files in trusty.
        dpkg -i "$@" || apt-get -f install -y
    else
        apt install -y "$@"
    fi
}
