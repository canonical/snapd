#!/bin/bash

set -e

get_assets() {
    CACHE="$1"

    if [ -d "$CACHE" ]; then
        echo "Using existing cache dir $CACHE"
        return
    fi

    mkdir -p "$CACHE"
    # get the snaps
    for snap in pc-kernel pc; do
        snap download --channel=22 --target-directory="$CACHE" "$snap"
        unsquashfs -n -d "$CACHE"/snap-"$snap" "$CACHE"/"$snap"_*.snap
    done
    for snap in snapd core22; do
        snap download --target-directory="$CACHE" "$snap"
    done

    # get the ubuntu classic base
    (cd "$CACHE" && wget -c http://cdimage.ubuntu.com/ubuntu-base/releases/22.04/release/ubuntu-base-22.04-base-amd64.tar.gz)
}

cleanup() {
    IMG="$(readlink -f "$1")"
    MNT="$(readlink -f "$2")"

    sleep 1
    sudo umount "$MNT"/* || true
    sleep 1
    sudo kpartx -d "$IMG" || true
}

create_image() {
    IMG="$(readlink -f "$1")"

    rm -f "$IMG"
    truncate --size=6G "$IMG"
    echo "Creating partition on $IMG"
    cat <<EOF | sfdisk -q "$IMG"
label: gpt
device: boot.img
unit: sectors
first-lba: 34
last-lba: 12453854
sector-size: 512

boot.img1 : start=        2048, size=        2048, type=21686148-6449-6E6F-744E-656564454649, uuid=ECD24EAE-A687-4177-9223-6DDB4FCFF842, name="BIOS Boot"
##### no ubuntu-seed on the initial version but we need a EFI system
boot.img2 : start=        4096, size=     202752, type=C12A7328-F81F-11D2-BA4B-00A0C93EC93B, uuid=21A0079F-3E45-4669-8FF2-B3917819279F, name="EFI System partition"
boot.img3 : start=     2461696, size=     1536000, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, uuid=338DD9E7-CFE1-524A-A8B6-7D87DA8A4B34, name="ubuntu-boot"
boot.img4 : start=     3997696, size=       32768, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, uuid=1144DFB5-DFC2-0745-A1F2-AD311FEBE0DB, name="ubuntu-save"
boot.img5 : start=     4030464, size=     8423391, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, uuid=B84565A3-E9F8-8A40-AB04-810A4B891F8C, name="ubuntu-data"
EOF
}

install_data_partition() {
    set -x
    local DESTDIR=$1
    local CACHE=$2
    # just some random date for the seed label
    local SEED_LABEL=20220617

    snap_idx=(kernel gadget base snapd)
    declare -A SNAP_NAME SNAP_F SNAP_P IS_UNASSERTED
    SNAP_NAME[kernel]=pc-kernel
    SNAP_NAME[gadget]=pc
    SNAP_NAME[base]=core22
    SNAP_NAME[snapd]=snapd
    for i in "${snap_idx[@]}"; do
        snap_n=${SNAP_NAME[$i]}
        if [ "${IN_SNAP_F[$snap_n]}" != "" ]; then
            IS_UNASSERTED[$i]=true
            SNAP_F[$i]=${IN_SNAP_F[$snap_n]}
            SNAP_P[$i]=${IN_SNAP_P[$snap_n]}
        else
            IS_UNASSERTED[$i]=false
            SNAP_F[$i]=$(find "$CACHE" -maxdepth 1 -name "${SNAP_NAME[$i]}_*.snap" -printf "%f\n")
            SNAP_P[$i]="$CACHE/${SNAP_F[$i]}"
        fi
    done

    # Copy base filesystem
    sudo tar -C "$DESTDIR" -xf "$CACHE"/ubuntu-base-22.04-base-amd64.tar.gz

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
    cat > "$CACHE"/00-ethernet.yaml <<'EOF'
network:
  ethernets:
    any:
      match:
        name: e*
      dhcp4: true
  version: 2
EOF
    sudo cp "$CACHE"/00-ethernet.yaml "$DESTDIR"/etc/netplan

    # mount bits needed to be able to update boot assets
    sudo mkdir -p "$DESTDIR"/boot/grub "$DESTDIR"/boot/efi
    sudo tee "$DESTDIR"/etc/fstab <<'EOF'
/run/mnt/ubuntu-boot/EFI/ubuntu /boot/grub none bind 0 0
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

    # Populate snapd data
    cat > modeenv <<EOF
mode=run
recovery_system=$SEED_LABEL
current_recovery_systems=$SEED_LABEL
good_recovery_systems=$SEED_LABEL
base=${SNAP_F[base]}
gadget=${SNAP_F[gadget]}
current_kernels=${SNAP_F[kernel]}
model=canonical/ubuntu-core-22-pc-amd64
grade=dangerous
model_sign_key_id=9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn
current_kernel_command_lines=["snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 quiet splash"]
EOF
    sudo cp modeenv "$DESTDIR"/var/lib/snapd/
    # needed from the beginning in ubuntu-data as these are mounted by snap-bootstrap
    # (UC also has base here, but we do not mount it from initramfs in classic)
    # TODO use prepare-image --classic for this
    sudo mkdir -p "$DESTDIR"/var/lib/snapd/snaps/
    sudo cp "${SNAP_P[kernel]}" "${SNAP_P[gadget]}" \
         "$DESTDIR"/var/lib/snapd/snaps/
    # populate seed
    local seed_snaps_d="$DESTDIR"/var/lib/snapd/seed/snaps
    local recsys_d="$DESTDIR"/var/lib/snapd/seed/systems/"$SEED_LABEL"
    sudo mkdir -p "$recsys_d"/snaps "$recsys_d"/assertions "$seed_snaps_d"

    for i in "${snap_idx[@]}"; do
        if [ "${IS_UNASSERTED[$i]}" = true ]; then
            sudo cp "${SNAP_P[$i]}" "$recsys_d"/snaps
        else
            sudo cp "${SNAP_P[$i]}" "$seed_snaps_d"
        fi
    done
    sudo cp classic-model.assert "$recsys_d"/model
    {
        for assert in "$CACHE"/*.assert; do
            cat "$assert"
            printf "\n"
        done
    } > "$CACHE"/snap-asserts
    sudo cp "$CACHE"/snap-asserts "$recsys_d"/assertions/snaps
    sudo cp model-etc "$recsys_d"/assertions/

    # write options file if we have some unasserted snap in the seed
    if [ "${#IN_SNAP_F[@]}" -gt 0 ]; then
        OPTIONS_DATA="snaps:\n"
        for snap_n in "${!IN_SNAP_F[@]}"; do
            OPTIONS_DATA="${OPTIONS_DATA}- name: $snap_n\n  unasserted: ${IN_SNAP_F[$snap_n]}\n"
        done
        sudo sh -c "printf %b \"$OPTIONS_DATA\" > \"$recsys_d\"/options.yaml"
    fi
}

populate_image() {
    IMG="$(readlink -f "$1")"
    CACHE="$(readlink -f "$2")"
    MNT="$(readlink -f "$3")"
    local KERNEL_SNAP
    KERNEL_SNAP=$(find "$CACHE" -maxdepth 1 -name 'pc-kernel_*.snap' -printf "%f\n")

    mkdir -p "$MNT"
    loop=$(sudo kpartx -asv "$IMG" | head -n1 | cut -d' ' -f3)
    loop=${loop%p*}

    loop_esp="${loop}"p2
    loop_boot="${loop}"p3
    loop_save="${loop}"p4
    loop_data="${loop}"p5
    # XXX: on a real UC device this the ESP is "ubuntu-seed"
    sudo mkfs.fat /dev/mapper/"$loop_esp"
    sudo mkfs.ext4 -L ubuntu-boot -q /dev/mapper/"$loop_boot"
    sudo mkfs.ext4 -L ubuntu-save -q /dev/mapper/"$loop_save"
    sudo mkfs.ext4 -L ubuntu-data -q /dev/mapper/"$loop_data"
    for name in esp ubuntu-boot ubuntu-save ubuntu-data; do
        mkdir -p "$MNT"/"$name"
    done
    sudo mount /dev/mapper/"$loop_esp" "$MNT"/esp
    sudo mount /dev/mapper/"$loop_boot" "$MNT"/ubuntu-boot
    sudo mount /dev/mapper/"$loop_save" "$MNT"/ubuntu-save
    sudo mount /dev/mapper/"$loop_data" "$MNT"/ubuntu-data

    # install things into the image
    install_data_partition "$MNT"/ubuntu-data/ "$CACHE"

    # ESP partition just chainloads into ubuntu-boot
    # XXX: do we want this given that we don't have recovery systems?
    sudo mkdir -p "$MNT"/esp/EFI/boot
    sudo cp "$CACHE"/snap-pc/grubx64.efi "$MNT"/esp/EFI/boot
    sudo cp "$CACHE"/snap-pc/shim.efi.signed "$MNT"/esp/EFI/boot/bootx64.efi
    cat > "$CACHE"/esp-grub.cfg <<'EOF'
set default=0
set timeout=3

search --no-floppy --set=boot_fs --label ubuntu-boot
menuentry "Continue to run mode" --hotkey=n --id=run {
    chainloader ($boot_fs)/EFI/boot/grubx64.efi
}
EOF
    sudo mkdir -p "$MNT"/esp/EFI/ubuntu
    sudo cp "$CACHE"/esp-grub.cfg "$MNT"/esp/EFI/ubuntu/grub.cfg

    # ubuntu-boot
    sudo mkdir -p "$MNT"/ubuntu-boot/EFI/boot
    sudo cp -a "$CACHE"/snap-pc/grubx64.efi "$MNT"/ubuntu-boot/EFI/boot
    sudo cp -a "$CACHE"/snap-pc/shim.efi.signed "$MNT"/ubuntu-boot/EFI/boot/bootx64.efi

    sudo mkdir -p "$MNT"/ubuntu-boot/EFI/ubuntu
    cat > "$CACHE"/grub.cfg <<'EOF'
set default=0
set timeout=3

# load only kernel_status and kernel command line variables set by snapd from
# the bootenv
load_env --file /EFI/ubuntu/grubenv kernel_status snapd_extra_cmdline_args snapd_full_cmdline_args

set snapd_static_cmdline_args='console=ttyS0 console=tty1 panic=-1'
set cmdline_args="$snapd_static_cmdline_args $snapd_extra_cmdline_args"
if [ -n "$snapd_full_cmdline_args" ]; then
    set cmdline_args="$snapd_full_cmdline_args"
fi

set kernel=kernel.efi

if [ "$kernel_status" = "try" ]; then
    # a new kernel got installed
    set kernel_status="trying"
    save_env kernel_status

    # use try-kernel.efi
    set kernel=try-kernel.efi
elif [ "$kernel_status" = "trying" ]; then
    # nothing cleared the "trying snap" so the boot failed
    # we clear the mode and boot normally
    set kernel_status=""
    save_env kernel_status
elif [ -n "$kernel_status" ]; then
    # ERROR invalid kernel_status state, reset to empty
    echo "invalid kernel_status!!!"
    echo "resetting to empty"
    set kernel_status=""
    save_env kernel_status
fi

if [ -e $prefix/$kernel ]; then
menuentry "Run Ubuntu Core 22" {
    # use $prefix because the symlink manipulation at runtime for kernel snap
    # upgrades, etc. should only need the /boot/grub/ directory, not the
    # /EFI/ubuntu/ directory
    chainloader $prefix/$kernel snapd_recovery_mode=run $cmdline_args
}
else
    # nothing to boot :-/
    echo "missing kernel at $prefix/$kernel!"
fi
EOF
    sudo cp -a "$CACHE"/grub.cfg "$MNT"/ubuntu-boot/EFI/ubuntu/
    # This must be exactly 1024 bytes
    GRUBENV="# GRUB Environment Block
#######################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################################"
    printf "%s" "$GRUBENV" > "$CACHE"/grubenv
    sudo cp -a "$CACHE"/grubenv "$MNT"/ubuntu-boot/EFI/ubuntu/grubenv
    local assert_p=classic-model.assert
    if [ ! -f "$assert_p" ]; then
        printf "%s not found, please sign an assertion using classic-model.json as model\n" \
               "$assert_p"
        exit 1
    fi
    sudo mkdir -p "$MNT"/ubuntu-boot/device/
    sudo cp -a "$assert_p" "$MNT"/ubuntu-boot/device/model

    # kernel
    sudo mkdir -p "$MNT"/ubuntu-boot/EFI/ubuntu/"$KERNEL_SNAP"
    sudo cp -a "$CACHE"/snap-pc-kernel/kernel.efi "$MNT"/ubuntu-boot/EFI/ubuntu/"$KERNEL_SNAP"
    sudo ln -sf "$KERNEL_SNAP"/kernel.efi "$MNT"/ubuntu-boot/EFI/ubuntu/kernel.efi

    # cleanup
    sync
    sudo umount "$MNT"/ubuntu-*
}

show_how_to_run_qemu() {
    IMG="$1"

    echo "Image ready, run as"
    echo kvm -m 1500 -snapshot \
         -netdev user,id=net.0,hostfwd=tcp::10022-:22 \
        -device rtl8139,netdev=net.0 \
        -bios /usr/share/OVMF/OVMF_CODE.fd \
        -drive file="$1",if=virtio \
        -serial stdio

    echo "grub will chainload from ESP to ubuntu-boot"
    echo "there press ESC and add 'dangerous rd.systemd.debug-shell=1' after kernel.efi"
}

main() {
    # shellcheck disable=SC2064
    trap "cleanup \"$BOOT_IMG\" \"$MNT_DIR\"" EXIT INT

    get_assets "$CACHE_DIR"
    create_image "$BOOT_IMG"
    populate_image "$BOOT_IMG" "$CACHE_DIR" "$MNT_DIR"

    show_how_to_run_qemu "$BOOT_IMG"
    # XXX: show how to mount/chroot into the dir to test seeding
}

# 4th and later are optional local snaps
set -x
BOOT_IMG="${1:-./boot.img}"
CACHE_DIR="${2:-./cache}"
MNT_DIR="${3:-./mnt}"
shift 3 || true
declare -A IN_SNAP_P IN_SNAP_F
for sn_p in "$@"; do
    sn_f=${sn_p##*/}
    sn_name=${sn_f%%_*}
    IN_SNAP_P[$sn_name]=$sn_p
    IN_SNAP_F[$sn_name]=$sn_f
done

main "$BOOT_IMG" "$CACHE_DIR" "$MNT_DIR"
