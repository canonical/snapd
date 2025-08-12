set -eu

loop_device=$(losetup --show -f ./fake-disk.img)
mounts=()
cleanup() {
    losetup -d "${loop_device}" || true
    for m in "${mounts[@]}"; do
      umount -l "${m}" || true
    done
}
trap cleanup EXIT INT

# and "install" the current seed to the fake disk
./muinstaller -label "$LABEL" -device "$loop_device" -rootfs-creator "$TESTSLIB"/muinstaller/mk-classic-rootfs.sh
# validate that the fake installer created the expected partitions
sfdisk -d "$loop_device" > fdisk_output
MATCH "${loop_device}p1 .* name=\"BIOS Boot\""   < fdisk_output
# TODO: the real MVP hybrid device will not contain a ubuntu-seed
#       partition (needs a different gadget)
MATCH "${loop_device}p2 .* name=\"EFI System partition\"" < fdisk_output
MATCH "${loop_device}p3 .* name=\"ubuntu-boot\"" < fdisk_output
MATCH "${loop_device}p4 .* name=\"ubuntu-save\"" < fdisk_output
MATCH "${loop_device}p5 .* name=\"ubuntu-data\"" < fdisk_output

# image partitions are not mounted anymore
for d in ubuntu-seed ubuntu-boot ubuntu-data ubuntu-save; do
    test -d /run/mnt/"$d"
    not mountpoint /run/mnt/"$d"
done

# mount image to inspect data
mount -o ro "${loop_device}"p3 /run/mnt/ubuntu-boot
mounts+=(/run/mnt/ubuntu-boot)
mount -o ro "${loop_device}"p5 /run/mnt/ubuntu-data
mounts+=(/run/mnt/ubuntu-data)

# seed is populated
test -d /run/mnt/ubuntu-data/var/lib/snapd/seed/systems/"$LABEL"
# rootfs is there
test -x /run/mnt/ubuntu-data/usr/lib/systemd/systemd
# ensure not "ubuntu-data/system-data" is generated, this is a dir only
# used on core and should not be there on classic
not test -d /run/mnt/ubuntu-data/system-data
# TODO: ensure we don't have this
#not test -d /run/mnt/ubuntu-data/_writable_defaults
# and the boot assets are in the right place
test -e /run/mnt/ubuntu-boot/EFI/ubuntu/kernel.efi
test -e /run/mnt/ubuntu-boot/EFI/ubuntu/grubenv
test -e /run/mnt/ubuntu-boot/EFI/boot/grubx64.efi
# and we have a modenv in the image
MATCH "mode=run" < /run/mnt/ubuntu-data/var/lib/snapd/modeenv
MATCH "classic=true" < /run/mnt/ubuntu-data/var/lib/snapd/modeenv
