#!/bin/sh
execute_remote(){
    sshpass -p $PASSWORD ssh -p $SSH_PORT -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no $USER@localhost "$*"
}

prepare_ssh(){
    execute_remote "sudo adduser --extrausers --quiet --disabled-password --gecos '' test"
    execute_remote "echo test:ubuntu | sudo chpasswd"
    execute_remote "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/test-user"
}

wait_for_ssh(){
    retry=30
    while ! execute_remote true; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for ssh. Aborting!"
            exit 1
        fi
        sleep 1
    done
}

set_vars(){
    case "$NESTED_ARCH" in
    amd64)
        model_file=pc.model
        vm_unit_command="$(which qemu-system-x86_64) ${VM_UNIT_COMMAND_SUFFIX}"
        spread_system="ubuntu-core-16-64"
        ;;
    i386)
        model_file=pc-i386.model
        vm_unit_command="$(which qemu-system-i386) ${VM_UNIT_COMMAND_SUFFIX}"
        spread_system="ubuntu-core-16-64"
        ;;
    *)
        echo "unsupported architecture"
        exit 1
        ;;
    esac
}
