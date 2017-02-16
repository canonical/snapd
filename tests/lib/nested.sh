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
