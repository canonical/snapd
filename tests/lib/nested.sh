#!/bin/bash

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB"/systemd.sh

WORK_DIR=/tmp/work-dir
NESTED_VM=nested-vm
SSH_PORT=8022
MON_PORT=8888

wait_for_ssh(){
    retry=180
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
    retry=120
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

test_ssh(){
    sshpass -p ubuntu ssh -p 8022 -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost true
}

prepare_ssh(){
    execute_remote "sudo adduser --extrausers --quiet --disabled-password --gecos '' test"
    execute_remote "echo test:ubuntu | sudo chpasswd"
    execute_remote "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/test-user"
}

create_assertions_disk(){
    mkdir -p "$WORK_DIR"
    dd if=/dev/null of="$WORK_DIR/assertions.disk" bs=1M seek=1
    mkfs.ext4 -F "$WORK_DIR/assertions.disk"
    debugfs -w -R "write $TESTSLIB/assertions/auto-import.assert auto-import.assert" "$WORK_DIR/assertions.disk"
}

get_qemu_for_nested_vm(){
    command -v qemu-system-x86_64
}

get_google_image_url_for_nested_vm(){
    case "$SPREAD_SYSTEM" in
        ubuntu-16.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-19.10-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/eoan-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/focal-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

get_ubuntu_image_url_for_nested_vm(){
    case "$SPREAD_SYSTEM" in
        ubuntu-16.04-64)
            echo "https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64)
            echo "https://cloud-images.ubuntu.com/bionic/current/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-19.10-64)
            echo "https://cloud-images.ubuntu.com/eoan/current/eoan-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64)
            echo "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

get_image_url_for_nested_vm(){
    if [[ "$SPREAD_BACKEND" == google* ]]; then
        get_google_image_url_for_nested_vm
    else
        get_ubuntu_image_url_for_nested_vm
    fi
}

is_core_nested_system(){
    if [ -z "$NESTED_TYPE" ]; then
        echo "Variable NESTED_TYPE not defined. Exiting..."
        exit 1
    fi

    if [ "$NESTED_TYPE" = core ]; then
        return 0
    fi
    return 1
}

is_classic_nested_system(){
    if [ -z "$NESTED_TYPE" ]; then
        echo "Variable NESTED_TYPE not defined. Exiting..."
        exit 1
    fi

    if [ "$NESTED_TYPE" = classic ]; then
        return 0
    fi
    return 1
}

is_focal_system(){
    test "$(lsb_release -cs)" = focal
}

is_core_20_nested_system(){
    if [ "$SPREAD_SYSTEM" = ubuntu-20.04-64 ]; then
        return 0
    fi
    return 1
}

is_bionic_system(){
    test "$(lsb_release -cs)" = bionic
}

is_core_18_nested_system(){
    if [ "$SPREAD_SYSTEM" = ubuntu-18.04-64 ]; then
        return 0
    fi
    return 1
}

is_xenial_system(){
    test "$(lsb_release -cs)" = xenial
}

is_core_16_nested_system(){
    if [ "$SPREAD_SYSTEM" = ubuntu-16.04-64 ]; then
        return 0
    fi
    return 1
}

refresh_to_new_core(){
    local NEW_CHANNEL=$1
    if [ "$NEW_CHANNEL" = "" ]; then
        echo "Channel to refresh is not defined."
        exit 1
    else
        echo "Refreshing the core/snapd snap"
        if is_classic_nested_system; then
            execute_remote "snap refresh core --${NEW_CHANNEL}"
            execute_remote "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi

        if is_core_18_nested_system || is_core_20_nested_system; then
            execute_remote "snap refresh snapd --${NEW_CHANNEL}"
            execute_remote "snap info snapd" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        else
            execute_remote "snap refresh core --${NEW_CHANNEL}"
            wait_for_no_ssh
            wait_for_ssh
            execute_remote "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi
    fi
}

cleanup_nested_env(){
    rm -rf "$WORK_DIR"
}

create_nested_core_vm(){
    local BUILD_FROM_CURRENT="${1:-}"

    mkdir -p "$WORK_DIR/image"
    if [ ! -f "$WORK_DIR/image/ubuntu-core.img" ]; then
        local UBUNTU_IMAGE

        if ! snap list ubuntu-image; then
            snap install ubuntu-image --classic
        fi
        UBUNTU_IMAGE=/snap/bin/ubuntu-image

        # create ubuntu-core image
        local EXTRA_FUNDAMENTAL=""
        local EXTRA_SNAPS=""
        if [ -d "${PWD}/extra-snaps" ] && [ "$(find "${PWD}/extra-snaps/" -type f -name "*.snap" | wc -l)" -gt 0 ]; then
            EXTRA_SNAPS="--snap ${PWD}/extra-snaps/*.snap"
        fi

        local NESTED_MODEL=""
        case "$SPREAD_SYSTEM" in
        ubuntu-16.04-64)
            NESTED_MODEL="$TESTSLIB/assertions/nested-amd64.model"
            ;;
        ubuntu-18.04-64)
            NESTED_MODEL="$TESTSLIB/assertions/nested-18-amd64.model"
            ;;
        ubuntu-20.04-64)
            NESTED_MODEL="$TESTSLIB/assertions/nested-20-amd64.model"

            # shellcheck source=tests/lib/prepare.sh
            . "$TESTSLIB"/prepare.sh
            snap download --basename=pc-kernel --channel="20/edge" pc-kernel
            uc20_build_initramfs_kernel_snap "$PWD/pc-kernel.snap" "$WORK_DIR/image"

            EXTRA_FUNDAMENTAL="--snap $WORK_DIR/image/pc-kernel_*.snap"
            chmod 0600 "$WORK_DIR"/image/pc-kernel_*.snap
            rm -f "$PWD/pc-kernel.snap"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac

        if [ "$BUILD_FROM_CURRENT" = "true" ]; then
            if is_core_16_nested_system; then
                echo "Build from current branch is not supported yet for uc16"
                exit 1
            fi
            # shellcheck source=tests/lib/prepare.sh
            . "$TESTSLIB"/prepare.sh
            snap download --channel="latest/edge" snapd
            repack_snapd_snap_with_deb_content_and_run_mode_firstboot_tweaks "$PWD/new-snapd" "true"
            EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $PWD/new-snapd/snapd_*.snap"
        fi

        "$UBUNTU_IMAGE" --image-size 10G "$NESTED_MODEL" \
            --channel "$CORE_CHANNEL" \
            --output "$WORK_DIR/image/ubuntu-core.img" \
            "$EXTRA_FUNDAMENTAL" \
            "$EXTRA_SNAPS"

        if [ "$BUILD_WITH_CLOUD_INIT" = "true" ]; then
            configure_cloud_init_nested_core_vm
        else
            create_assertions_disk
        fi
    fi
}

configure_cloud_init_nested_core_vm(){
    create_cloud_init_config "$WORK_DIR/data.cfg"

    loops=$(kpartx -avs "$WORK_DIR/image/ubuntu-core.img"  | cut -d' ' -f 3)
    part=$(echo "$loops" | tail -1)
    tmp=$(mktemp -d)
    mount "/dev/mapper/$part" "$tmp"

    mkdir -p "$tmp/ubuntu-seed/data/etc/cloud/cloud.cfg.d/"
    cp "$WORK_DIR/data.cfg" "$tmp/ubuntu-seed/data/etc/cloud/cloud.cfg.d/"

    umount "$tmp"
    kpartx -d "$WORK_DIR/image/ubuntu-core.img"
}

start_nested_core_vm(){
    local IMAGE QEMU
    QEMU=$(get_qemu_for_nested_vm)
    # As core18 systems use to fail to start the assetion disk when using the
    # snapshot feature, we copy the original image and use that copy to start
    # the VM.
    IMAGE_FILE="$WORK_DIR/image/ubuntu-core-new.img"
    cp -f "$WORK_DIR/image/ubuntu-core.img" "$IMAGE_FILE"

    # Now qemu parameters are defined
    PARAM_MEM="-m 2048"
    PARAM_DISPLAY="-nographic"
    PARAM_EXTRA="-machine accel=kvm"
    PARAM_NETWORK="-net nic,model=virtio -net user,hostfwd=tcp::$SSH_PORT-:22"
    PARAM_MONITOR="-monitor tcp:127.0.0.1:$MON_PORT,server,nowait -usb"
    if is_core_20_nested_system; then
        if ! is_focal_system; then
            cp /etc/apt/sources.list /etc/apt/sources.list.back
            echo "deb http://us-east1.gce.archive.ubuntu.com/ubuntu/ focal main restricted" >> /etc/apt/sources.list
            apt update
            apt install -y ovmf
            mv /etc/apt/sources.list.back /etc/apt/sources.list
            apt update
        fi
        cp -f /usr/share/OVMF/OVMF_VARS.snakeoil.fd "$WORK_DIR/image/OVMF_VARS.snakeoil.fd"
        if ! snap list swtpm-mvo; then
            snap install swtpm-mvo --beta
        fi

        PARAM_CPU="-smp 2"
        PARAM_BIOS="-drive file=/usr/share/OVMF/OVMF_CODE.secboot.fd,if=pflash,format=raw,unit=0,readonly=on -drive file=$WORK_DIR/image/OVMF_VARS.snakeoil.fd,if=pflash,format=raw,unit=1"
        PARAM_ASSERTIONS=""
        PARAM_IMAGE="-drive file=$IMAGE_FILE,cache=none,format=raw,id=disk1,if=none -device virtio-blk-pci,drive=disk1,bootindex=1"
        PARAM_MACHINE="-machine q35 -global ICH9-LPC.disable_s3=1"
        PARAM_TPM="-chardev socket,id=chrtpm,path=/var/snap/swtpm-mvo/current/swtpm-sock -tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-tis,tpmdev=tpm0"
    else
        PARAM_CPU=""
        PARAM_BIOS=""
        PARAM_ASSERTIONS="-drive file=$WORK_DIR/assertions.disk,cache=none,format=raw"
        PARAM_IMAGE="-drive file=$IMAGE_FILE,cache=none,format=raw"
        PARAM_MACHINE=""
        PARAM_TPM=""
    fi

    # Systemd unit is created, it is important to respect the qemu parameters order
    systemd_create_and_start_unit "$NESTED_VM" "${QEMU} \
        ${PARAM_CPU} \
        ${PARAM_MEM} \
        ${PARAM_MACHINE} \
        ${PARAM_DISPLAY} \
        ${PARAM_NETWORK} \
        ${PARAM_BIOS} \
        ${PARAM_TPM} \
        ${PARAM_IMAGE} \
        ${PARAM_ASSERTIONS} \
        ${PARAM_MONITOR} \
        ${PARAM_EXTRA} "

    # Wait until the system has been initialized
    #if ! wait_for_ssh; then
    #    # In case it is not possible to connect through ssh restart the vm
    #    systemctl stop "$NESTED_VM"
    #    sleep 5
    #    systemctl start "$NESTED_VM"
    #fi

    # Wait until ssh is ready and configure ssh
    if wait_for_ssh; then
        prepare_ssh
    else
        echo "ssh not established, exiting..."
        exit 1
    fi
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
EOF
}

create_nested_classic_vm(){
    mkdir -p "$WORK_DIR/image"
    IMAGE=$(ls $WORK_DIR/image/*.img || true)
    if [ -z "$IMAGE" ]; then
        # Get the cloud image
        local IMAGE_URL
        IMAGE_URL=$(get_image_url_for_nested_vm)
        wget -P "$WORK_DIR/image" "$IMAGE_URL"
        # Check the image
        local IMAGE
        IMAGE=$(ls $WORK_DIR/image/*.img)
        test "$(echo "$IMAGE" | wc -l)" = "1"

        # Prepare the cloud-init configuration and configure image
        create_cloud_init_config "$WORK_DIR/seed"
        cloud-localds -H "$(hostname)" "$WORK_DIR/seed.img" "$WORK_DIR/seed"
    fi
}

get_nested_classic_image_path() {
    ls $WORK_DIR/image/*.img
}

start_nested_classic_vm(){
    local IMAGE QEMU
    IMAGE=$(ls $WORK_DIR/image/*.img)
    QEMU=$(get_qemu_for_nested_vm)

    systemd_create_and_start_unit "$NESTED_VM" "${QEMU} -m 2048 -nographic  \
        -net nic,model=virtio -net user,hostfwd=tcp::$SSH_PORT-:22 \
        -drive file=$IMAGE,if=virtio \
        -drive file=$WORK_DIR/seed.img,if=virtio \
        -monitor tcp:127.0.0.1:$MON_PORT,server,nowait -usb \
        -snapshot -machine accel=kvm"
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
