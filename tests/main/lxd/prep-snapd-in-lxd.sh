#!/bin/sh

set -ex

# XXX: remove once the "umount /snap" change in postrm has propagated all
#      the way to the image
if [ -e /var/lib/dpkg/info/snapd.postrm ]; then
    # ensure we can umount /snap
    sed -i 's#echo "Final directory cleanup"#umount /snap || true#' /var/lib/dpkg/info/snapd.postrm
fi

# wait until we can do a DNS lookup on ubuntu.com before doing any apt 
# operations, as the lxd container sometimes sees really weird errors about the 
# apt sources.list such as:
#
# Reading package lists...
# E: Type 'ubuntu' is not known on line 50 in source list /etc/apt/sources.list
# E: The list of sources could not be read.
#
# waiting like this seems to resolve the race condition wherever it is in apt 

#shellcheck disable=2034
for i in $(seq 1 60); do
    if  nslookup -timeout=1 archive.ubuntu.com; then
        break
    fi
    sleep 0.1
done

apt autoremove --purge -y snapd ubuntu-core-launcher
apt update

# requires the snapd deb to already have been "lxd file push"d into the 
# container
apt install -y /root/snapd_*.deb

# reload to take effect of the proxy that may have been set before this script
# XXX: systemctl daemon-reload times out in 16.04:my-nesting-lxd but every
#      appears to be working normally
systemctl daemon-reload || true
systemctl restart snapd.service

# wait for snapd to finish seeding
snap wait system seed.loaded

# for debugging
cat /etc/environment
