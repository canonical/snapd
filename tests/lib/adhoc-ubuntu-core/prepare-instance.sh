#!/bin/sh
set -x

INSTANCE_IP="${1:-localhost}"
INSTANCE_PORT="${2:-8022}"
USER="${3:-$(whoami)}"
SNAPBUILD_BINARY="${4:-$(which snapbuild)}"

execute_remote(){
    ssh -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -p $INSTANCE_PORT $USER@$INSTANCE_IP "$@"
}

scp -P $INSTANCE_PORT "$SNAPBUILD_BINARY" $USER@$INSTANCE_IP:/home/$USER

execute_remote "sudo rm -rf /home/gopath"
execute_remote "sudo adduser --extrausers --quiet --disabled-password --gecos '' test"
execute_remote "(echo test:ubuntu | sudo chpasswd)"
execute_remote "(echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test)"
execute_remote "sudo cp /home/$USER/snapbuild /home/test"
