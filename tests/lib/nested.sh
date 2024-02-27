#!/bin/bash

: "${NESTED_WORK_DIR:=/tmp/work-dir}"
: "${NESTED_IMAGES_DIR:=${NESTED_WORK_DIR}/images}"
: "${NESTED_RUNTIME_DIR:=${NESTED_WORK_DIR}/runtime}"
: "${NESTED_ASSETS_DIR:=${NESTED_WORK_DIR}/assets}"
: "${NESTED_LOGS_DIR:=${NESTED_WORK_DIR}/logs}"
: "${NESTED_ARCHITECTURE:=amd64}"

: "${NESTED_VM:=nested-vm}"
: "${NESTED_SSH_PORT:=8022}"
: "${NESTED_MON_PORT:=8888}"

: "${NESTED_CUSTOM_MODEL:=}"
: "${NESTED_CUSTOM_AUTO_IMPORT_ASSERTION:=}"
: "${NESTED_FAKESTORE_BLOB_DIR:=${NESTED_WORK_DIR}/fakestore/blobs}"
: "${NESTED_SIGN_SNAPS_FAKESTORE:=false}"
: "${NESTED_FAKESTORE_SNAP_DECL_PC_GADGET:=}"
: "${NESTED_UBUNTU_IMAGE_SNAPPY_FORCE_SAS_URL:=}"
: "${NESTED_UBUNTU_IMAGE_PRESEED_KEY:=}"

: "${NESTED_DISK_PHYSICAL_BLOCK_SIZE:=512}"
: "${NESTED_DISK_LOGICAL_BLOCK_SIZE:=512}"

nested_wait_for_ssh() {
    local retry=${1:-800}
    local wait=${2:-1}

    until remote.exec "true" &>/dev/null; do
        if [ "$retry" -le 0 ]; then
            return 1
        fi
        retry=$(( retry - 1 ))
        sleep "$wait"
    done
}

nested_wait_for_no_ssh() {
    local retry=${1:-200}
    local wait=${2:-1}

    while remote.exec "true" &>/dev/null; do
        if [ "$retry" -le 0 ]; then
            return 1
        fi
        retry=$(( retry - 1 ))
        sleep "$wait"
    done
}

nested_wait_vm_ready() {
    echo "Waiting the vm is ready to be used"
    local retry=${1:-120}
    local log_limit=${2:-30}

    local output_lines=0
    local serial_log="$NESTED_LOGS_DIR"/serial.log
    while true; do
        # Check the timeout is reached
        if [ "$retry" -le 0 ]; then
            echo "Timed out waiting for vm ready. Aborting!"
            return 1
        fi
        retry=$(( retry - 1 ))

        # Check the vm is active
        if ! systemctl is-active "$NESTED_VM"; then
            echo "Unit $NESTED_VM is not active. Aborting!"
            return 1
        fi

        # Check during $limit seconds that the serial log is growing
        # shellcheck disable=SC2016
        if ! retry -n "$log_limit" --wait 1 --env serial_log="$serial_log" --env output_lines="$output_lines" \
            sh -c 'test "$(wc -l <"$serial_log")" -gt "$output_lines"';
        then
            echo "Serial log for $NESTED_VM unit is not producing output, Aborting!"
            return 1
        fi
        output_lines="$(wc -l <"$serial_log")"

        # Check no infinite loops during boot
        if nested_is_core_20_system || nested_is_core_22_system; then
            test "$(grep -c -E "Command line:.*snapd_recovery_mode=install" "$serial_log")" -le 1
            test "$(grep -c -E "Command line:.*snapd_recovery_mode=run" "$serial_log")" -le 1
        elif nested_is_core_16_system || nested_is_core_18_system; then
            test "$(grep -c -E "Command line:.*BOOT_IMAGE=\(loop\)/kernel.img" "$serial_log")" -le 1
        fi

        # Check if ssh connection can be established, and return if it is possible
        if nested_wait_for_ssh 1 1; then
            echo "SSH connection ready"
            return
        fi

        sleep 3
    done

    nested_check_unit_stays_active "$NESTED_VM" 2 1
}

nested_wait_for_snap_command() {
    # In this function the remote retry command cannot be used because it could
    # be executed before the tool is deployed.
    local retry=${1:-200}
    local wait=${2:-1}

    while ! remote.exec "command -v snap" &>/dev/null; do
        if [ "$retry" -le 0 ]; then
            echo "Timed out waiting for command 'command -v snap' to success. Aborting!"
            return 1
        fi
        retry=$(( retry - 1 ))
        sleep "$wait"
    done
}

nested_check_unit_stays_active() {
    local nested_unit="${1:-$NESTED_VM}"
    local retry=${2:-5}
    local wait=${3:-1}

    while [ "$retry" -ge 0 ]; do
        retry=$(( retry - 1 ))

        if ! systemctl is-active "$nested_unit"; then
            echo "Unit $nested_unit is not active. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}

nested_get_boot_id() {
    remote.exec "cat /proc/sys/kernel/random/boot_id"
}

nested_wait_for_reboot() {
    local initial_boot_id="$1"
    local last_boot_id="$initial_boot_id"
    local retry=150
    local wait=5

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

nested_uc20_transition_to_system_mode() {
    local recovery_system="$1"
    local mode="$2"

    if ! nested_is_core_20_system && ! nested_is_core_22_system; then
        echo "Transition can be done just on uc20 and uc22 systems, exiting..."
        exit 1
    fi

    local current_boot_id
    current_boot_id=$(nested_get_boot_id)
    remote.exec "sudo snap reboot --$mode $recovery_system"
    nested_wait_for_reboot "$current_boot_id"

    # verify we are now in the requested mode
    if ! remote.exec "cat /proc/cmdline" | MATCH "snapd_recovery_mode=$mode"; then
        return 1
    fi

    # Copy tools to be used on tests
    nested_prepare_tools
}

nested_prepare_ssh() {
    remote.exec "sudo adduser --uid 12345 --extrausers --quiet --disabled-password --gecos '' test"
    remote.exec "echo test:ubuntu | sudo chpasswd"
    remote.exec "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test"
    # Check we can connect with the new test user and make sudo
    remote.exec --user test --pass ubuntu "sudo true"

    remote.exec "sudo adduser --extrausers --quiet --disabled-password --gecos '' external"
    remote.exec "echo external:ubuntu | sudo chpasswd"
    remote.exec "echo 'external ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-external"
    # Check we can connect with the new external user and make sudo
    remote.exec --user external --pass ubuntu "sudo true"
}


nested_is_kvm_enabled() {
    if [ -n "$NESTED_ENABLE_KVM" ]; then
        [ "$NESTED_ENABLE_KVM" = true ]
    fi
    return 0

}

nested_is_tpm_enabled() {
    if [ -n "$NESTED_ENABLE_TPM" ]; then
        [ "$NESTED_ENABLE_TPM" = true ]
    else
        case "${SPREAD_SYSTEM:-}" in
            ubuntu-1*)
                return 1
                ;;
            ubuntu-2*)
                # TPM enabled by default on 20.04 and later
                return 0
                ;;
            *)
                echo "unsupported system"
                exit 1
                ;;
        esac
    fi
}

nested_is_secure_boot_enabled() {
    if [ -n "$NESTED_ENABLE_SECURE_BOOT" ]; then
        [ "$NESTED_ENABLE_SECURE_BOOT" = true ]
    else
        case "${SPREAD_SYSTEM:-}" in
            ubuntu-1*)
                return 1
                ;;
            ubuntu-2*)
                # secure boot enabled by default on 20.04 and later
                return 0
                ;;
            *)
                echo "unsupported system"
                exit 1
                ;;
        esac
    fi
}

nested_create_assertions_disk() {
    mkdir -p "$NESTED_ASSETS_DIR"
    local ASSERTIONS_DISK LOOP_DEV
    ASSERTIONS_DISK="$NESTED_ASSETS_DIR/assertions.disk"

    # make an image
    dd if=/dev/null of="$ASSERTIONS_DISK" bs=1M seek=1
    # format it as dos with a vfat partition
    # TODO: can we do this more programmatically without printing into fdisk ?
    printf 'o\nn\np\n1\n\n\nt\nc\nw\n' | fdisk "$ASSERTIONS_DISK"
    # mount the disk image
    kpartx -av "$ASSERTIONS_DISK"
    # find the loopback device for the partition
    LOOP_DEV=$(losetup --list | grep "$ASSERTIONS_DISK" | awk '{print $1}' | grep -Po "/dev/loop\K([0-9]*)")
    # wait for the loop device to show up
    retry -n 3 --wait 1 test -e "/dev/mapper/loop${LOOP_DEV}p1"
    # make a vfat partition
    mkfs.vfat -n SYSUSER "/dev/mapper/loop${LOOP_DEV}p1"
    # mount the partition and copy the files 
    mkdir -p "$NESTED_ASSETS_DIR/sys-user-partition"
    mount "/dev/mapper/loop${LOOP_DEV}p1" "$NESTED_ASSETS_DIR/sys-user-partition"
    
    # use custom assertion if set
    local AUTO_IMPORT_ASSERT
    if [ -n "$NESTED_CUSTOM_AUTO_IMPORT_ASSERTION" ]; then
        VERSION="$(nested_get_version)"
        # shellcheck disable=SC2001
        AUTO_IMPORT_ASSERT="$(echo "$NESTED_CUSTOM_AUTO_IMPORT_ASSERTION" | sed "s/{VERSION}/$VERSION/g")"
    else
        local per_model_auto
        per_model_auto="$(nested_model_authority).auto-import.assert"
        if [ -e "$TESTSLIB/assertions/${per_model_auto}" ]; then
            AUTO_IMPORT_ASSERT="$TESTSLIB/assertions/${per_model_auto}"
        else
            AUTO_IMPORT_ASSERT="$TESTSLIB/assertions/auto-import.assert"
        fi
    fi
    cp "$AUTO_IMPORT_ASSERT" "$NESTED_ASSETS_DIR/sys-user-partition/auto-import.assert"

    # unmount the partition and the image disk
    sudo umount "$NESTED_ASSETS_DIR/sys-user-partition"
    sudo kpartx -d "$ASSERTIONS_DISK"
}

nested_qemu_name() {
    if os.query is-arm; then
        command -v qemu-system-aarch64
    elif [ "$NESTED_ARCHITECTURE" = "i386" ]; then
        command -v qemu-system-i386
    else
        command -v qemu-system-x86_64
    fi

}

nested_get_snap_rev_for_channel() {
    local SNAP=$1
    local CHANNEL=$2

    curl -s \
         -H "Snap-Device-Architecture: $NESTED_ARCHITECTURE" \
         -H "Snap-Device-Series: 16" \
         -X POST \
         -H "Content-Type: application/json" \
         --data "{\"context\": [], \"actions\": [{\"action\": \"install\", \"name\": \"$SNAP\", \"channel\": \"$CHANNEL\", \"instance-key\": \"1\"}]}" \
         https://api.snapcraft.io/v2/snaps/refresh | \
        jq '.results[0].snap.revision'
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

nested_is_core_22_system() {
    os.query is-jammy
}

nested_is_core_20_system() {
    os.query is-focal
}

nested_is_core_18_system() {
    os.query is-bionic
}

nested_is_core_16_system() {
    os.query is-xenial
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
            remote.exec "sudo snap refresh core --${NEW_CHANNEL}"
            remote.exec "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi

        if nested_is_core_18_system || nested_is_core_20_system || nested_is_core_22_system; then
            remote.exec "sudo snap refresh snapd --${NEW_CHANNEL}"
            remote.exec "snap info snapd" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        else
            CHANGE_ID=$(remote.exec "sudo snap refresh core --${NEW_CHANNEL} --no-wait")
            nested_wait_for_no_ssh 200 1
            nested_wait_for_ssh 300 1
            # wait for the refresh to be done before checking, if we check too
            # quickly then operations on the core snap like reverting, etc. may
            # fail because it will have refresh-snap change in progress
            remote.exec "snap watch $CHANGE_ID"
            remote.exec "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi
    fi
}

nested_get_snakeoil_key() {
    local KEYNAME="PkKek-1-snakeoil"
    wget -q https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.key
    wget -q https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.pem
    echo "$KEYNAME"
}

nested_secboot_remove_signature() {
    local FILE="$1"
    while sbverify --list "$FILE" | grep "^signature [0-9]*$"; do
        sbattach --remove "$FILE"
    done
}

nested_secboot_sign_file() {
    local FILE="$1"
    local KEY="$2"
    local CERT="$3"
    nested_secboot_remove_signature "$FILE"
    sbsign --key "$KEY" --cert "$CERT" --output "$FILE" "$FILE"
}

nested_secboot_sign_gadget() {
    local GADGET_DIR="$1"
    local KEY="$2"
    local CERT="$3"
    nested_secboot_sign_file "$GADGET_DIR/shim.efi.signed" "$KEY" "$CERT"
}

nested_prepare_env() {
    mkdir -p "$NESTED_IMAGES_DIR"
    mkdir -p "$NESTED_RUNTIME_DIR"
    mkdir -p "$NESTED_ASSETS_DIR"
    mkdir -p "$NESTED_LOGS_DIR"
    mkdir -p "$(nested_get_extra_snaps_path)"
}

nested_cleanup_env() {
    rm -rf "$NESTED_RUNTIME_DIR"
    rm -rf "$NESTED_ASSETS_DIR"
    rm -rf "$NESTED_LOGS_DIR"
    rm -rf "$NESTED_IMAGES_DIR"/*.img
    rm -rf "$(nested_get_extra_snaps_path)"
}

nested_get_image_name() {
    local TYPE="$1"
    local SOURCE="${NESTED_CORE_CHANNEL}"
    local NAME="${NESTED_IMAGE_ID:-generic}"
    local VERSION

    VERSION="$(nested_get_version)"
    # Use task name to build the image in case the NESTED_IMAGE_ID is unset
    # This scenario is valid on manual tests when it is required to set the NESTED_IMAGE_ID
    if [ "$NAME" = "unset" ]; then
        NAME="$(basename "$SPREAD_TASK")"
        if [ -n "$SPREAD_VARIANT" ]; then
            NAME="${NAME}_${SPREAD_VARIANT}"
        fi
    fi

    if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
        SOURCE="custom"
    fi
    if [ "$(nested_get_extra_snaps | wc -l)" != "0" ]; then
        SOURCE="custom"
    fi
    echo "ubuntu-${TYPE}-${VERSION}-${SOURCE}-${NAME}.img"
}

nested_is_generic_image() {
    test -z "${NESTED_IMAGE_ID:-}"
}

nested_get_extra_snaps_path() {
    echo "/tmp/extra-snaps"
}

nested_get_assets_path() {
    echo "$NESTED_ASSETS_DIR"
}

nested_get_images_path() {
    echo "$NESTED_IMAGES_DIR"
}

nested_get_extra_snaps() {
    local EXTRA_SNAPS=""
    local EXTRA_SNAPS_PATH
    EXTRA_SNAPS_PATH="$(nested_get_extra_snaps_path)"

    if [ -d "$EXTRA_SNAPS_PATH" ]; then
        while IFS= read -r mysnap; do
            echo "$mysnap"
        done < <(find "$EXTRA_SNAPS_PATH" -name '*.snap')
    fi
}

nested_download_image() {
    local IMAGE_URL=$1
    local IMAGE_NAME=$2

    curl -C - -L -o "${NESTED_IMAGES_DIR}/${IMAGE_NAME}" "$IMAGE_URL"

    if [[ "$IMAGE_URL" == *.img.xz ]]; then
        mv "${NESTED_IMAGES_DIR}/${IMAGE_NAME}" "${NESTED_IMAGES_DIR}/${IMAGE_NAME}.xz"
        unxz "${NESTED_IMAGES_DIR}/${IMAGE_NAME}.xz"
    elif [[ "$IMAGE_URL" == *.img ]]; then
        echo "Image doesn't need to be decompressed"
    else
        echo "Image extension not supported for image $IMAGE_URL, exiting..."
        exit 1
    fi
}

nested_get_version() {
    if nested_is_core_16_system; then
        echo "16"
    elif nested_is_core_18_system; then
        echo "18"
    elif nested_is_core_20_system; then
        echo "20"
    elif nested_is_core_22_system; then
        echo "22"
    fi
}

nested_get_model() {
    # use custom model if defined
    if [ -n "$NESTED_CUSTOM_MODEL" ]; then
        VERSION="$(nested_get_version)"
        # shellcheck disable=SC2001
        echo "$NESTED_CUSTOM_MODEL" | sed "s/{VERSION}/$VERSION/g"
        return
    fi
    case "$SPREAD_SYSTEM" in
        ubuntu-16.04-64)
            echo "$TESTSLIB/assertions/nested-amd64.model"
            ;;
        ubuntu-18.04-64)
            echo "$TESTSLIB/assertions/nested-18-amd64.model"
            ;;
        ubuntu-20.04-64)
            echo "$TESTSLIB/assertions/nested-20-amd64.model"
            ;;
        ubuntu-22.04-64)
            echo "$TESTSLIB/assertions/nested-22-amd64.model"
            ;;
        ubuntu-22.04-arm-64)
            echo "$TESTSLIB/assertions/nested-22-arm64.model"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
    esac
}

nested_model_authority() {
    local model
    model="$(nested_get_model)"
    grep "authority-id:" "$model"|cut -d ' ' -f2
}

nested_ensure_ubuntu_save() {
    local GADGET_DIR="$1"
    shift
    "$TESTSLIB"/ensure_ubuntu_save.py "$@" "$GADGET_DIR"/meta/gadget.yaml > /tmp/gadget-with-save.yaml
    if [ "$(cat /tmp/gadget-with-save.yaml)" != "" ]; then
        mv /tmp/gadget-with-save.yaml "$GADGET_DIR"/meta/gadget.yaml
    else
        rm -f /tmp/gadget-with-save.yaml
    fi
}

nested_prepare_snapd() {
    if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
        echo "Repacking snapd snap"
        local snap_name output_name snap_id
        if nested_is_core_16_system; then
            snap_name="core"
            output_name="core-from-snapd-deb.snap"
            snap_id="99T7MUlRhtI3U0QFgl5mXXESAiSwt776"
        else
            snap_name="snapd"
            output_name="snapd-from-deb.snap"
            snap_id="PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4"
        fi

        if [ ! -f "$NESTED_ASSETS_DIR/$output_name" ]; then
            "$TESTSTOOLS"/snaps-state repack_snapd_deb_into_snap "$snap_name" "$NESTED_ASSETS_DIR"
        fi
        cp "$NESTED_ASSETS_DIR/$output_name" "$(nested_get_extra_snaps_path)/$output_name"

        # sign the snapd snap with fakestore if requested
        if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
            "$TESTSTOOLS"/store-state make-snap-installable --noack "$NESTED_FAKESTORE_BLOB_DIR" "$(nested_get_extra_snaps_path)/$output_name" "$snap_id"
        fi
    fi
}

nested_prepare_kernel() {
    # allow repacking the kernel
    if [ "$NESTED_REPACK_KERNEL_SNAP" = "true" ]; then
        echo "Repacking kernel snap"
        local kernel_snap output_name snap_id version
        output_name="pc-kernel.snap"
        snap_id="pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza"
        version="$(nested_get_version)"

        if [ ! -f "$NESTED_ASSETS_DIR/$output_name" ]; then
            if nested_is_core_16_system || nested_is_core_18_system; then
                kernel_snap=pc-kernel-new.snap
                repack_kernel_snap "$kernel_snap"

            elif nested_is_core_20_system || nested_is_core_22_system; then
                snap download --basename=pc-kernel --channel="$version/${NESTED_KERNEL_CHANNEL}" pc-kernel

                # set the unix bump time if the NESTED_* var is set,
                # otherwise leave it empty
                local epochBumpTime
                epochBumpTime=${NESTED_CORE20_INITRAMFS_EPOCH_TIMESTAMP:-}
                if [ -n "$epochBumpTime" ]; then
                    epochBumpTime="--epoch-bump-time=$epochBumpTime"
                fi

                uc20_build_initramfs_kernel_snap "pc-kernel.snap" "$NESTED_ASSETS_DIR" "$epochBumpTime"
                rm -f "pc-kernel.snap" "pc-kernel.assert"

                # Prepare the pc kernel snap
                kernel_snap=$(ls "$NESTED_ASSETS_DIR"/pc-kernel_*.snap)
                chmod 0600 "$kernel_snap"
            fi
            mv "$kernel_snap" "$NESTED_ASSETS_DIR/$output_name"
        fi
        cp "$NESTED_ASSETS_DIR/$output_name" "$(nested_get_extra_snaps_path)/$output_name"

        # sign the pc-kernel snap with fakestore if requested
        if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
            "$TESTSTOOLS"/store-state make-snap-installable --noack "$NESTED_FAKESTORE_BLOB_DIR" "$(nested_get_extra_snaps_path)/$output_name" "$snap_id"
        fi
    fi
}

nested_prepare_gadget() {
    if [ "$NESTED_REPACK_GADGET_SNAP" = "true" ]; then
        if nested_is_core_20_system || nested_is_core_22_system; then
            # Prepare the pc gadget snap (unless provided by extra-snaps)
            local snap_id version gadget_snap
            version="$(nested_get_version)"
            snap_id="UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH"

            existing_snap=$(find "$(nested_get_extra_snaps_path)" -name 'pc_*.snap')
            if [ -n "$existing_snap" ]; then
                echo "Using generated pc gadget snap $existing_snap"
                if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                    "$TESTSTOOLS"/store-state make-snap-installable --noack --extra-decl-json "$NESTED_FAKESTORE_SNAP_DECL_PC_GADGET" "$NESTED_FAKESTORE_BLOB_DIR" "$existing_snap" "$snap_id"
                fi
                return
            fi

            # XXX: deal with [ "$NESTED_ENABLE_SECURE_BOOT" != "true" ] && [ "$NESTED_ENABLE_TPM" != "true" ]
            echo "Repacking pc snap"
            # Get the snakeoil key and cert
            local key_name snakeoil_key snakeoil_cert
            key_name=$(nested_get_snakeoil_key)
            snakeoil_key="$PWD/$key_name.key"
            snakeoil_cert="$PWD/$key_name.pem"

            snap download --basename=pc --channel="$version/${NESTED_GADGET_CHANNEL}" pc
            unsquashfs -d pc-gadget pc.snap
            nested_secboot_sign_gadget pc-gadget "$snakeoil_key" "$snakeoil_cert"
            case "${NESTED_UBUNTU_SAVE:-}" in
                add)
                    # ensure that ubuntu-save is present
                    nested_ensure_ubuntu_save pc-gadget --add
                    touch ubuntu-save-added
                    ;;
                remove)
                    # ensure that ubuntu-save is removed
                    nested_ensure_ubuntu_save pc-gadget --remove
                    touch ubuntu-save-removed
                    ;;
            esac

            # also make logging persistent for easier debugging of
            # test failures, otherwise we have no way to see what
            # happened during a failed nested VM boot where we
            # weren't able to login to a device
            cat >> pc-gadget/meta/gadget.yaml << EOF
defaults:
  system:
    journal:
      persistent: true
EOF
            local GADGET_EXTRA_CMDLINE=""
            if [ "$NESTED_SNAPD_DEBUG_TO_SERIAL" = "true" ]; then
                # add snapd debug and log to serial console for extra
                # visibility what happens when a machine fails to boot
                GADGET_EXTRA_CMDLINE="console=ttyS0 snapd.debug=1 systemd.journald.forward_to_console=1"
            fi
            if [ -n "$NESTED_EXTRA_CMDLINE" ]; then
                GADGET_EXTRA_CMDLINE="$GADGET_EXTRA_CMDLINE $NESTED_EXTRA_CMDLINE"
            fi

            if [ -n "$GADGET_EXTRA_CMDLINE" ]; then
                echo "Configuring command line parameters in the gadget snap: \"console=ttyS0 $GADGET_EXTRA_CMDLINE\""
                echo "$GADGET_EXTRA_CMDLINE" > pc-gadget/cmdline.extra
            fi

            # pack the gadget
            snap pack pc-gadget/ "$NESTED_ASSETS_DIR"

            gadget_snap=$(ls "$NESTED_ASSETS_DIR"/pc_*.snap)
            cp "$gadget_snap" "$(nested_get_extra_snaps_path)/pc.snap"
            rm -f "pc.snap" "pc.assert" "$snakeoil_key" "$snakeoil_cert"
        fi
        # sign the pc gadget snap with fakestore if requested
        if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
            # XXX: this is a bit of a hack, but some nested tests 
            # need extra bits in their snap declaration, so inject
            # that here, it could end up being empty in which case
            # it is ignored
            "$TESTSTOOLS"/store-state make-snap-installable --noack --extra-decl-json "$NESTED_FAKESTORE_SNAP_DECL_PC_GADGET" "$NESTED_FAKESTORE_BLOB_DIR" "$(nested_get_extra_snaps_path)/pc.snap" "$snap_id"
        fi
    fi
}

nested_prepare_base() {
    if [ "$NESTED_REPACK_BASE_SNAP" = "true" ]; then
        if nested_is_core_16_system; then
            echo "No base snap to prepare in core 16"
            return
        elif nested_is_core_18_system; then
            snap_name="core18"
            snap_id="CSO04Jhav2yK0uz97cr0ipQRyqg0qQL6"
        elif nested_is_core_20_system; then
            snap_name="core20"
            snap_id="DLqre5XGLbDqg9jPtiAhRRjDuPVa5X1q"
        elif nested_is_core_22_system; then
            snap_name="core22"
            snap_id="amcUKQILKXHHTlmSa7NMdnXSx02dNeeT"
        fi
        output_name="${snap_name}.snap"

        existing_snap=$(find "$(nested_get_extra_snaps_path)" -name "${snap_name}*.snap")
        if [ -n "$existing_snap" ]; then
            echo "Using generated base snap $existing_snap"
            if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                "$TESTSTOOLS"/store-state make-snap-installable --noack "$NESTED_FAKESTORE_BLOB_DIR" "$existing_snap" "$snap_id"
            fi
            return
        fi
        
        if [ ! -f "$NESTED_ASSETS_DIR/$output_name" ]; then
            echo "Repacking $snap_name snap"
            snap download --channel="$CORE_CHANNEL" --basename="$snap_name" "$snap_name"
            repack_core_snap_with_tweaks "${snap_name}.snap" "new-${snap_name}.snap"
            rm -f "$snap_name".snap "$snap_name".assert

            mv "new-${snap_name}.snap" "$NESTED_ASSETS_DIR/$output_name"
        fi
        cp "$NESTED_ASSETS_DIR/$output_name" "$(nested_get_extra_snaps_path)/$output_name"

        # sign the base snap with fakestore if requested
        if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
            "$TESTSTOOLS"/store-state make-snap-installable --noack "$NESTED_FAKESTORE_BLOB_DIR" "$(nested_get_extra_snaps_path)/${snap_name}.snap" "$snap_id"
        fi
    fi 
}

nested_prepare_essential_snaps() {
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    # shellcheck source=tests/lib/snaps.sh
    . "$TESTSLIB"/snaps.sh

    nested_prepare_snapd
    nested_prepare_kernel
    nested_prepare_gadget
    nested_prepare_base
}

nested_configure_default_user() {
    local IMAGE_NAME
    IMAGE_NAME="$(nested_get_image_name core)"
    # Configure the user for the vm
    if [ "$NESTED_USE_CLOUD_INIT" = "true" ]; then
        if nested_is_core_20_system || nested_is_core_22_system; then
            nested_configure_cloud_init_on_core20_vm "$NESTED_IMAGES_DIR/$IMAGE_NAME"
        else
            nested_configure_cloud_init_on_core_vm "$NESTED_IMAGES_DIR/$IMAGE_NAME"
        fi
    else
        nested_create_assertions_disk
    fi

    # Save a copy of the image
    cp -v "$NESTED_IMAGES_DIR/$IMAGE_NAME" "$NESTED_IMAGES_DIR/$IMAGE_NAME.pristine"
}

nested_create_core_vm() {
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    # shellcheck source=tests/lib/snaps.sh
    . "$TESTSLIB"/snaps.sh

    local IMAGE_NAME
    IMAGE_NAME="$(nested_get_image_name core)"
    mkdir -p "$NESTED_IMAGES_DIR"

    if [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME.pristine" ]; then
        cp -v "$NESTED_IMAGES_DIR/$IMAGE_NAME.pristine" "$NESTED_IMAGES_DIR/$IMAGE_NAME"
        if [ ! "$NESTED_USE_CLOUD_INIT" = "true" ]; then
            nested_create_assertions_disk
        fi
        return

    elif [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
        if [ -n "$NESTED_CUSTOM_IMAGE_URL" ]; then
            # download the ubuntu-core image from $CUSTOM_IMAGE_URL
            nested_download_image "$NESTED_CUSTOM_IMAGE_URL" "$IMAGE_NAME"
        else
            # create the ubuntu-core image
            local UBUNTU_IMAGE="$GOHOME"/bin/ubuntu-image
            if os.query is-xenial || os.query is-arm; then
                # ubuntu-image on 16.04 needs to be installed from a snap
                UBUNTU_IMAGE=/snap/bin/ubuntu-image
            fi

            if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
                nested_prepare_snapd
                nested_prepare_kernel
                nested_prepare_gadget
                nested_prepare_base
            fi

            # Invoke ubuntu image
            local NESTED_MODEL
            NESTED_MODEL="$(nested_get_model)"
            
            local EXTRA_SNAPS=""
            for mysnap in $(nested_get_extra_snaps); do
                EXTRA_SNAPS="$EXTRA_SNAPS --snap $mysnap"
            done

            # only set SNAPPY_FORCE_SAS_URL because we don't need it defined 
            # anywhere else but here, where snap prepare-image as called by 
            # ubuntu-image will look for assertions for the snaps we provide
            # to it
            SNAPPY_FORCE_SAS_URL="$NESTED_UBUNTU_IMAGE_SNAPPY_FORCE_SAS_URL"
            export SNAPPY_FORCE_SAS_URL
            UBUNTU_IMAGE_SNAP_CMD=/usr/bin/snap
            export UBUNTU_IMAGE_SNAP_CMD
            if [ -n "$NESTED_CORE_CHANNEL" ]; then
                UBUNTU_IMAGE_CHANNEL_ARG="--channel $NESTED_CORE_CHANNEL"
            else 
                UBUNTU_IMAGE_CHANNEL_ARG=""
            fi

            declare -a UBUNTU_IMAGE_PRESEED_ARGS
            if [ -n "$NESTED_UBUNTU_IMAGE_PRESEED_KEY" ]; then
                # shellcheck disable=SC2191
                UBUNTU_IMAGE_PRESEED_ARGS+=(--preseed  --preseed-sign-key=\""$NESTED_UBUNTU_IMAGE_PRESEED_KEY"\")
            fi
            # ubuntu-image creates sparse image files
            # shellcheck disable=SC2086
            "$UBUNTU_IMAGE" snap --image-size 10G --validation=enforce \
               "$NESTED_MODEL" \
                $UBUNTU_IMAGE_CHANNEL_ARG \
                "${UBUNTU_IMAGE_PRESEED_ARGS[@]:-}" \
                --output-dir "$NESTED_IMAGES_DIR" \
                --sector-size "${NESTED_DISK_LOGICAL_BLOCK_SIZE}" \
                $EXTRA_SNAPS

            # ubuntu-image dropped the --output parameter, so we have to rename
            # the image ourselves, the images are named after volumes listed in
            # gadget.yaml
            find "$NESTED_IMAGES_DIR/" -maxdepth 1 -name '*.img' | while read -r imgname; do
                if [ -e "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
                    echo "Image $IMAGE_NAME file already present"
                    exit 1
                fi
                mv "$imgname" "$NESTED_IMAGES_DIR/$IMAGE_NAME"
            done
            unset SNAPPY_FORCE_SAS_URL
            unset UBUNTU_IMAGE_SNAP_CMD
        fi
    fi

    nested_configure_default_user
}

nested_configure_cloud_init_on_core_vm() {
    local IMAGE=$1
    nested_create_cloud_init_data "$NESTED_ASSETS_DIR/user-data" "$NESTED_ASSETS_DIR/meta-data"

    local devloop writableDev tmp
    # mount the image and find the loop device /dev/loop that is created for it
    kpartx -avs "$IMAGE"
    devloop=$(losetup --list --noheadings | grep "$IMAGE" | awk '{print $1}')
    dev=$(basename "$devloop")
    
    # we add cloud-init data to the 3rd partition, which is writable
    writableDev="/dev/mapper/${dev}p3"
    
    # wait for the loop device to show up
    retry -n 3 --wait 1 test -e "$writableDev"
    tmp=$(mktemp -d)
    mount "$writableDev" "$tmp"

    # use nocloud-net for the dir to copy data into
    mkdir -p "$tmp/system-data/var/lib/cloud/seed/nocloud-net/"
    cp "$NESTED_ASSETS_DIR/user-data" "$tmp/system-data/var/lib/cloud/seed/nocloud-net/"
    cp "$NESTED_ASSETS_DIR/meta-data" "$tmp/system-data/var/lib/cloud/seed/nocloud-net/"

    sync
    umount "$tmp"
    kpartx -d "$IMAGE"
}

nested_create_cloud_init_data() {
    local USER_DATA=$1
    local META_DATA=$2
    cat <<EOF > "$USER_DATA"
#cloud-config
  ssh_pwauth: True
  users:
   - name: user1
     sudo: ALL=(ALL) NOPASSWD:ALL
     shell: /bin/bash
  chpasswd:
   list: |
    user1:ubuntu
   expire: False
EOF

    cat <<EOF > "$META_DATA"
instance_id: cloud-images
EOF
}

# TODO: see if the uc20 config works for classic here too, that would be faster
#       as the chpasswd module from cloud-init runs rather late in the boot
nested_create_cloud_init_config() {
    local CONFIG_PATH=$1
    cat <<EOF > "$CONFIG_PATH"
#cloud-config
  ssh_pwauth: True
  users:
   - name: user1
     sudo: ALL=(ALL) NOPASSWD:ALL
     shell: /bin/bash
  chpasswd:
   list: |
    user1:ubuntu
   expire: False
  datasource_list: [ NoCloud ]
  datasource:
    NoCloud:
     userdata_raw: |
      #!/bin/bash
      logger -t nested test running || true
EOF
}

nested_create_cloud_init_uc20_config() {
    local CONFIG_PATH=$1
    cat << 'EOF' > "$CONFIG_PATH"
#cloud-config
datasource_list: [NoCloud]
users:
  - name: user1
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    lock_passwd: false
    plain_text_passwd: "ubuntu"
EOF
}

nested_add_file_to_image() {
    local IMAGE=$1
    local FILE=$2
    local devloop ubuntuSeedDev tmp
    # Bind the image the a free loop device
    devloop="$(retry -n 3 --wait 1 losetup -f --show -P --sector-size "${NESTED_DISK_LOGICAL_BLOCK_SIZE}" "${IMAGE}")"

    # we add cloud-init data to the 2nd partition, which is ubuntu-seed
    ubuntuSeedDev="${devloop}p2"
    if os.query is-arm; then
        # In arm the BIOS partition does not exist, so ubuntu-seed is the p1
        ubuntuSeedDev="${devloop}p1"
    fi

    # Wait for the partition to show up
    retry -n 2 --wait 1 test -b "${ubuntuSeedDev}" || true

    # losetup does not set the right block size on LOOP_CONFIGURE
    # but with LOOP_SET_BLOCK_SIZE later. So the block size might have
    # been wrong during the part scan. In this case we need to rescan
    # manually.
    if ! [ -b "${ubuntuSeedDev}" ]; then
        partx -u "${devloop}"
        # Wait for the partition to show up
        retry -n 2 --wait 1 test -b "${ubuntuSeedDev}"
    fi

    tmp=$(mktemp -d)
    retry -n 5 --wait 2 mount "$ubuntuSeedDev" "$tmp"
    mkdir -p "$tmp/data/etc/cloud/cloud.cfg.d/"
    cp -f "$FILE" "$tmp/data/etc/cloud/cloud.cfg.d/"
    sync
    umount "$tmp"
    losetup -d "${devloop}"
}

nested_configure_cloud_init_on_core20_vm() {
    local IMAGE=$1
    nested_create_cloud_init_uc20_config "$NESTED_ASSETS_DIR/data.cfg"

    nested_add_file_to_image "$IMAGE" "$NESTED_ASSETS_DIR/data.cfg"
}

nested_save_serial_log() {
    if [ -f "${NESTED_LOGS_DIR}/serial.log" ]; then
        for i in $(seq 1 9); do
            if [ ! -f "${NESTED_LOGS_DIR}/serial.log.${i}" ]; then
                cp "${NESTED_LOGS_DIR}/serial.log" "${NESTED_LOGS_DIR}/serial.log.${i}"
                break
            fi
        done
        # make sure we start with clean log file
        echo > "${NESTED_LOGS_DIR}/serial.log"
    fi
}

nested_print_serial_log() {
    if [ -f "${NESTED_LOGS_DIR}/serial.log.1" ]; then
        # here we disable SC2045 because previously it is checked there is at least
        # 1 file which matches. In this case ls command is needed because it is important
        # to get the list in reverse order.
        # shellcheck disable=SC2045
        for logfile in $(ls "${NESTED_LOGS_DIR}"/serial.log.*); do
            cat "$logfile"
        done
    fi
    if [ -f "${NESTED_LOGS_DIR}/serial.log" ]; then
        cat "${NESTED_LOGS_DIR}/serial.log"
    fi
}

nested_force_stop_vm() {
    systemctl stop "$NESTED_VM"
}

nested_force_start_vm() {
    # if the $NESTED_VM is using a swtpm, we need to wait until the file exists
    # because the file disappears temporarily after qemu exits
    if systemctl show "$NESTED_VM" -p ExecStart | grep -q test-snapd-swtpm; then
        retry -n 10 --wait 1 test -S /var/snap/test-snapd-swtpm/current/swtpm-sock
    fi
    systemctl start "$NESTED_VM"
}

nested_start_core_vm_unit() {
    local QEMU CURRENT_IMAGE
    CURRENT_IMAGE=$1
    QEMU=$(nested_qemu_name)

    # Now qemu parameters are defined

    # use only 2G of RAM for qemu-nested
    # the caller can override PARAM_MEM
    local PARAM_MEM PARAM_SMP
    if [ "$SPREAD_BACKEND" = "google-nested" ] || [ "$SPREAD_BACKEND" = "google-nested-arm" ]; then
        PARAM_MEM="-m ${NESTED_MEM:-4096}"
        PARAM_SMP="-smp ${NESTED_CPUS:-2}"
    elif [ "$SPREAD_BACKEND" = "google-nested-dev" ]; then
        PARAM_MEM="-m ${NESTED_MEM:-8192}"
        PARAM_SMP="-smp ${NESTED_CPUS:-4}"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MEM="-m ${NESTED_MEM:-2048}"
        PARAM_SMP="-smp ${NESTED_CPUS:-1}"
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi

    PARAM_PHYS_BLOCK_SIZE="physical_block_size=${NESTED_DISK_PHYSICAL_BLOCK_SIZE}"
    PARAM_LOGI_BLOCK_SIZE="logical_block_size=${NESTED_DISK_LOGICAL_BLOCK_SIZE}"

    local PARAM_DISPLAY PARAM_NETWORK PARAM_MONITOR PARAM_USB PARAM_CD PARAM_RANDOM PARAM_CPU PARAM_TRACE PARAM_LOG PARAM_SERIAL PARAM_RTC
    PARAM_DISPLAY="-nographic"
    PARAM_NETWORK="-net nic,model=virtio -net user,hostfwd=tcp::$NESTED_SSH_PORT-:22,hostfwd=tcp::8023-:8023,hostfwd=tcp::9022-:9022"
    PARAM_MONITOR="-monitor tcp:127.0.0.1:$NESTED_MON_PORT,server=on,wait=off"
    PARAM_USB="-usb"
    PARAM_CD="${NESTED_PARAM_CD:-}"
    PARAM_RANDOM="-object rng-random,id=rng0,filename=/dev/urandom -device virtio-rng-pci,rng=rng0"
    PARAM_CPU=""
    PARAM_TRACE="-d cpu_reset"
    PARAM_LOG="-D $NESTED_LOGS_DIR/qemu.log"
    PARAM_RTC="${NESTED_PARAM_RTC:-}"
    PARAM_EXTRA="${NESTED_PARAM_EXTRA:-}"

    # Open port 7777 on the host so that failures in the nested VM (e.g. to
    # create users) can be debugged interactively via
    # "telnet localhost 7777". Also keeps the logs
    #
    # XXX: should serial just be logged to stdout so that we just need
    #      to "journalctl -u $NESTED_VM" to see what is going on ?
    if "$QEMU" -version | grep '2\.5'; then
        # XXX: remove once we no longer support xenial hosts
        PARAM_SERIAL="-serial file:${NESTED_LOGS_DIR}/serial.log"
    else
        PARAM_SERIAL="-chardev socket,telnet=on,host=localhost,server=on,port=7777,wait=off,id=char0,logfile=${NESTED_LOGS_DIR}/serial.log,logappend=on -serial chardev:char0"
    fi

    # save logs from previous runs
    nested_save_serial_log

    # Set kvm attribute
    local ATTR_KVM
    ATTR_KVM=""
    if nested_is_kvm_enabled; then
        ATTR_KVM=",accel=kvm"
        # CPU can be defined just when kvm is enabled
        PARAM_CPU="-cpu host"
    fi

    local PARAM_MACHINE
    if [[ "$SPREAD_BACKEND" = google-nested* ]]; then
        if os.query is-arm; then
            PARAM_MACHINE="-machine virt${ATTR_KVM}"
            PARAM_CPU="-cpu host"
        else
            PARAM_MACHINE="-machine ubuntu${ATTR_KVM}"
        fi
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        # check if we have nested kvm
        if [ "$(cat /sys/module/kvm_*/parameters/nested)" = "1" ]; then
            PARAM_MACHINE="-machine ubuntu${ATTR_KVM}"
        else
            # and if not reset kvm related parameters
            PARAM_MACHINE=""
            PARAM_CPU=""
            ATTR_KVM=""
        fi
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi
    
    local PARAM_ASSERTIONS PARAM_BIOS PARAM_TPM PARAM_IMAGE
    PARAM_ASSERTIONS=""
    PARAM_BIOS=""
    PARAM_TPM=""
    PARAM_REEXEC_ON_FAILURE=""
    if [ "$NESTED_USE_CLOUD_INIT" != "true" ]; then
        # TODO: fix using the old way of an ext4 formatted drive w/o partitions
        #       as this used to work but has since regressed
        
        # this simulates a usb drive attached to the device, the removable=true
        # is necessary otherwise snapd will not import it, as snapd only 
        # considers removable devices for cold-plug first-boot runs
        # the nec-usb-xhci device is necessary to create the bus we attach the
        # storage to
        PARAM_ASSERTIONS="-drive if=none,id=stick,format=raw,file=$NESTED_ASSETS_DIR/assertions.disk,cache=none,format=raw -device nec-usb-xhci,id=xhci -device usb-storage,bus=xhci.0,removable=true,drive=stick"
    fi
    if nested_is_core_20_system || nested_is_core_22_system; then
        # use a bundle EFI bios by default
        if os.query is-arm; then
            PARAM_BIOS="-bios /usr/share/AAVMF/AAVMF_CODE.fd"
        else
            PARAM_BIOS="-bios /usr/share/ovmf/OVMF.fd"
        fi
        local OVMF_CODE OVMF_VARS
        OVMF_CODE="secboot"
        OVMF_VARS="ms"

        if nested_is_core_22_system; then
            wget -q https://storage.googleapis.com/snapd-spread-tests/dependencies/OVMF_CODE.secboot.fd
            mv OVMF_CODE.secboot.fd /usr/share/OVMF/OVMF_CODE.secboot.fd
            wget -q https://storage.googleapis.com/snapd-spread-tests/dependencies/OVMF_VARS.snakeoil.fd
            mv OVMF_VARS.snakeoil.fd /usr/share/OVMF/OVMF_VARS.snakeoil.fd
        fi
        # In this case the kernel.efi is unsigned and signed with snaleoil certs
        if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
            OVMF_VARS="snakeoil"
        fi

        if [ "${NESTED_ENABLE_OVMF:-}" = "true" ]; then
            if os.query is-arm; then
                PARAM_BIOS="-bios /usr/share/AAVMF/AAVMF_CODE.fd"
            else
                PARAM_BIOS="-bios /usr/share/OVMF/OVMF_CODE.fd"
            fi
        fi
        if nested_is_secure_boot_enabled; then
            if os.query is-arm; then
                cp -f "/usr/share/AAVMF/AAVMF_VARS.fd" "$NESTED_ASSETS_DIR/AAVMF_VARS.fd"
                PARAM_BIOS="-drive file=/usr/share/AAVMF/AAVMF_CODE.fd,if=pflash,format=raw,unit=0,readonly=on -drive file=$NESTED_ASSETS_DIR/AAVMF_VARS.fd,if=pflash,format=raw"
            else
                cp -f "/usr/share/OVMF/OVMF_VARS.$OVMF_VARS.fd" "$NESTED_ASSETS_DIR/OVMF_VARS.$OVMF_VARS.fd"
                PARAM_BIOS="-drive file=/usr/share/OVMF/OVMF_CODE.$OVMF_CODE.fd,if=pflash,format=raw,unit=0,readonly=on -drive file=$NESTED_ASSETS_DIR/OVMF_VARS.$OVMF_VARS.fd,if=pflash,format=raw"
                PARAM_MACHINE="-machine q35${ATTR_KVM} -global ICH9-LPC.disable_s3=1"
            fi
        fi

        if nested_is_tpm_enabled; then
            if snap list test-snapd-swtpm >/dev/null; then
                if [ -z "$NESTED_TPM_NO_RESTART" ]; then
                    # reset the tpm state
                    snap stop test-snapd-swtpm > /dev/null
                    rm /var/snap/test-snapd-swtpm/current/tpm2-00.permall || true
                    snap start test-snapd-swtpm > /dev/null
                fi
            else
                snap install test-snapd-swtpm --edge
            fi
            # wait for the tpm sock file to exist
            retry -n 10 --wait 1 test -S /var/snap/test-snapd-swtpm/current/swtpm-sock
            PARAM_TPM="-chardev socket,id=chrtpm,path=/var/snap/test-snapd-swtpm/current/swtpm-sock -tpmdev emulator,id=tpm0,chardev=chrtpm"
            if os.query is-arm; then
                PARAM_TPM="$PARAM_TPM -device tpm-tis-device,tpmdev=tpm0"
            else
                PARAM_TPM="$PARAM_TPM -device tpm-tis,tpmdev=tpm0"
            fi
        fi
        PARAM_IMAGE="-drive file=$CURRENT_IMAGE,cache=none,format=raw,id=disk1,if=none -device virtio-blk-pci,drive=disk1,bootindex=1"
    else
        PARAM_IMAGE="-drive file=$CURRENT_IMAGE,cache=none,format=raw,id=disk1,if=none -device ide-hd,drive=disk1"
    fi
    PARAM_IMAGE="$PARAM_IMAGE,${PARAM_PHYS_BLOCK_SIZE},${PARAM_LOGI_BLOCK_SIZE}"

    if nested_is_core_20_system; then
        # This is to deal with the following qemu error which occurs using q35 machines in focal
        # Error -> Code=qemu-system-x86_64: /build/qemu-rbeYHu/qemu-4.2/include/hw/core/cpu.h:633: cpu_asidx_from_attrs: Assertion `ret < cpu->num_ases && ret >= 0' failed
        # It is reproducible on an Intel machine without unrestricted mode support, the failure is most likely due to the guest entering an invalid state for Intel VT
        # The workaround is to restart the vm and check that qemu doesn't go into this bad state again
        PARAM_REEXEC_ON_FAILURE="[Service]\nRestart=on-failure\nRestartSec=5s"
    fi

    # ensure we have a log dir
    mkdir -p "$NESTED_LOGS_DIR"
    # make sure we start with clean log file
    echo > "${NESTED_LOGS_DIR}/serial.log"
    # Systemd unit is created, it is important to respect the qemu parameters order
    tests.systemd create-and-start-unit "$NESTED_VM" "${QEMU} \
        ${PARAM_SMP} \
        ${PARAM_CPU} \
        ${PARAM_MEM} \
        ${PARAM_TRACE} \
        ${PARAM_LOG} \
        ${PARAM_RTC} \
        ${PARAM_MACHINE} \
        ${PARAM_DISPLAY} \
        ${PARAM_NETWORK} \
        ${PARAM_BIOS} \
        ${PARAM_TPM} \
        ${PARAM_RANDOM} \
        ${PARAM_IMAGE} \
        ${PARAM_ASSERTIONS} \
        ${PARAM_SERIAL} \
        ${PARAM_MONITOR} \
        ${PARAM_USB} \
        ${PARAM_CD}  \
        ${PARAM_EXTRA} " "${PARAM_REEXEC_ON_FAILURE}"

    local EXPECT_SHUTDOWN
    EXPECT_SHUTDOWN=${NESTED_EXPECT_SHUTDOWN:-}

    if [ "$EXPECT_SHUTDOWN" != "1" ]; then
        # Wait until the vm is ready to receive connections
        if ! nested_wait_vm_ready 120 40; then
            echo "failed to wait for the vm becomes ready to receive connections"
            return 1
        fi
        # Wait for the snap command to be available
        nested_wait_for_snap_command 120 1
        # Wait for snap seeding to be done
        # retry this wait command up to 3 times since we sometimes see races 
        # where the snap command appears, then immediately disappears and then 
        # re-appears immediately after and so the next command fails
        attempts=0
        until remote.exec "sudo snap wait system seed.loaded"; do
            attempts=$(( attempts + 1))
            if [ "$attempts" = 3 ]; then
                echo "failed to wait for snap wait command to return successfully"
                return 1
            fi
            sleep 1
        done
        # Copy tools to be used on tests
        nested_prepare_tools
        # Wait for cloud init to be done if the system is using cloud-init
        if [ "$NESTED_USE_CLOUD_INIT" = true ]; then
            remote.exec "retry --wait 1 -n 5 sh -c 'cloud-init status --wait'"
        fi
    fi
}

nested_get_current_image_name() {
    echo "ubuntu-core-current.img"
}

nested_start_core_vm() {
    local CURRENT_IMAGE CURRENT_NAME
    CURRENT_NAME="$(nested_get_current_image_name)"
    CURRENT_IMAGE="$NESTED_IMAGES_DIR/$CURRENT_NAME"

    # In case the current image already exists, it needs to be reused and in that
    # case is neither required to copy the base image nor prepare the ssh
    if [ ! -f "$CURRENT_IMAGE" ]; then
        # As core18 systems use to fail to start the assertion disk when using the
        # snapshot feature, we copy the original image and use that copy to start
        # the VM.
        # Some tests however need to force stop and restart the VM with different
        # options, so if that env var is set, we will reuse the existing file if it
        # exists
        local IMAGE_NAME
        IMAGE_NAME="$(nested_get_image_name core)"
        if ! [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
            echo "No image found to be started"
            exit 1
        fi

        # images are created as sparse files, simple cp should preserve that
        # property
        cp -v "$NESTED_IMAGES_DIR/$IMAGE_NAME" "$CURRENT_IMAGE"

        # Start the nested core vm
        nested_start_core_vm_unit "$CURRENT_IMAGE"

        if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME.configured" ]; then
            # configure ssh for first time
            nested_prepare_ssh
            sync

            # keep a copy of the current image if it is a generic image
            if nested_is_generic_image && [ "$NESTED_CONFIGURE_IMAGES" = "true" ]; then
                # Stop the current image and compress it
                nested_shutdown

                # Save the image with the name of the original image
                cp -v "${CURRENT_IMAGE}" "$NESTED_IMAGES_DIR/$IMAGE_NAME"
                touch "$NESTED_IMAGES_DIR/$IMAGE_NAME.configured"

                # Start the current image again and wait until it is ready
                nested_start
            fi
        fi
    else
        # Start the nested core vm
        nested_start_core_vm_unit "$CURRENT_IMAGE"
    fi
}

nested_shutdown() {
    # we sometimes have bugs in nested vm's where files that were successfully
    # written become empty all of a sudden, so doing a sync here in the VM, and
    # another one in the host when done probably helps to avoid that, and at
    # least can't hurt anything
    remote.exec "sync"
    remote.exec "sudo shutdown now" || true
    nested_wait_for_no_ssh 120 1
    nested_force_stop_vm
    tests.systemd wait-for-service -n 30 --wait 1 --state inactive "$NESTED_VM"
    sync
}

nested_start() {
    nested_save_serial_log
    nested_force_start_vm
    tests.systemd wait-for-service -n 30 --wait 1 --state active "$NESTED_VM"
    nested_wait_for_ssh 300 1
    nested_prepare_tools
}

nested_force_restart_vm() {
    nested_force_stop_vm
    nested_force_start_vm
    tests.systemd wait-for-service -n 30 --wait 1 --state active "$NESTED_VM"
}

nested_create_classic_vm() {
    local IMAGE_NAME
    IMAGE_NAME="$(nested_get_image_name classic)"

    mkdir -p "$NESTED_IMAGES_DIR"
    if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
        # shellcheck source=tests/lib/image.sh
        . "$TESTSLIB"/image.sh

        # Get the cloud image
        local IMAGE_URL
        IMAGE_URL="$(get_image_url_for_vm)"
        wget -q -P "$NESTED_IMAGES_DIR" "$IMAGE_URL"
        nested_download_image "$IMAGE_URL" "$IMAGE_NAME"

        # Prepare the cloud-init configuration and configure image
        nested_create_cloud_init_config "$NESTED_ASSETS_DIR/seed"
        cloud-localds -H "$(hostname)" "$NESTED_ASSETS_DIR/seed.img" "$NESTED_ASSETS_DIR/seed"
    fi

    # Save a copy of the image
    cp -v "$NESTED_IMAGES_DIR/$IMAGE_NAME" "$NESTED_IMAGES_DIR/$IMAGE_NAME.pristine"
}

nested_start_classic_vm() {
    local IMAGE QEMU IMAGE_NAME
    QEMU="$(nested_qemu_name)"
    IMAGE_NAME="$(nested_get_image_name classic)"

    if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ] ; then
        cp -v "$NESTED_IMAGES_DIR/$IMAGE_NAME.pristine" "$IMAGE_NAME"
    fi
    # Give extra disk space for the image
    qemu-img resize "$NESTED_IMAGES_DIR/$IMAGE_NAME" +2G

    # Now qemu parameters are defined
    local PARAM_SMP PARAM_MEM
    PARAM_SMP="-smp 1"
    # use only 2G of RAM for qemu-nested
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MEM="-m ${NESTED_MEM:-4096}"
        PARAM_SMP="-smp ${NESTED_CPUS:-2}"
    elif [ "$SPREAD_BACKEND" = "google-nested-dev" ]; then
        PARAM_MEM="-m ${NESTED_MEM:-8192}"
        PARAM_SMP="-smp ${NESTED_CPUS:-4}"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MEM="-m ${NESTED_MEM:-2048}"
        PARAM_SMP="-smp ${NESTED_CPUS:-1}"
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi
    local PARAM_DISPLAY PARAM_NETWORK PARAM_MONITOR PARAM_USB PARAM_CPU PARAM_CD PARAM_RANDOM PARAM_SNAPSHOT
    PARAM_DISPLAY="-nographic"
    PARAM_NETWORK="-net nic,model=virtio -net user,hostfwd=tcp::$NESTED_SSH_PORT-:22"
    PARAM_MONITOR="-monitor tcp:127.0.0.1:$NESTED_MON_PORT,server=on,wait=off"
    PARAM_USB="-usb"
    PARAM_CPU=""
    PARAM_CD="${NESTED_PARAM_CD:-}"
    PARAM_RANDOM="-object rng-random,id=rng0,filename=/dev/urandom -device virtio-rng-pci,rng=rng0"
    # TODO: can this be removed? we create a "pristine" copy above?
    #PARAM_SNAPSHOT="-snapshot"
    PARAM_SNAPSHOT=""
    PARAM_EXTRA="${NESTED_PARAM_EXTRA:-}"

    # XXX: duplicated from nested core vm
    # Set kvm attribute
    local ATTR_KVM
    ATTR_KVM=""
    if nested_is_kvm_enabled; then
        ATTR_KVM=",accel=kvm"
        # CPU can be defined just when kvm is enabled
        PARAM_CPU="-cpu host"
    fi

    local PARAM_MACHINE PARAM_IMAGE PARAM_SEED PARAM_SERIAL PARAM_BIOS PARAM_TPM
    if [[ "$SPREAD_BACKEND" = google-nested* ]]; then
        PARAM_MACHINE="-machine ubuntu,accel=kvm"
        PARAM_CPU="-cpu host"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        # check if we have nested kvm
        if [ "$(cat /sys/module/kvm_*/parameters/nested)" = "1" ]; then
            PARAM_MACHINE="-machine ubuntu${ATTR_KVM}"
        else
            # and if not reset kvm related parameters
            PARAM_MACHINE=""
            PARAM_CPU=""
            ATTR_KVM=""
        fi
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi

    PARAM_IMAGE="-drive file=$NESTED_IMAGES_DIR/$IMAGE_NAME,if=none,id=disk1 -device virtio-blk-pci,drive=disk1,bootindex=1"
    PARAM_SEED="-drive file=$NESTED_ASSETS_DIR/seed.img,if=virtio"
    # Open port 7777 on the host so that failures in the nested VM (e.g. to
    # create users) can be debugged interactively via
    # "telnet localhost 7777". Also keeps the logs
    #
    # XXX: should serial just be logged to stdout so that we just need
    #      to "journalctl -u $NESTED_VM" to see what is going on ?
    if "$QEMU" -version | grep '2\.5'; then
        # XXX: remove once we no longer support xenial hosts
        PARAM_SERIAL="-serial file:${NESTED_LOGS_DIR}/serial.log"
    else
        PARAM_SERIAL="-chardev socket,telnet=on,host=localhost,server=on,port=7777,wait=off,id=char0,logfile=${NESTED_LOGS_DIR}/serial.log,logappend=on -serial chardev:char0"
    fi
    PARAM_BIOS=""
    PARAM_TPM=""

    # ensure we have a log dir
    mkdir -p "$NESTED_LOGS_DIR"
    # save logs from previous runs
    nested_save_serial_log

    # Systemd unit is created, it is important to respect the qemu parameters 
    # order
    tests.systemd create-and-start-unit "$NESTED_VM" "${QEMU}  \
        ${PARAM_SMP} \
        ${PARAM_CPU} \
        ${PARAM_MEM} \
        ${PARAM_SNAPSHOT} \
        ${PARAM_MACHINE} \
        ${PARAM_DISPLAY} \
        ${PARAM_NETWORK} \
        ${PARAM_BIOS} \
        ${PARAM_TPM} \
        ${PARAM_RANDOM} \
        ${PARAM_IMAGE} \
        ${PARAM_SEED} \
        ${PARAM_SERIAL} \
        ${PARAM_MONITOR} \
        ${PARAM_USB} \
        ${PARAM_EXTRA} \
        ${PARAM_CD} "

    if ! nested_wait_vm_ready 60 60; then
        echo "failed to wait for the vm becomes ready to receive connections"
        return 1
    fi

    # Copy tools to be used on tests
    nested_prepare_tools
}

nested_destroy_vm() {
    tests.systemd stop-unit --remove "$NESTED_VM"

    local CURRENT_IMAGE
    CURRENT_IMAGE="$NESTED_IMAGES_DIR/$(nested_get_current_image_name)" 
    rm -f "$CURRENT_IMAGE"
}

nested_status_vm() {
    systemctl status "$NESTED_VM" || true
}

remote.exec_as() {
    local USER="$1"
    local PASSWD="$2"
    shift 2
    sshpass -p "$PASSWD" ssh -p "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$USER"@localhost "$@"
}

nested_prepare_tools() {
    TOOLS_PATH=/writable/test-tools
    if ! remote.exec "test -d $TOOLS_PATH" &>/dev/null; then
        remote.exec "sudo mkdir -p $TOOLS_PATH"
        remote.exec "sudo chown user1:user1 $TOOLS_PATH"
    fi

    if ! remote.exec "test -e $TOOLS_PATH/retry" &>/dev/null; then
        remote.push "$TESTSTOOLS/retry"
        remote.exec "mv retry $TOOLS_PATH/retry"
    fi

    if ! remote.exec "test -e $TOOLS_PATH/not" &>/dev/null; then
        remote.push "$TESTSTOOLS/not"
        remote.exec "mv not $TOOLS_PATH/not"
    fi

    if ! remote.exec "test -e $TOOLS_PATH/MATCH" &>/dev/null; then
        # shellcheck source=tests/lib/spread-funcs.sh
        . "$TESTSLIB"/spread-funcs.sh
        echo '#!/bin/bash' > MATCH_FILE
        type MATCH | tail -n +2 >> MATCH_FILE
        echo 'MATCH "$@"' >> MATCH_FILE
        chmod +x MATCH_FILE
        remote.push "MATCH_FILE"
        remote.exec "mv MATCH_FILE $TOOLS_PATH/MATCH"
        rm -f MATCH_FILE
    fi

    if ! remote.exec "test -e $TOOLS_PATH/NOMATCH" &>/dev/null; then
        # shellcheck source=tests/lib/spread-funcs.sh
        . "$TESTSLIB"/spread-funcs.sh
        echo '#!/bin/bash' > NOMATCH_FILE
        type NOMATCH | tail -n +2 >> NOMATCH_FILE
        echo 'NOMATCH "$@"' >> NOMATCH_FILE
        chmod +x NOMATCH_FILE
        remote.push "NOMATCH_FILE"
        remote.exec "mv NOMATCH_FILE $TOOLS_PATH/NOMATCH"
        rm -f NOMATCH_FILE
    fi

    if ! remote.exec "grep -qE PATH=.*$TOOLS_PATH /etc/environment"; then
        # shellcheck disable=SC2016
        REMOTE_PATH="$(remote.exec 'echo $PATH')"
        remote.exec "echo PATH=$TOOLS_PATH:$REMOTE_PATH | sudo tee -a /etc/environment"
    fi
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
    remote.exec "snap info core" | awk "/${CHANNEL}: / {print(\$4)}" | sed -e 's/(\(.*\))/\1/'
}

nested_get_core_revision_installed() {
    remote.exec "snap info core" | awk "/installed: / {print(\$3)}" | sed -e 's/(\(.*\))/\1/'
}

nested_fetch_spread() {
    if [ ! -f "$NESTED_WORK_DIR/spread" ]; then
        mkdir -p "$NESTED_WORK_DIR"
        curl -s https://storage.googleapis.com/snapd-spread-tests/spread/spread-amd64.tar.gz | tar -xz -C "$NESTED_WORK_DIR"
        # make sure spread really exists
        test -x "$NESTED_WORK_DIR/spread"
    fi
    echo "$NESTED_WORK_DIR/spread"
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

nested_wait_for_device_initialized_change() {
    local retry=60
    local wait=1

    while ! remote.exec "snap changes" | MATCH "Done.*Initialize device"; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for device to be fully initialized. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}
