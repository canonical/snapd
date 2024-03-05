#!/bin/bash

# old-flash.sh: Install a flashing boot entry to flash a new disk image
#
# Copyright (C) 2024 Canonical Ltd
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

# This version is only intended to be used on 16.04 where it is
# difficult to run an initrd during shutdown.

set -eux

DEBIAN_FRONTEND=noninteractive apt install -y --no-install-recommends dracut-core busybox-initramfs

mkdir initrd

cp "$1" initrd/to-flash.img

mkdir initrd/proc
mkdir initrd/dev
cat > "initrd/init" << EOF
#!/bin/sh

set -eux

mount -t proc none /proc
mount -t devtmpfs none /dev

# blow away everything
OF=/dev/sda
if [ -e /dev/vda ]; then
    OF=/dev/vda
fi
gunzip -c /to-flash.img | dd of=\$OF bs=4M
# and reboot
sync
echo b > /proc/sysrq-trigger
EOF

chmod +x "initrd/init"

/usr/lib/dracut/dracut-install --ldd -Dinitrd -a dd sync mount
/usr/lib/dracut/dracut-install --ldd -Dinitrd /usr/lib/initramfs-tools/bin/busybox /bin/busybox
[ -d initrd/bin ] || mkdir -p initrd/bin
ln -s busybox initrd/bin/sh
ln -s busybox initrd/bin/gunzip

(cd initrd; find . | cpio --create --quiet --format=newc --owner=0:0) >/initrd-reflash.img

cat >/boot/grub/grub.cfg <<EOF
set default=0
set timeout=2
menuentry 'flash-all-snaps' {
linux /vmlinuz console=tty1 console=ttyS0
initrd /initrd-reflash.img
}
EOF
