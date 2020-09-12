#!/bin/sh
set -ex

INSTANCE_IP="${1:-localhost}"
INSTANCE_PORT="${2:-8022}"
USER="${3:-$(whoami)}"
KEY_NAME="${4:-spread_external}"
PASSPHRASE="${5:-}"

execute_remote() {
    # shellcheck disable=SC2029
    ssh -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -p "$INSTANCE_PORT" "$USER@$INSTANCE_IP" "$@"
}

# Create the key in case it is not already created
if [ ! -f "$KEY_NAME" ] || [ ! -f "$KEY_NAME".pub ]; then
	ssh-keygen -t rsa -N "$PASSPHRASE" -f "$KEY_NAME"
fi

execute_remote "sudo mkdir -p /root/.ssh"
execute_remote "sudo chmod 700 /root/.ssh"
execute_remote "sudo tee -a /root/.ssh/authorized_keys > /dev/null" < "$KEY_NAME".pub
