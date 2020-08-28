#!/bin/bash

set -e

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB"/systemd.sh

WORK_DIR="${WORK_DIR:-/tmp/work-dir}"
NESTED_VM=nested-vm
SSH_PORT=8022
MON_PORT=8888

wait_for_ssh(){
    retry=400
    wait=1
    while ! execute_remote true; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for ssh. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}

wait_for_no_ssh(){
    retry=200
    wait=1
    while execute_remote true; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for no ssh. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}

prepare_ssh(){
    execute_remote "sudo adduser --uid 12345 --extrausers --quiet --disabled-password --gecos '' test"
    execute_remote "echo test:ubuntu | sudo chpasswd"
    execute_remote "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test"

    execute_remote "sudo adduser --extrausers --quiet --disabled-password --gecos '' external"
    execute_remote "echo external:ubuntu | sudo chpasswd"
    execute_remote "echo 'external ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-external"
}

create_assertions_disk(){
    mkdir -p "$WORK_DIR"
    ASSERTIONS_DISK="$WORK_DIR/assertions.disk"
    # make an image
    dd if=/dev/null of="$ASSERTIONS_DISK" bs=1M seek=1
    # format it as dos with a vfat partition
    # TODO: can we do this more programmatically without printing into fdisk ?
    printf 'o\nn\np\n1\n\n\nt\nc\nw\n' | fdisk "$ASSERTIONS_DISK"
    # mount the disk image
    kpartx -av "$ASSERTIONS_DISK"
    # find the loopback device for the partition
    LOOP_DEV=$(losetup --list | grep "$ASSERTIONS_DISK" | awk '{print $1}' | grep -Po "/dev/loop\K([0-9]*)")
    # wait for the loop device to show up
    retry -n 3 --wait 1 test -e "/dev/mapper/loop${LOOP_DEV}p1"
    # make a vfat partition
    mkfs.vfat -n SYSUSER "/dev/mapper/loop${LOOP_DEV}p1"
    # mount the partition and copy the files 
    mkdir -p "$WORK_DIR/sys-user-partition"
    mount "/dev/mapper/loop${LOOP_DEV}p1" "$WORK_DIR/sys-user-partition"
    sudo cp "$TESTSLIB/assertions/auto-import.assert" "$WORK_DIR/sys-user-partition"

    # unmount the partition and the image disk
    sudo umount "$WORK_DIR/sys-user-partition"
    sudo kpartx -d "$ASSERTIONS_DISK"
}

get_qemu_for_nested_vm(){
    case "${NESTED_ARCHITECTURE:-amd64}" in
    amd64)
        command -v qemu-system-x86_64
        ;;
    i386)
        command -v qemu-system-i386
        ;;
    *)
        echo "unsupported architecture"
        exit 1
        ;;
    esac
}

# shellcheck disable=SC2120
get_google_image_url_for_nested_vm(){
    case "${1:-$SPREAD_SYSTEM}" in
        ubuntu-16.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.10-64*)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/groovy-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

get_ubuntu_image_url_for_nested_vm(){
    case "$SPREAD_SYSTEM" in
        ubuntu-16.04-64*)
            echo "https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64*)
            echo "https://cloud-images.ubuntu.com/bionic/current/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64*)
            echo "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.10-64*)
            echo "https://cloud-images.ubuntu.com/groovy/current/groovy-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

get_cdimage_current_image_url(){
    VERSION=$1
    CHANNEL=$2
    ARCH=$3

    echo "http://cdimage.ubuntu.com/ubuntu-core/$VERSION/$CHANNEL/current/ubuntu-core-$VERSION-$ARCH.img.xz"
}

get_nested_snap_rev(){
    SNAP=$1
    execute_remote "snap list $SNAP" | grep -E "^$SNAP" | awk '{ print $3 }' | tr -d '\n'
}

get_snap_rev_for_channel(){
    SNAP=$1
    CHANNEL=$2
    snap info "$SNAP" | grep "$CHANNEL" | awk '{ print $4 }' | sed 's/.*(\(.*\))/\1/' | tr -d '\n'
}

get_nested_snap_channel(){
    SNAP=$1
    execute_remote "snap list $SNAP" | grep -E "^$SNAP" | awk '{ print $4 }' | tr -d '\n'
}

get_image_url_for_nested_vm(){
    if [[ "$SPREAD_BACKEND" == google* ]]; then
        #shellcheck disable=SC2119
        get_google_image_url_for_nested_vm
    else
        get_ubuntu_image_url_for_nested_vm
    fi
}

is_nested_system(){
    if is_core_nested_system || is_classic_nested_system ; then
        return 0
    else 
        return 1
    fi
}

is_core_nested_system(){
    if [ -z "${NESTED_TYPE-}" ]; then
        echo "Variable NESTED_TYPE not defined."
        return 1
    fi

    test "$NESTED_TYPE" = "core"
}

is_classic_nested_system(){
    if [ -z "${NESTED_TYPE-}" ]; then
        echo "Variable NESTED_TYPE not defined."
        return 1
    fi

    test "$NESTED_TYPE" = "classic"
}

is_focal_system(){
    test "$(lsb_release -cs)" = focal
}

is_core_20_nested_system(){
    is_focal_system
}

is_bionic_system(){
    test "$(lsb_release -cs)" = bionic
}

is_core_18_nested_system(){
    is_bionic_system
}

is_xenial_system(){
    test "$(lsb_release -cs)" = xenial
}

is_core_16_nested_system(){
    is_xenial_system
}

refresh_to_new_core(){
    local NEW_CHANNEL=$1
    local CHANGE_ID
    if [ "$NEW_CHANNEL" = "" ]; then
        echo "Channel to refresh is not defined."
        exit 1
    else
        echo "Refreshing the core/snapd snap"
        if is_classic_nested_system; then
            execute_remote "sudo snap refresh core --${NEW_CHANNEL}"
            execute_remote "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi

        if is_core_18_nested_system || is_core_20_nested_system; then
            execute_remote "sudo snap refresh snapd --${NEW_CHANNEL}"
            execute_remote "snap info snapd" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        else
            CHANGE_ID=$(execute_remote "sudo snap refresh core --${NEW_CHANNEL} --no-wait")
            wait_for_no_ssh
            wait_for_ssh
            # wait for the refresh to be done before checking, if we check too
            # quickly then operations on the core snap like reverting, etc. may
            # fail because it will have refresh-snap change in progress
            execute_remote "snap watch $CHANGE_ID"
            execute_remote "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi
    fi
}

get_snakeoil_key(){
    local KEYNAME="PkKek-1-snakeoil"
    wget https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.key
    wget https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.pem
    echo "$KEYNAME"
}

secboot_sign_gadget(){
    local GADGET_DIR="$1"
    local KEY="$2"
    local CERT="$3"
    sbattach --remove "$GADGET_DIR"/shim.efi.signed
    sbsign --key "$KEY" --cert "$CERT" --output pc-gadget/shim.efi.signed pc-gadget/shim.efi.signed
}

cleanup_nested_env(){
    rm -rf "$WORK_DIR"
}

get_image_dir(){
    echo "$WORK_DIR/image"
}

get_image_name(){
    local TYPE="$1"
    local SOURCE="${CORE_CHANNEL}"
    local VERSION="16"

    if is_core_20_nested_system; then
        VERSION="20"
    elif is_core_18_nested_system; then
        VERSION="18"
    fi

    if [ "$BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
        SOURCE="custom"
    fi
    if [ "$(get_extra_snaps | wc -l)" != "0" ]; then
        SOURCE="custom"
    fi
    echo "ubuntu-${TYPE}-${VERSION}-${SOURCE}.img"
}

get_image_path(){
    echo "$WORK_DIR/image"
}

prepare_image_dir(){
    mkdir -p "$(get_image_dir)"
}

get_extra_snaps_path(){
    echo "${PWD}/extra-snaps"
}

get_extra_snaps(){
    local EXTRA_SNAPS=""
    local EXTRA_SNAPS_PATH
    EXTRA_SNAPS_PATH="$(get_extra_snaps_path)"

    if [ -d "$EXTRA_SNAPS_PATH" ]; then
        while IFS= read -r mysnap; do
            echo "$mysnap"
        done < <(find "$EXTRA_SNAPS_PATH" -name '*.snap')
    fi
}

download_nested_image(){
    local IMAGE_URL=$1
    local IMAGE_NAME=$2
    local IMAGE_DIR
    IMAGE_DIR="$(get_image_dir)"

    curl -L -o "${IMAGE_DIR}/${IMAGE_NAME}" "$IMAGE_URL"

    if [[ "$IMAGE_URL" == *.img.xz ]]; then
        mv "${IMAGE_DIR}/${IMAGE_NAME}" "${IMAGE_DIR}/${IMAGE_NAME}.xz"
        unxz "${IMAGE_DIR}/${IMAGE_NAME}.xz"
    elif [[ "$IMAGE_URL" == *.img ]]; then
        echo "Image doesn't need to be decompressed"
    else
        echo "Image extension not supported for image $IMAGE_URL, exiting..."
        exit 1
    fi
}

get_nested_model(){
    case "$SPREAD_SYSTEM" in
        ubuntu-16.04-64)
            echo "$TESTSLIB/assertions/nested-amd64.model"
            ;;
        ubuntu-18.04-64)
            echo "$TESTSLIB/assertions/nested-18-amd64.model"
            ;;
        ubuntu-20.04-64)
            echo "$TESTSLIB/assertions/nested-20-amd64.model"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

create_nested_core_vm(){
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    # shellcheck source=tests/lib/snaps.sh
    . "$TESTSLIB"/snaps.sh

    local IMAGE_DIR IMAGE_NAME
    IMAGE_DIR="$(get_image_dir)"
    IMAGE_NAME="$(get_image_name core)"

    prepare_image_dir
    if [ ! -f "$IMAGE_DIR/$IMAGE_NAME" ]; then

        if [ -n "$CUSTOM_IMAGE_URL" ]; then
            # download the ubuntu-core image from $CUSTOM_IMAGE_URL
            download_nested_image "$CUSTOM_IMAGE_URL" "$IMAGE_NAME"
        else
            # create the ubuntu-core image
            local UBUNTU_IMAGE=/snap/bin/ubuntu-image
            local EXTRA_FUNDAMENTAL=""
            local EXTRA_SNAPS=""
            for mysnap in $(get_extra_snaps); do
                EXTRA_SNAPS="$EXTRA_SNAPS --snap $mysnap"
            done

            if [ "$BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
                if is_core_16_nested_system; then
                    repack_snapd_deb_into_core_snap "$WORK_DIR"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $WORK_DIR/core-from-snapd-deb.snap"

                elif is_core_18_nested_system; then
                    repack_snapd_deb_into_snapd_snap "$WORK_DIR"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $WORK_DIR/snapd-from-deb.snap"

                elif is_core_20_nested_system; then
                    snap download --basename=pc-kernel --channel="20/edge" pc-kernel
                    uc20_build_initramfs_kernel_snap "$PWD/pc-kernel.snap" "$WORK_DIR/image"

                    # Get the snakeoil key and cert
                    KEY_NAME=$(get_snakeoil_key)
                    SNAKEOIL_KEY="$PWD/$KEY_NAME.key"
                    SNAKEOIL_CERT="$PWD/$KEY_NAME.pem"

                    # Prepare the pc kernel snap
                    KERNEL_SNAP=$(ls "$IMAGE_DIR"/pc-kernel_*.snap)
                    KERNEL_UNPACKED="$IMAGE_DIR"/kernel-unpacked
                    unsquashfs -d "$KERNEL_UNPACKED" "$KERNEL_SNAP"
                    sbattach --remove "$KERNEL_UNPACKED/kernel.efi"
                    sbsign --key "$SNAKEOIL_KEY" --cert "$SNAKEOIL_CERT" "$KERNEL_UNPACKED/kernel.efi"  --output "$KERNEL_UNPACKED/kernel.efi"
                    snap pack "$KERNEL_UNPACKED" "$WORK_DIR/image"

                    chmod 0600 "$KERNEL_SNAP"
                    rm -f "$PWD/pc-kernel.snap"
                    rm -rf "$KERNEL_UNPACKED"
                    EXTRA_FUNDAMENTAL="--snap $KERNEL_SNAP"

                    # Prepare the pc gadget snap (unless provided by extra-snaps)
                    GADGET_SNAP=""
                    if [ -d "$(get_extra_snaps_path)" ]; then
                        GADGET_SNAP=$(find extra-snaps -name 'pc_*.snap')
                    fi
                    # XXX: deal with [ "$ENABLE_SECURE_BOOT" != "true" ] && [ "$ENABLE_TPM" != "true" ]
                    if [ -z "$GADGET_SNAP" ]; then
                        snap download --basename=pc --channel="20/edge" pc
                        unsquashfs -d pc-gadget pc.snap
                        secboot_sign_gadget pc-gadget "$SNAKEOIL_KEY" "$SNAKEOIL_CERT"
                        snap pack pc-gadget/ "$IMAGE_DIR"

                        GADGET_SNAP=$(ls "$IMAGE_DIR"/pc_*.snap)
                        rm -f "$PWD/pc.snap" "$SNAKEOIL_KEY" "$SNAKEOIL_CERT"
                        EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $GADGET_SNAP"
                    fi
                    snap download --channel="latest/edge" snapd
                    repack_snapd_snap_with_deb_content_and_run_mode_firstboot_tweaks "$PWD/new-snapd" "false"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $PWD/new-snapd/snapd_*.snap"
                else
                    echo "unknown nested core system (host is $(lsb_release -cs) )"
                    exit 1
                fi
            fi

            # Invoke ubuntu image
            local NESTED_MODEL
            NESTED_MODEL="$(get_nested_model)"
            "$UBUNTU_IMAGE" --image-size 10G "$NESTED_MODEL" \
                --channel "$CORE_CHANNEL" \
                --output "$IMAGE_DIR/$IMAGE_NAME" \
                "$EXTRA_FUNDAMENTAL" \
                "$EXTRA_SNAPS"
        fi

        # Configure the user for the vm
        if [ "$USE_CLOUD_INIT" = "true" ]; then
            if is_core_20_nested_system; then
                configure_cloud_init_nested_core_vm_uc20 "$IMAGE_DIR/$IMAGE_NAME"
            else
                configure_cloud_init_nested_core_vm "$IMAGE_DIR/$IMAGE_NAME"
            fi
        else
            create_assertions_disk
        fi
    fi
}

configure_cloud_init_nested_core_vm(){
    local IMAGE=$1
    create_cloud_init_data "$WORK_DIR/user-data" "$WORK_DIR/meta-data"

    # mount the image and find the loop device /dev/loop that is created for it
    kpartx -avs "$IMAGE"
    devloop=$(losetup --list --noheadings | grep "$IMAGE" | awk '{print $1}')
    dev=$(basename "$devloop")
    
    # we add cloud-init data to the 3rd partition, which is writable
    writableDev="/dev/mapper/${dev}p3"
    
    # wait for the loop device to show up
    retry -n 3 --wait 1 test -e "$writableDev"
    tmp=$(mktemp -d)
    mount "$writableDev" "$tmp"

    # use nocloud-net for the dir to copy data into
    mkdir -p "$tmp/system-data/var/lib/cloud/seed/nocloud-net/"
    cp "$WORK_DIR/user-data" "$tmp/system-data/var/lib/cloud/seed/nocloud-net/"
    cp "$WORK_DIR/meta-data" "$tmp/system-data/var/lib/cloud/seed/nocloud-net/"

    sync
    umount "$tmp"
    kpartx -d "$IMAGE"
}

create_cloud_init_data(){
    USER_DATA=$1
    META_DATA=$2
    cat <<EOF > "$USER_DATA"
#cloud-config
  ssh_pwauth: True
  users:
   - name: user1
     sudo: ALL=(ALL) NOPASSWD:ALL
     shell: /bin/bash
  chpasswd:
   list: |
    user1:ubuntu
   expire: False
EOF

    cat <<EOF > "$META_DATA"
instance_id: cloud-images
EOF
}

create_cloud_init_config(){
    CONFIG_PATH=$1
    cat <<EOF > "$CONFIG_PATH"
#cloud-config
  ssh_pwauth: True
  users:
   - name: user1
     sudo: ALL=(ALL) NOPASSWD:ALL
     shell: /bin/bash
  chpasswd:
   list: |
    user1:ubuntu
   expire: False
  datasource_list: [ "None"]
  datasource:
    None:
     userdata_raw: |
      #!/bin/bash
      echo test
EOF
}

configure_cloud_init_nested_core_vm_uc20(){
    local IMAGE=$1
    create_cloud_init_config "$WORK_DIR/data.cfg"

    # mount the image and find the loop device /dev/loop that is created for it
    kpartx -avs "$IMAGE"
    devloop=$(losetup --list --noheadings | grep "$IMAGE" | awk '{print $1}')
    dev=$(basename "$devloop")
    
    # we add cloud-init data to the 2nd partition, which is ubuntu-seed
    ubuntuSeedDev="/dev/mapper/${dev}p2"
    
    # wait for the loop device to show up
    retry -n 3 --wait 1 test -e "$ubuntuSeedDev"
    tmp=$(mktemp -d)
    mount "$ubuntuSeedDev" "$tmp"
    mkdir -p "$tmp/data/etc/cloud/cloud.cfg.d/"
    cp -f "$WORK_DIR/data.cfg" "$tmp/data/etc/cloud/cloud.cfg.d/"
    sync
    umount "$tmp"
    kpartx -d "$IMAGE"
}

force_stop_nested_vm(){
    systemctl stop nested-vm
}

start_nested_core_vm_unit(){
    local QEMU CURRENT_IMAGE
    CURRENT_IMAGE=$1
    QEMU=$(get_qemu_for_nested_vm)

    # Now qemu parameters are defined

    # use only 2G of RAM for qemu-nested
    # the caller can override PARAM_MEM
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MEM="${PARAM_MEM:--m 4096}"
        PARAM_SMP="-smp 2"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MEM="${PARAM_MEM:--m 2048}"
        PARAM_SMP="-smp 1"
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi

    PARAM_DISPLAY="-nographic"
    PARAM_NETWORK="-net nic,model=virtio -net user,hostfwd=tcp::$SSH_PORT-:22"
    PARAM_MONITOR="-monitor tcp:127.0.0.1:$MON_PORT,server,nowait"
    PARAM_USB="-usb"
    PARAM_CD="${PARAM_CD:-}"
    PARAM_RANDOM="-object rng-random,id=rng0,filename=/dev/urandom -device virtio-rng-pci,rng=rng0"
    PARAM_CPU=""
    PARAM_TRACE="-d cpu_reset"
    PARAM_LOG="-D $WORK_DIR/qemu.log"
    PARAM_SERIAL="-serial file:${WORK_DIR}/serial.log"

    # Set kvm attribute
    ATTR_KVM=""
    if [ "$ENABLE_KVM" = "true" ]; then
        ATTR_KVM=",accel=kvm"
        # CPU can be defined just when kvm is enabled
        PARAM_CPU="-cpu host"
        # Increase the number of cpus used once the issue related to kvm and ovmf is fixed
        # https://bugs.launchpad.net/ubuntu/+source/kvm/+bug/1872803
        PARAM_SMP="-smp 1"
    fi

    # with qemu-nested, we can't use kvm acceleration
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MACHINE="-machine ubuntu${ATTR_KVM}"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MACHINE=""
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi
    
    PARAM_ASSERTIONS=""
    PARAM_BIOS=""
    PARAM_TPM=""
    if [ "$USE_CLOUD_INIT" != "true" ]; then
        # TODO: fix using the old way of an ext4 formatted drive w/o partitions
        #       as this used to work but has since regressed
        
        # this simulates a usb drive attached to the device, the removable=true
        # is necessary otherwise snapd will not import it, as snapd only 
        # considers removable devices for cold-plug first-boot runs
        # the nec-usb-xhci device is necessary to create the bus we attach the
        # storage to
        PARAM_ASSERTIONS="-drive if=none,id=stick,format=raw,file=$WORK_DIR/assertions.disk,cache=none,format=raw -device nec-usb-xhci,id=xhci -device usb-storage,bus=xhci.0,removable=true,drive=stick"
    fi
    if is_core_20_nested_system; then
        # use a bundle EFI bios by default
        PARAM_BIOS="-bios /usr/share/ovmf/OVMF.fd"
        OVMF_CODE="secboot"
        OVMF_VARS="ms"
        # In this case the kernel.efi is unsigned and signed with snaleoil certs
        if [ "$BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
            OVMF_VARS="snakeoil"
        fi

        if [ "$ENABLE_SECURE_BOOT" = "true" ]; then
            cp -f "/usr/share/OVMF/OVMF_VARS.$OVMF_VARS.fd" "$WORK_DIR/image/OVMF_VARS.$OVMF_VARS.fd"
            PARAM_BIOS="-drive file=/usr/share/OVMF/OVMF_CODE.$OVMF_CODE.fd,if=pflash,format=raw,unit=0,readonly -drive file=$WORK_DIR/image/OVMF_VARS.$OVMF_VARS.fd,if=pflash,format=raw"
            PARAM_MACHINE="-machine q35${ATTR_KVM} -global ICH9-LPC.disable_s3=1"
        fi

        if [ "$ENABLE_TPM" = "true" ]; then
            if ! snap list swtpm-mvo; then
                snap install swtpm-mvo --beta
            fi
            PARAM_TPM="-chardev socket,id=chrtpm,path=/var/snap/swtpm-mvo/current/swtpm-sock -tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-tis,tpmdev=tpm0"
        fi
        PARAM_IMAGE="-drive file=$CURRENT_IMAGE,cache=none,format=raw,id=disk1,if=none -device virtio-blk-pci,drive=disk1,bootindex=1"
    else
        PARAM_IMAGE="-drive file=$CURRENT_IMAGE,cache=none,format=raw"
    fi

    # Systemd unit is created, it is important to respect the qemu parameters order
    systemd_create_and_start_unit "$NESTED_VM" "${QEMU} \
        ${PARAM_SMP} \
        ${PARAM_CPU} \
        ${PARAM_MEM} \
        ${PARAM_TRACE} \
        ${PARAM_LOG} \
        ${PARAM_MACHINE} \
        ${PARAM_DISPLAY} \
        ${PARAM_NETWORK} \
        ${PARAM_BIOS} \
        ${PARAM_TPM} \
        ${PARAM_RANDOM} \
        ${PARAM_IMAGE} \
        ${PARAM_ASSERTIONS} \
        ${PARAM_SERIAL} \
        ${PARAM_MONITOR} \
        ${PARAM_USB} \
        ${PARAM_CD} "

    # wait for the nested-vm service to appear active
    wait_for_service "$NESTED_VM"

    # Wait until ssh is ready
    wait_for_ssh
}

get_current_image(){
    IMAGE_DIR="$(get_image_dir)"
    echo "$IMAGE_DIR/ubuntu-core-current.img"
}

start_nested_core_vm(){
    local IMAGE_DIR CURRENT_IMAGE
    IMAGE_DIR="$(get_image_dir)"
    CURRENT_IMAGE="$(get_current_image)"

    # In case the current image already exists, it needs to be reused and in that
    # case is neither required to copy the base image nor prepare the ssh
    if [ ! -f "$CURRENT_IMAGE" ]; then
        # As core18 systems use to fail to start the assertion disk when using the
        # snapshot feature, we copy the original image and use that copy to start
        # the VM.
        # Some tests however need to force stop and restart the VM with different
        # options, so if that env var is set, we will reuse the existing file if it
        # exists
        IMAGE_NAME="$(get_image_name core)"
        cp "$IMAGE_DIR/$IMAGE_NAME" "$CURRENT_IMAGE"

        # Start the nested core vm
        start_nested_core_vm_unit "$CURRENT_IMAGE"

        # configure ssh for first time
        prepare_ssh
    else
        # Start the nested core vm
        start_nested_core_vm_unit "$CURRENT_IMAGE"
    fi


}

create_nested_classic_vm(){
    local IMAGE_DIR IMAGE_NAME
    IMAGE_DIR="$(get_image_dir)"
    IMAGE_NAME="$(get_image_name classic)"

    prepare_image_dir
    if [ ! -f "$IMAGE_DIR/$IMAGE_NAME" ]; then
        # Get the cloud image
        local IMAGE_URL
        IMAGE_URL="$(get_image_url_for_nested_vm)"
        wget -P "$IMAGE_DIR" "$IMAGE_URL"
        download_nested_image "$IMAGE_URL" "$IMAGE_NAME"

        # Prepare the cloud-init configuration and configure image
        create_cloud_init_config "$WORK_DIR/seed"
        cloud-localds -H "$(hostname)" "$WORK_DIR/seed.img" "$WORK_DIR/seed"
    fi
}

start_nested_classic_vm(){
    local IMAGE QEMU IMAGE_DIR IMAGE_NAME
    QEMU="$(get_qemu_for_nested_vm)"
    IMAGE_DIR="$(get_image_dir)"
    IMAGE_NAME="$(get_image_name classic)"

    # Now qemu parameters are defined
    PARAM_SMP="-smp 1"
    # use only 2G of RAM for qemu-nested
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MEM="-m 4096"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MEM="-m 2048"
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi
    PARAM_DISPLAY="-nographic"
    PARAM_NETWORK="-net nic,model=virtio -net user,hostfwd=tcp::$SSH_PORT-:22"
    PARAM_MONITOR="-monitor tcp:127.0.0.1:$MON_PORT,server,nowait"
    PARAM_USB="-usb"
    PARAM_CPU=""
    PARAM_RANDOM="-object rng-random,id=rng0,filename=/dev/urandom -device virtio-rng-pci,rng=rng0"
    PARAM_SNAPSHOT="-snapshot"

    # with qemu-nested, we can't use kvm acceleration
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MACHINE="-machine ubuntu,accel=kvm"
        PARAM_CPU="-cpu host"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MACHINE=""
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi

    PARAM_IMAGE="-drive file=$IMAGE_DIR/$IMAGE_NAME,if=virtio"
    PARAM_SEED="-drive file=$WORK_DIR/seed.img,if=virtio"
    PARAM_SERIAL="-serial file:${WORK_DIR}/serial.log"
    PARAM_BIOS=""
    PARAM_TPM=""

    systemd_create_and_start_unit "$NESTED_VM" "${QEMU}  \
        ${PARAM_SMP} \
        ${PARAM_CPU} \
        ${PARAM_MEM} \
        ${PARAM_SNAPSHOT} \
        ${PARAM_MACHINE} \
        ${PARAM_DISPLAY} \
        ${PARAM_NETWORK} \
        ${PARAM_BIOS} \
        ${PARAM_TPM} \
        ${PARAM_RANDOM} \
        ${PARAM_IMAGE} \
        ${PARAM_SEED} \
        ${PARAM_SERIAL} \
        ${PARAM_MONITOR} \
        ${PARAM_USB} "

    wait_for_ssh
}

destroy_nested_vm(){
    systemd_stop_and_destroy_unit "$NESTED_VM"
}

execute_remote(){
    sshpass -p ubuntu ssh -p "$SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost "$*"
}

copy_remote(){
    sshpass -p ubuntu scp -P "$SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$@" user1@localhost:~
}

add_tty_chardev(){
    local CHARDEV_ID=$1
    local CHARDEV_PATH=$2
    echo "chardev-add file,path=$CHARDEV_PATH,id=$CHARDEV_ID" | nc -q 0 127.0.0.1 "$MON_PORT"
    echo "chardev added"
}

remove_chardev(){
    local CHARDEV_ID=$1
    echo "chardev-remove $CHARDEV_ID" | nc -q 0 127.0.0.1 "$MON_PORT"
    echo "chardev added"
}

add_usb_serial_device(){
    local DEVICE_ID=$1
    local CHARDEV_ID=$2
    local SERIAL_NUM=$3
    echo "device_add usb-serial,chardev=$CHARDEV_ID,id=$DEVICE_ID,serial=$SERIAL_NUM" | nc -q 0 127.0.0.1 "$MON_PORT"
    echo "device added"
}

del_device(){
    local DEVICE_ID=$1
    echo "device_del $DEVICE_ID" | nc -q 0 127.0.0.1 "$MON_PORT"
    echo "device deleted"
}

get_nested_core_revision_for_channel(){
    local CHANNEL=$1
    execute_remote "snap info core" | awk "/${CHANNEL}: / {print(\$4)}" | sed -e 's/(\(.*\))/\1/'
}

get_nested_core_revision_installed(){
    execute_remote "snap info core" | awk "/installed: / {print(\$3)}" | sed -e 's/(\(.*\))/\1/'
}

fetch_spread() {
    mkdir -p "$WORK_DIR"
    curl https://niemeyer.s3.amazonaws.com/spread-amd64.tar.gz | tar -xzv -C "$WORK_DIR"
    # make sure spread really exists
    test -x "$WORK_DIR/spread"
    echo "$WORK_DIR/spread"
}
