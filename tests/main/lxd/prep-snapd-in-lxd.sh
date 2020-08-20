#!/bin/sh

set -ex

# ensure /snap can be removed by the "apt purge snapd" later
umount /snap || true

apt autoremove --purge -y snapd ubuntu-core-launcher
apt update

# requires the snapd deb to already have been "lxd file push"d into the 
# container
apt install -y /root/snapd_*.deb

# reload to take effect of the proxy that may have been set before this script
systemctl daemon-reload
systemctl restart snapd.service

# wait for snapd to finish seeding
snap wait system seed.loaded

# for debugging
cat /etc/environment
