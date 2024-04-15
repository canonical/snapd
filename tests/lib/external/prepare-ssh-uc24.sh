#!/bin/sh
set -ex

INSTANCE_IP="${1:-localhost}"
INSTANCE_PORT="${2:-8022}"
USER="${3:-$(whoami)}"

execute_remote(){
    # shellcheck disable=SC2029
    ssh -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -p "$INSTANCE_PORT" "$USER@$INSTANCE_IP" "$@"
}

execute_remote "sudo useradd --uid 12345 --extrausers test"
execute_remote "echo test:ubuntu123 | sudo chpasswd"
execute_remote "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test"

execute_remote "sudo useradd --extrausers external"
execute_remote "echo external:ubuntu123 | sudo chpasswd"
execute_remote "echo 'external ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-external"
