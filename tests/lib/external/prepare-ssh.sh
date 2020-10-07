#!/bin/sh
set -ex

INSTANCE_IP="${1:-localhost}"
INSTANCE_PORT="${2:-8022}"
USER_NAME="${3:-$(whoami)}"
USER_PASS="${4:-}"
USER_TYPE="${5:-}"

SSHPASS_PARAM=""

execute_remote(){
    # shellcheck disable=SC2029
    $SSHPASS_PARAM ssh -q -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -p "$INSTANCE_PORT" "$USER_NAME@$INSTANCE_IP" "$@"
}

USER_TYPE_PARAM=""
if [ -z "$USER_TYPE" ]; then
    USER_TYPE_PARAM="--extrausers"
fi

# It is needed to use sshpass to connect using password
if [ -n "$USER_PASS" ]; then
    SSHPASS_PARAM="sshpass -p $USER_PASS"
fi

# When user is root, neither sudo nor sshpass is needed
SUDO_PARAM="sudo"
if [ "$USER_NAME" = "root" ]; then
    SUDO_PARAM=""
elif [ -n "$USER_PASS" ]; then
	SUDO_PARAM="sshpass -p $USER_PASS sudo"
fi

execute_remote "$SUDO_PARAM adduser --uid 12345 $USER_TYPE_PARAM --quiet --disabled-password --gecos '' test"
execute_remote "echo test:ubuntu | $SUDO_PARAM chpasswd"
execute_remote "echo 'test ALL=(ALL) NOPASSWD:ALL' | $SUDO_PARAM tee /etc/sudoers.d/create-user-test"

execute_remote "$SUDO_PARAM adduser $USER_TYPE_PARAM --quiet --disabled-password --gecos '' external"
execute_remote "echo external:ubuntu | $SUDO_PARAM chpasswd"
execute_remote "echo 'external ALL=(ALL) NOPASSWD:ALL' | $SUDO_PARAM tee /etc/sudoers.d/create-user-external"
