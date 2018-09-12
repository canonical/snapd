#!/bin/bash

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB"/systemd.sh

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

create_nested_core_vm(){
    # determine arch related vars
    case "$NESTED_ARCH" in
    amd64)
        QEMU="$(command -v qemu-system-x86_64)"
        ;;
    i386)
        QEMU="$(command -v qemu-system-i386)"
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
    start_nested_vm
    wait_for_ssh
    prepare_ssh
}

start_nested_vm(){
    systemd_create_and_start_unit nested-vm "${QEMU} -m 2048 -nographic -net nic,model=virtio -net user,hostfwd=tcp::8022-:22 -drive file=/tmp/work-dir/ubuntu-core.img,if=virtio,cache=none,format=raw -drive file=${PWD}/assertions.disk,if=virtio,cache=none,format=raw"
    if ! wait_for_ssh; then
        systemctl restart nested-vm
    fi
}

destroy_nested_core_vm(){
    systemd_stop_and_destroy_unit nested-vm
    rm -rf /tmp/work-dir
}

execute_remote(){
    sshpass -p ubuntu ssh -p 8022 -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost "$*"
}   
