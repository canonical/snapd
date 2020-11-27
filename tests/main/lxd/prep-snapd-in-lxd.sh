#!/bin/sh

set -ex

# XXX: remove once the "umount /snap" change in postrm has propagated all
#      the way to the image
if [ -e /var/lib/dpkg/info/snapd.postrm ]; then
    # ensure we can umount /snap
    sed -i 's#echo "Final directory cleanup"#umount /snap || true#' /var/lib/dpkg/info/snapd.postrm
fi

# wait for cloud-init to finish before doing any apt operations, since it will
# re-write the apt sources.list file and we will be racing with the re-write 
# trying to do apt operations before cloud-init is done
# TODO: we should eventually use `cloud-init status --wait`, but that doesn't work
# in nested containers, see https://bugs.launchpad.net/cloud-init/+bug/1905493
for _ in $(seq 1 60); do
    if python3 -c "import apt;apt.apt_pkg.SourceList().read_main_list()"; then
        break
    fi
    sleep 1
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
