#!/bin/bash

# shellcheck source=tests/lib/systems.sh
. "$TESTSLIB"/systems.sh

NESTED_WORK_DIR="${NESTED_WORK_DIR:-/tmp/work-dir}"
NESTED_IMAGES_DIR="$NESTED_WORK_DIR/images"
NESTED_RUNTIME_DIR="$NESTED_WORK_DIR/runtime"
NESTED_ASSETS_DIR="$NESTED_WORK_DIR/assets"
NESTED_LOGS_DIR="$NESTED_WORK_DIR/logs"


NESTED_VM=nested-vm
NESTED_SSH_PORT=8022
NESTED_MON_PORT=8888

nested_wait_for_ssh() {
    # TODO:UC20: the retry count should be lowered to something more reasonable.
    nested_retry_until_success 800 1 "true"
}

nested_wait_for_no_ssh() {
    nested_retry_while_success 200 1 "true"
}

nested_get_boot_id() {
    nested_exec "cat /proc/sys/kernel/random/boot_id"
}

nested_wait_for_reboot() {
    local initial_boot_id="$1"
    local retry wait last_boot_id
    retry=150
    wait=5

    last_boot_id=""
    while [ $retry -ge 0 ]; do
        retry=$(( retry - 1 ))
        # The get_boot_id could fail because the connection is broken due to the reboot
        last_boot_id="$(nested_get_boot_id)" || true
        if [[ "$last_boot_id" =~ .*-.*-.*-.*-.* ]] && [ "$last_boot_id" != "$initial_boot_id" ]; then
            break
        fi
        sleep "$wait"
    done

    [ "$last_boot_id" != "$initial_boot_id" ]
}

nested_retry_while_success() {
    local retry="$1"
    local wait="$2"
    shift 2

    while nested_exec "$@"; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for command '$*' to fail. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}

nested_retry_until_success() {
    local retry="$1"
    local wait="$2"
    shift 2

    until nested_exec "$@"; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for command '$*' to succeed. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}

# shellcheck disable=SC2120
nested_get_google_image_url_for_vm() {
    case "${1:-$SPREAD_SYSTEM}" in
        ubuntu-16.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.10-64*)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/groovy-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

# shellcheck disable=SC2120
nested_get_ubuntu_image_url_for_vm() {
    case "${1:-$SPREAD_SYSTEM}" in
        ubuntu-16.04-64*)
            echo "https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64*)
            echo "https://cloud-images.ubuntu.com/bionic/current/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64*)
            echo "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.10-64*)
            echo "https://cloud-images.ubuntu.com/groovy/current/groovy-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

# shellcheck disable=SC2120
nested_get_image_url_for_vm() {
    if [[ "$SPREAD_BACKEND" == google* ]]; then
        nested_get_google_image_url_for_vm "$@"
    else
        nested_get_ubuntu_image_url_for_vm "$@"
    fi
}

nested_get_cdimage_current_image_url() {
    local VERSION=$1
    local CHANNEL=$2
    local ARCH=$3

    echo "http://cdimage.ubuntu.com/ubuntu-core/$VERSION/$CHANNEL/current/ubuntu-core-$VERSION-$ARCH.img.xz"
}

nested_get_snap_rev_for_channel() {
    local SNAP=$1
    local CHANNEL=$2
    # This should be executed on remote system but as nested architecture is the same than the
    # host then the snap info is executed in the host
    snap info "$SNAP" | grep "$CHANNEL" | awk '{ print $4 }' | sed 's/.*(\(.*\))/\1/' | tr -d '\n'
}

nested_is_nested_system() {
    if nested_is_core_system || nested_is_classic_system ; then
        return 0
    else 
        return 1
    fi
}

nested_is_core_system() {
    if [ -z "${NESTED_TYPE:-}" ]; then
        echo "Variable NESTED_TYPE not defined."
        return 1
    fi

    test "$NESTED_TYPE" = "core"
}

nested_is_classic_system() {
    if [ -z "${NESTED_TYPE:-}" ]; then
        echo "Variable NESTED_TYPE not defined."
        return 1
    fi

    test "$NESTED_TYPE" = "classic"
}

nested_is_core_20_system() {
    is_focal_system
}

nested_is_core_18_system() {
    is_bionic_system
}

nested_is_core_16_system() {
    is_xenial_system
}

nested_refresh_to_new_core() {
    local NEW_CHANNEL=$1
    local CHANGE_ID
    if [ "$NEW_CHANNEL" = "" ]; then
        echo "Channel to refresh is not defined."
        exit 1
    else
        echo "Refreshing the core/snapd snap"
        if nested_is_classic_nested_system; then
            nested_exec "sudo snap refresh core --${NEW_CHANNEL}"
            nested_exec "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi

        if nested_is_core_18_system || nested_is_core_20_system; then
            nested_exec "sudo snap refresh snapd --${NEW_CHANNEL}"
            nested_exec "snap info snapd" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        else
            CHANGE_ID=$(nested_exec "sudo snap refresh core --${NEW_CHANNEL} --no-wait")
            nested_wait_for_no_ssh
            nested_wait_for_ssh
            # wait for the refresh to be done before checking, if we check too
            # quickly then operations on the core snap like reverting, etc. may
            # fail because it will have refresh-snap change in progress
            nested_exec "snap watch $CHANGE_ID"
            nested_exec "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi
    fi
}

nested_get_snakeoil_key() {
    local KEYNAME="PkKek-1-snakeoil"
    wget https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.key
    wget https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.pem
    echo "$KEYNAME"
}

nested_secboot_sign_file() {
    local FILE="$1"
    local KEY="$2"
    local CERT="$3"
    sbattach --remove "$FILE"
    sbsign --key "$KEY" --cert "$CERT" --output "$FILE" "$FILE"
}

nested_secboot_sign_gadget() {
    local GADGET_DIR="$1"
    local KEY="$2"
    local CERT="$3"
    nested_secboot_sign_file "$GADGET_DIR/shim.efi.signed" "$KEY" "$CERT"
}

nested_get_image_name() {
    local TYPE="$1"
    local SOURCE="${NESTED_CORE_CHANNEL}"
    local NAME="${NESTED_IMAGE_ID:-generic}"
    local VERSION="16"

    if nested_is_core_20_system; then
        VERSION="20"
    elif nested_is_core_18_system; then
        VERSION="18"
    fi

    if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
        SOURCE="custom"
    fi
    if [ "$(nested_get_extra_snaps | wc -l)" != "0" ]; then
        SOURCE="custom"
    fi
    echo "ubuntu-${TYPE}-${VERSION}-${SOURCE}-${NAME}.img"
}

nested_get_extra_snaps_path() {
    echo "${PWD}/extra-snaps"
}

nested_exec() {
    sshpass -p ubuntu ssh -p "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost "$@"
}

nested_exec_as() {
    local USER="$1"
    local PASSWD="$2"
    shift 2
    sshpass -p "$PASSWD" ssh -p "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$USER"@localhost "$@"
}

nested_copy() {
    sshpass -p ubuntu scp -P "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$@" user1@localhost:~
}

nested_copy_from_remote() {
    sshpass -p ubuntu scp -P "$SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost:"$1" "$2"
}

nested_add_tty_chardev() {
    local CHARDEV_ID=$1
    local CHARDEV_PATH=$2
    echo "chardev-add file,path=$CHARDEV_PATH,id=$CHARDEV_ID" | nc -q 0 127.0.0.1 "$NESTED_MON_PORT"
    echo "chardev added"
}

nested_remove_chardev() {
    local CHARDEV_ID=$1
    echo "chardev-remove $CHARDEV_ID" | nc -q 0 127.0.0.1 "$NESTED_MON_PORT"
    echo "chardev added"
}

nested_add_usb_serial_device() {
    local DEVICE_ID=$1
    local CHARDEV_ID=$2
    local SERIAL_NUM=$3
    echo "device_add usb-serial,chardev=$CHARDEV_ID,id=$DEVICE_ID,serial=$SERIAL_NUM" | nc -q 0 127.0.0.1 "$NESTED_MON_PORT"
    echo "device added"
}

nested_del_device() {
    local DEVICE_ID=$1
    echo "device_del $DEVICE_ID" | nc -q 0 127.0.0.1 "$NESTED_MON_PORT"
    echo "device deleted"
}

nested_get_core_revision_for_channel() {
    local CHANNEL=$1
    nested_exec "snap info core" | awk "/${CHANNEL}: / {print(\$4)}" | sed -e 's/(\(.*\))/\1/'
}

nested_get_core_revision_installed() {
    nested_exec "snap info core" | awk "/installed: / {print(\$3)}" | sed -e 's/(\(.*\))/\1/'
}

nested_fetch_spread() {
    if [ ! -f "$NESTED_WORK_DIR/spread" ]; then
        mkdir -p "$NESTED_WORK_DIR"
        curl https://niemeyer.s3.amazonaws.com/spread-amd64.tar.gz | tar -xzv -C "$NESTED_WORK_DIR"
        # make sure spread really exists
        test -x "$NESTED_WORK_DIR/spread"
        echo "$NESTED_WORK_DIR/spread"
    fi
}

nested_build_seed_cdrom() {
    local SEED_DIR="$1"
    local SEED_NAME="$2"
    local LABEL="$3"

    shift 3

    local ORIG_DIR=$PWD

    pushd "$SEED_DIR" || return 1 
    genisoimage -output "$ORIG_DIR/$SEED_NAME" -volid "$LABEL" -joliet -rock "$@"
    popd || return 1 
}