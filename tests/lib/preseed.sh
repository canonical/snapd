#!/bin/bash

# mount ubuntu cloud image through qemu-nbd and mount
# critical virtual filesystems (such as proc) under
# the root of mounted image.
mount_ubuntu_image() {
    local CLOUD_IMAGE=$1
    local IMAGE_MOUNTPOINT=$2

    if ! lsmod | grep nbd; then
        modprobe nbd
    fi

    qemu-nbd -c /dev/nbd0 "$CLOUD_IMAGE"
    # nbd0p1 may take a short while to become available
    retry-tool -n 5 --wait 1 mount /dev/nbd0p1 "$IMAGE_MOUNTPOINT"
    mount -t proc /proc "$IMAGE_MOUNTPOINT/proc"
    mount -t sysfs sysfs "$IMAGE_MOUNTPOINT/sys"
    mount -t devtmpfs udev "$IMAGE_MOUNTPOINT/dev"
    mount -t securityfs securityfs "$IMAGE_MOUNTPOINT/sys/kernel/security"
}

umount_ubuntu_image() {
    local IMAGE_MOUNTPOINT=$1

    for fs in proc dev sys/kernel/security sys; do
        umount "$IMAGE_MOUNTPOINT/$fs"
    done
    umount "$IMAGE_MOUNTPOINT"
    rmdir "$IMAGE_MOUNTPOINT"

    # qemu-nbd -d may sporadically fail when removing the device,
    # reporting it's still in use.
    retry-tool -n 5 --wait 1 qemu-nbd -d /dev/nbd0
}

# XXX inject new snapd into the core image in seed/snaps of the cloud image
# and make core unasserted.
# this will go away once snapd on the core is new enough to support
# pre-seeding.
setup_preseeding() {
    local IMAGE_MOUNTPOINT=$1
    local CORE_IMAGE

    CORE_IMAGE=$(find "$IMAGE_MOUNTPOINT/var/lib/snapd/seed/snaps/" -name "core_*.snap")
    unsquashfs "$CORE_IMAGE"
    cp /usr/lib/snapd/snapd squashfs-root/usr/lib/snapd/snapd
    rm "$CORE_IMAGE"
    #shellcheck source=tests/lib/snaps.sh
    . "$TESTSLIB"/snaps.sh
    mksnap_fast squashfs-root "$CORE_IMAGE"
    sed -i "$IMAGE_MOUNTPOINT/var/lib/snapd/seed/seed.yaml" -E -e 's/^(\s+)name: core/\1name: core\n\1unasserted: true/'
}
