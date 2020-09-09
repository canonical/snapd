#!/bin/sh
set -ex

INSTANCE_IP="${1:-localhost}"
INSTANCE_PORT="${2:-8022}"
USER="${3:-$(whoami)}"
CERT_NAME="${4:-spread_external}"
PASSPHRASE="${5:-ubuntu}"

execute_remote() {
    # shellcheck disable=SC2029
    ssh -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -p "$INSTANCE_PORT" "$USER@$INSTANCE_IP" "$@"
}

# Create certificates in case those are not stored
if [ ! -f ~/.ssh/"$CERT_NAME" ] || [ ! -f ~/.ssh/"$CERT_NAME".pub ]; then
	ssh-keygen -t rsa -N "$PASSPHRASE" -f ~/.ssh/"$CERT_NAME"
fi

execute_remote "sudo mkdir -p /root/.ssh"
execute_remote "sudo chmod 700 /root/.ssh"
cat ~/.ssh/"$CERT_NAME".pub | execute_remote "sudo tee -a /root/.ssh/authorized_keys > /dev/null"
