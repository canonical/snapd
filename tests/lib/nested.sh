#!/bin/bash

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB"/systemd.sh

WORK_DIR=/tmp/work-dir
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

prepare_ssh(){
    execute_remote "sudo adduser --extrausers --quiet --disabled-password --gecos '' test"
    execute_remote "echo test:ubuntu | sudo chpasswd"
    execute_remote "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/test-user"
}

create_assertions_disk(){
    dd if=/dev/null of=assertions.disk bs=1M seek=1
    mkfs.ext4 -F assertions.disk
    mkdir /mnt/assertions
    mount -t ext4 -o loop assertions.disk /mnt/assertions
    cp "$TESTSLIB/assertions/auto-import.assert" /mnt/assertions
    umount /mnt/assertions && rm -rf /mnt/assertions
}

get_qemu_for_nested_vm(){
    case "$NESTED_ARCH" in
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

get_image_url_for_nested_vm(){
    case "$NESTED_SYSTEM" in
    xenial|trusty)
        echo "https://cloud-images.ubuntu.com/${NESTED_SYSTEM}/current/${NESTED_SYSTEM}-server-cloudimg-${NESTED_ARCH}-disk1.img"
        ;;
    bionic)
        echo "https://cloud-images.ubuntu.com/bionic/current/bionic-server-cloudimg-${NESTED_ARCH}.img"
        ;;
    *)
        echo "unsupported system"
        exit 1
        ;;
    esac

}

create_nested_core_vm(){
    local UBUNTU_IMAGE
    UBUNTU_IMAGE=$(command -v ubuntu-image)

    # create ubuntu-core image
    local EXTRA_SNAPS=""
    if [ -d "${PWD}/extra-snaps" ] && [ "$(find "${PWD}/extra-snaps/" -type f -name "*.snap" | wc -l)" -gt 0 ]; then
        EXTRA_SNAPS="--extra-snaps ${PWD}/extra-snaps/*.snap"
    fi
    mkdir -p "$WORK_DIR"

    "$UBUNTU_IMAGE" --image-size 3G "$TESTSLIB/assertions/nested-${NESTED_ARCH}.model" \
        --channel "$CORE_CHANNEL" \
        --output "$WORK_DIR/ubuntu-core.img" "$EXTRA_SNAPS"

    create_assertions_disk
    start_nested_core_vm
    if wait_for_ssh; then
        prepare_ssh
    else
        echo "ssh not stablished, exiting..."
        exit 1
    fi
}

start_nested_core_vm(){
    local QEMU
    QEMU=$(get_qemu_for_nested_vm)
    systemd_create_and_start_unit nested-vm "${QEMU} -m 2048 -nographic \
        -net nic,model=virtio -net user,hostfwd=tcp::$SSH_PORT-:22 \
        -drive file=$WORK_DIR/ubuntu-core.img,if=virtio,cache=none,format=raw \
        -drive file=${PWD}/assertions.disk,if=virtio,cache=none,format=raw \
        -monitor tcp:127.0.0.1:$MON_PORT,server,nowait -usb \
        -machine accel=kvm"
    if ! wait_for_ssh; then
        systemctl restart nested-vm
    fi
}

create_nested_classic_vm(){
    mkdir -p "$WORK_DIR"

    # Get the cloud image
    local IMAGE_URL
    IMAGE_URL=$(get_image_url_for_nested_vm)
    wget -P "$WORK_DIR" "$IMAGE_URL"
    # Check the image
    local IMAGE
    IMAGE=$(ls $WORK_DIR/*.img)
    test "$(echo "$IMAGE" | wc -l)" = "1"

    # Prepare the cloud-init configuration
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
    cloud-localds -H "$(hostname)" "$WORK_DIR/seed.img" "$WORK_DIR/seed"

    # Start the vm
    start_nested_classic_vm "$IMAGE"
}

start_nested_classic_vm(){
    local IMAGE=$1
    local QEMU
    QEMU=$(get_qemu_for_nested_vm)

    systemd_create_and_start_unit nested-vm "${QEMU} -m 2048 -nographic \
        -net nic,model=virtio -net user,hostfwd=tcp::$SSH_PORT-:22 \
        -drive file=$IMAGE,if=virtio \
        -drive file=$WORK_DIR/seed.img,if=virtio \
        -monitor tcp:127.0.0.1:$MON_PORT,server,nowait -usb \
        -machine accel=kvm"
    wait_for_ssh
}

destroy_nested_vm(){
    systemd_stop_and_destroy_unit nested-vm
    rm -rf "$WORK_DIR"
}

execute_remote(){
    sshpass -p ubuntu ssh -p "$SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost "$*"
}

copy_remote(){
    sshpass -p ubuntu scp -P "$SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$*" user1@localhost:~
}

add_tty_chardev(){
    local CHARDEV_ID=$1
    local CHARDEV_PATH=$2
    echo "chardev-add tty,path=$CHARDEV_PATH,id=$CHARDEV_ID" | nc -q 0 127.0.0.1 "$MON_PORT"
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
    echo "device_add usb-serial,chardev=$CHARDEV_ID,id=$DEVICE_ID" | nc -q 0 127.0.0.1 "$MON_PORT"
    echo "device added"
}

del_device(){
    local DEVICE_ID=$1
    echo "device_del $DEVICE_ID" | nc -q 0 127.0.0.1 "$MON_PORT"
    echo "device deleted"
}
