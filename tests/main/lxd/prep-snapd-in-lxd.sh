#!/bin/sh

set -ex

# XXX: remove once the "umount /snap" change in postrm has propagated all
#      the way to the image
if [ -e /var/lib/dpkg/info/snapd.postrm ]; then
    # ensure we can umount /snap
    sed -i 's#echo "Final directory cleanup"#umount /snap || true#' /var/lib/dpkg/info/snapd.postrm
fi

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
