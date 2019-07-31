#!/bin/bash

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB"/systemd.sh

WORK_DIR=/tmp/work-dir
NESTED_VM=nested-vm
SSH_PORT=8022
MON_PORT=8888

wait_for_ssh(){
    retry=150
    while ! execute_remote true; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for ssh. Aborting!"
            return 1
        fi
        sleep 1
    done
}

wait_for_no_ssh(){
    retry=150
    while execute_remote true; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for no ssh. Aborting!"
            return 1
        fi
        sleep 1
    done
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

get_image_url_for_nested_vm(){
    case "$SPREAD_SYSTEM" in
    ubuntu-16.04-64)
        echo "https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img"
        ;;
    ubuntu-18.04-64)
        echo "https://cloud-images.ubuntu.com/bionic/current/bionic-server-cloudimg-amd64.img"
        ;;
    *)
        echo "unsupported system"
        exit 1
        ;;
    esac
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

is_core_18_nested_system(){
    if [ "$SPREAD_SYSTEM" = ubuntu-18.04-64 ]; then
        return 0
    fi
    return 1
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
            execute_remote "snap info core" | grep -E "^tracking: +${NEW_CHANNEL}"
        fi

        if is_core_18_nested_system; then
            execute_remote "snap refresh snapd --${NEW_CHANNEL}"
            execute_remote "snap info snapd" | grep -E "^tracking: +${NEW_CHANNEL}"
        else
            execute_remote "snap refresh core --${NEW_CHANNEL}"
            wait_for_no_ssh
            wait_for_ssh
            execute_remote "snap info core" | grep -E "^tracking: +${NEW_CHANNEL}"
        fi
    fi
}

cleanup_nested_env(){
    rm -rf "$WORK_DIR"
}

create_nested_core_vm(){
    mkdir -p "$WORK_DIR/image"
    if [ ! -f "$WORK_DIR/image/ubuntu-core.img" ]; then
        local UBUNTU_IMAGE
        UBUNTU_IMAGE=$(command -v ubuntu-image)

        # create ubuntu-core image
        local EXTRA_SNAPS=""
        if [ -d "${PWD}/extra-snaps" ] && [ "$(find "${PWD}/extra-snaps/" -type f -name "*.snap" | wc -l)" -gt 0 ]; then
            EXTRA_SNAPS="--extra-snaps ${PWD}/extra-snaps/*.snap"
        fi

        local NESTED_MODEL=""
        case "$SPREAD_SYSTEM" in
        ubuntu-16.04-64)
            NESTED_MODEL="$TESTSLIB/assertions/nested-amd64.model"
            ;;
        ubuntu-18.04-64)
            NESTED_MODEL="$TESTSLIB/assertions/nested-18-amd64.model"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac

        "$UBUNTU_IMAGE" --image-size 3G "$NESTED_MODEL" \
            --channel "$CORE_CHANNEL" \
            --output "$WORK_DIR/image/ubuntu-core.img" "$EXTRA_SNAPS"

        create_assertions_disk
    fi
}

start_nested_core_vm(){
    local IMAGE QEMU
    QEMU=$(get_qemu_for_nested_vm)
    # As core18 systems use to fail to start the assetion disk when using the
    # snapshot feature, we copy the original image and use that copy to start
    # the VM.
    IMAGE="$WORK_DIR/image/ubuntu-core-new.img"

    cp -f "$WORK_DIR/image/ubuntu-core.img" "$IMAGE"
    systemd_create_and_start_unit "$NESTED_VM" "${QEMU} -m 2048 -nographic \
        -net nic,model=virtio -net user,hostfwd=tcp::$SSH_PORT-:22 \
        -drive file=$IMAGE,cache=none,format=raw \
        -drive file=$WORK_DIR/assertions.disk,cache=none,format=raw \
        -monitor tcp:127.0.0.1:$MON_PORT,server,nowait -usb \
        -machine accel=kvm"

    if ! wait_for_ssh; then
        systemctl restart nested-vm
    fi

    if wait_for_ssh; then
        prepare_ssh
    else
        echo "ssh not established, exiting..."
        exit 1
    fi
}

create_seed_image(){
    cat <<EOF > "$WORK_DIR/seed"
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
        create_seed_image
        cloud-localds -H "$(hostname)" "$WORK_DIR/seed.img" "$WORK_DIR/seed"
    fi
}

start_nested_classic_vm(){
    local IMAGE QEMU
    IMAGE=$(ls $WORK_DIR/image/*.img)
    QEMU=$(get_qemu_for_nested_vm)

    systemd_create_and_start_unit "$NESTED_VM" "${QEMU} -m 2048 -nographic \
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
