#!/bin/sh
set -ex

INSTANCE_IP="${1:-localhost}"
INSTANCE_PORT="${2:-8022}"
USER="${3:-$(whoami)}"

execute_remote(){
    ssh -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -p $INSTANCE_PORT $USER@$INSTANCE_IP "$@"
}

execute_remote "sudo adduser --extrausers --quiet --disabled-password --gecos '' test"
execute_remote "echo test:ubuntu | sudo chpasswd"
execute_remote "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test"
