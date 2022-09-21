#!/bin/bash -exu

replace_initramfs_bits() {
    KERNEL_EFI_ORIG="$CACHE_DIR"/snap-pc-kernel/kernel.efi
    if [ ! -d initrd ]; then
        objcopy -O binary -j .initrd "$KERNEL_EFI_ORIG" initrd.img
        unmkinitramfs initrd.img initrd
    fi

    # Retrieve efi stub from ppa so we can rebuild kernel.efi
    sudo DEBIAN_FRONTEND=noninteractive apt install -y --no-install-recommends ubuntu-dev-tools
    codename=$(lsb_release -cs)
    arch=$(dpkg-architecture -q DEB_BUILD_ARCH)
    pull-lp-debs -a "$arch" -D ppa \
                 --ppa ppa:snappy-dev/image ubuntu-core-initramfs "$codename"
    dpkg --fsys-tarfile ubuntu-core-initramfs_*.deb |
        tar --wildcards -xf - './usr/lib/ubuntu-core-initramfs/efi/linux*.efi.stub'

    cp "$SNAPD_BINPATH"/snap-bootstrap initrd/main/usr/lib/snapd/
    cd initrd/main
    find . | cpio --create --quiet --format=newc --owner=0:0 | lz4 -l -7 > ../../initrd.img.new
    cd -

    objcopy -O binary -j .linux "$KERNEL_EFI_ORIG" linux
    # Replace kernel.efi in unsquashed snap
    objcopy --add-section .linux=linux --change-section-vma .linux=0x2000000 \
            --add-section .initrd=initrd.img.new --change-section-vma .initrd=0x3000000 \
            usr/lib/ubuntu-core-initramfs/efi/linux*.efi.stub \
            "$KERNEL_EFI_ORIG"
}

cleanup() {
    IMG="$(readlink -f "$1")"
    MNT="$(readlink -f "$2")"

    sync
    sleep 1
    sudo umount "$MNT"/* || true
    sleep 1
    sudo kpartx -d "$IMG" || true
}

main() {
    MNT=mnt-replace

    mkdir -p "$MNT"/ubuntu-boot "$MNT"/data

    replace_initramfs_bits

    # shellcheck disable=SC2064
    trap "cleanup \"$IMG\" \"$MNT\"" EXIT

    loop=$(sudo kpartx -asv "$IMG" | head -n1 | cut -d' ' -f3)
    loop=${loop%p*}
    loop_boot="$loop"p3
    sudo mount /dev/mapper/"$loop_boot" "$MNT"/ubuntu-boot

    # copy kernel.efi with modified initramfs
    subpath=$(readlink "$MNT"/ubuntu-boot/EFI/ubuntu/kernel.efi)
    cp -a "$CACHE_DIR"/snap-pc-kernel/kernel.efi "$MNT"/ubuntu-boot/EFI/ubuntu/"$subpath"

    # replace snapd in data partition with the one compiled in the test
    data_mnt="$loop"p5
    sudo mount /dev/mapper/"$data_mnt" "$MNT"/data
    sudo cp ../../../../../snapd_*.deb "$MNT"/data/snapd.deb
    sudo chroot "$MNT"/data apt install -y --no-install-recommends ./snapd.deb
    sudo rm "$MNT"/data/snapd.deb
    # enable debug traces
    sudo mkdir -p "$MNT"/data/etc/systemd/system/snapd.service.d/
    sudo tee "$MNT"/data/etc/systemd/system/snapd.service.d/override.conf <<'EOF'
[Service]
Environment=SNAPD_DEBUG=1
EOF
}

IMG="${1:-./boot.img}"
CACHE_DIR="${2:-./cache}"
SNAPD_BINPATH="${3:-/usr/lib/snapd}"
main
