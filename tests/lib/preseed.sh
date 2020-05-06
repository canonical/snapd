#!/bin/bash

# mount ubuntu cloud image through qemu-nbd and mount
# critical virtual filesystems (such as proc) under
# the root of mounted image.
# The path of the image needs to be absolute as a systemd service
# gets created for qemu-nbd.
mount_ubuntu_image() {
    local CLOUD_IMAGE=$1
    local IMAGE_MOUNTPOINT=$2

    if ! lsmod | grep nbd; then
        modprobe nbd
    fi

    # Run qemu-ndb as a service, so that it does not interact with ssh
    # stdin/stdout it would otherwise inherit from the spread session.
    systemd-run --system --service-type=forking --unit=qemu-ndb-preseed.service "$(command -v qemu-nbd)" --fork -c /dev/nbd0 "$CLOUD_IMAGE"
    # nbd0p1 may take a short while to become available
    retry-tool -n 5 --wait 1 test -e /dev/nbd0p1
    mount /dev/nbd0p1 "$IMAGE_MOUNTPOINT"
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
