#!/bin/bash

# shellcheck source=tests/lib/systemd.sh
. $TESTSLIB/systemd.sh

wait_for_ssh(){
    retry=300
    while ! execute_remote true; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for ssh. Aborting!"
            exit 1
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

create_nested_core_vm(){
    # determine arch related vars
    case "$NESTED_ARCH" in
    amd64)
        QEMU="$(which qemu-system-x86_64)"
        ;;
    i386)
        QEMU="$(which qemu-system-i386)"
        ;;
    *)
        echo "unsupported architecture"
        exit 1
        ;;
    esac

    # create ubuntu-core image
    mkdir -p /tmp/work-dir
    /snap/bin/ubuntu-image --image-size 3G "$TESTSLIB/assertions/nested-${NESTED_ARCH}.model" --channel "$CORE_CHANNEL" --output ubuntu-core.img
    mv ubuntu-core.img /tmp/work-dir

    create_assertions_disk

    systemd_create_and_start_unit nested-vm "${QEMU} -m 1024 -nographic -net nic,model=virtio -net user,hostfwd=tcp::8022-:22 -drive file=/tmp/work-dir/ubuntu-core.img,if=virtio,cache=none -drive file=${PWD}/assertions.disk,if=virtio,cache=none"

    wait_for_ssh
    prepare_ssh
}

destroy_nested_core_vm(){
    systemd_stop_and_destroy_unit nested-vm
    rm -rf /tmp/work-dir
}

execute_remote(){
    sshpass -p ubuntu ssh -p 8022 -q -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost "$*"
}
