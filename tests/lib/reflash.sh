#!/bin/bash

# flash.sh: Install a flashing process to reboot to a new disk image
#
# Copyright (C) 2023-2024 Canonical Ltd
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program.  If not, see <https://www.gnu.org/licenses/>.
#
# This produces an Ubuntu Core image for testing in file `pc.img.gz`.
# It will initialize the spread user as well as the project directory
# (which has to be defined in `PROJECT_PATH`).
# This is expected to be called from spread.

# This is to reflash a spread runner.
# This script takes one argument: a gzipped image to be flashed onto the disk.
# After successfully running this script, a reboot or power down will
# flash the machine with the given image.

set -eux

DEBIAN_FRONTEND=noninteractive apt install -y --no-install-recommends dracut-core busybox-initramfs

[ -d /run/initramfs ] || mkdir -p /run/initramfs

systemd-mount --property=DefaultDependencies=no --options=exec,size=2G none --type=tmpfs /run/initramfs

cp -T "${1}" /run/initramfs/image.gz

for try in /usr/lib/systemd/systemd-shutdown /lib/systemd/systemd-shutdown; do
    if [ -x "${try}" ]; then
        systemd_shutdown="${try}"
    fi
done

/usr/lib/dracut/dracut-install --ldd -D/run/initramfs -a umount systemctl dd "${systemd_shutdown}"
/usr/lib/dracut/dracut-install --ldd -D/run/initramfs /usr/lib/initramfs-tools/bin/busybox /bin/busybox

ln -s busybox /run/initramfs/bin/sh
ln -s busybox /run/initramfs/bin/gunzip

if [ -b /dev/vda ]; then
    DISK=/dev/vda
elif [ -b /dev/sda ]; then
    DISK=/dev/sda
elif [ -b /dev/nvme0n1 ]; then
    DISK=/dev/nvme0n1
else
    echo "Cannot find disk" 2>&1
    exit 1
fi

cat <<EOF >/run/initramfs/shutdown
#!/bin/sh

echo "SHUTTING DOWN"

set -eu

umount -l /oldroot

gunzip -c /image.gz | dd of='${DISK}' bs=32M

exec '${systemd_shutdown}' "\${@}"
EOF

chmod +x /run/initramfs/shutdown
