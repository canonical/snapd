#!/bin/bash

set -e

# uncomment for better debug messages
#set -x
#exec > /tmp/mk-classic-rootfs.sh.log
#exec 2>&1


# XXX: merge with the work from alfonso in
# tests/nested/manual/fde-on-classic/mk-image.sh (PR:12102)
create_classic_rootfs() {
    set -x
    local DESTDIR="$1"


    # Create basic devices to be able to install packages
    [ -e "$DESTDIR"/dev/null ] || sudo mknod -m 666 "$DESTDIR"/dev/null c 1 3
    [ -e "$DESTDIR"/dev/zero ] || sudo mknod -m 666 "$DESTDIR"/dev/zero c 1 5
    [ -e "$DESTDIR"/dev/random ] || sudo mknod -m 666 "$DESTDIR"/dev/random c 1 8
    [ -e "$DESTDIR"/dev/urandom ] || sudo mknod -m 666 "$DESTDIR"/dev/urandom c 1 9
    # ensure resolving works inside the chroot
    echo "nameserver 8.8.8.8" | sudo tee -a "$DESTDIR"/etc/resolv.conf
    # install additional packages
    sudo chroot "$DESTDIR" /usr/bin/sh -c "DEBIAN_FRONTEND=noninteractive apt update"
    local pkgs="snapd ssh openssh-server sudo iproute2 iputils-ping isc-dhcp-client netplan.io vim-tiny kmod cloud-init"
    sudo chroot "$DESTDIR" /usr/bin/sh -c \
         "DEBIAN_FRONTEND=noninteractive apt install --no-install-recommends -y $pkgs"
    # netplan config
    cat <<'EOF' | sudo tee "$DESTDIR"/etc/netplan/00-ethernet.yaml
network:
  ethernets:
    any:
      match:
        name: e*
      dhcp4: true
  version: 2
EOF

    # ensure we can login
    sudo chroot "$DESTDIR" /usr/sbin/adduser --disabled-password --gecos "" user1
    printf "ubuntu\nubuntu\n" | sudo chroot "$DESTDIR" /usr/bin/passwd user1
    echo "user1 ALL=(ALL) NOPASSWD:ALL" | sudo tee -a "$DESTDIR"/etc/sudoers

    # set password for root user
    sudo chroot "$DESTDIR" /usr/bin/sh -c 'echo root:root | chpasswd'
    sudo tee -a "$DESTDIR/etc/ssh/sshd_config" <<'EOF'
PermitRootLogin yes
PasswordAuthentication yes
EOF

    # install the current in-development version of snapd when available,
    # this will give us seeding support
    GOPATH="${GOPATH:-./}"
    package=$(find "$GOPATH" -maxdepth 1 -name "snapd_*.deb")
    if [ -e "$package"  ]; then
        cp "$package" "$DESTDIR"/var/cache/apt/archives
        sudo chroot "$DESTDIR" /usr/bin/sh -c \
             "DEBIAN_FRONTEND=noninteractive apt install -y /var/cache/apt/archives/$(basename "$package")"
    fi

    # ensure that we have a mount point for the bind mount below
    sudo mkdir -p "$DESTDIR"/boot/grub
    # This is done by the the-modeenv script that is called by the
    # populate-writable service from initramfs on UC20+, but we don't
    # run it on classic.
    sudo tee -a "$DESTDIR/etc/fstab" <<'EOF'
/run/mnt/ubuntu-boot/EFI/ubuntu /boot/grub none bind
EOF
}

# get target dir from user
DST="$1"
if [ ! -d "$DST" ]; then
    echo "target dir $DST is not a directory"
    exit 1
fi

# extract the base
if [ -f /cdrom/casper/base.squashfs ]; then
    sudo unsquashfs -f -d "$DST" /cdrom/casper/base.squashfs
    # TODO: find out why the squashfs is preseeded
    /usr/lib/snapd/snap-preseed --reset "$DST"
else
    BASETAR=ubuntu-base.tar.gz
    wget -c http://cdimage.ubuntu.com/ubuntu-base/releases/22.04/release/ubuntu-base-22.04.1-base-amd64.tar.gz -O "$BASETAR"
    sudo tar -C "$DST" -xf "$BASETAR"
fi

# create minimal rootfs
create_classic_rootfs "$DST"
