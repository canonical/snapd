#!/bin/bash

# Nested VM lifecycle helpers for starting, stopping, restarting, and destroying guests.

nested_uc20_transition_to_system_mode() {
    local recovery_system="$1"
    local mode="$2"

    if nested_is_core_le 18; then
        echo "Transition can be done just on uc20+ systems, exiting..."
        exit 1
    fi

    local current_boot_id
    current_boot_id=$(nested_get_boot_id)
    remote.exec "sudo snap reboot --$mode $recovery_system" || true
    nested_wait_for_reboot "$current_boot_id"

    # verify we are now in the requested mode
    if ! remote.exec "cat /proc/cmdline" | MATCH "snapd_recovery_mode=$mode"; then
        return 1
    fi

    # Copy tools to be used on tests
    nested_prepare_tools
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
    local CURRENT_IMAGE
    CURRENT_IMAGE=$1

    # Now qemu parameters are defined
    nested_create_vm_service "$CURRENT_IMAGE"

    local EXPECT_SHUTDOWN
    EXPECT_SHUTDOWN=${NESTED_EXPECT_SHUTDOWN:-}

    if [ "$EXPECT_SHUTDOWN" != "1" ]; then
        # Wait until the vm is ready to receive connections
        if ! nested_wait_vm_ready 120; then
            echo "failed to wait for the vm becomes ready to receive connections"
            return 1
        fi
        # Wait for the snap command to be available
        nested_wait_for_snap_command 120 1
        nested_wait_for_snap_seeded
        echo "Waiting for snap seeding to complete"
        # Copy tools to be used on tests
        nested_prepare_tools
        # Wait for cloud init to be done if the system is using cloud-init
        # Do not wait for cloud-init on arm because it is disabled and takes delayes the tests
        if [ "$NESTED_USE_CLOUD_INIT" = true ] && ! os.query is-arm; then
            echo "Waiting for cloud-init to finish"
            if ! remote.exec "retry --wait 1 -n 5 sh -c 'cloud-init status --wait'"; then
                # Error 2 means 'recoverable error', ignore that case
                ret=0
                remote.exec "cloud-init status" || ret=$?
                if [ "$ret" -ne 0 ] && [ "$ret" -ne 2 ]; then
                    echo "cloud-init finished with error $ret"
                    # FIXME: remove core26 case.
                    # See https://github.com/canonical/cloud-init/issues/6699
                    if nested_is_core_26_system; then
                        echo "Ignoring error on core26 for now"
                    else
                        exit 1
                    fi
                fi
            fi
        fi
        nested_setup_vm
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
        local IMAGE_PATH
        IMAGE_NAME="$(nested_get_image_name core)"
        if ! [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
            echo "No image found to be started"
            exit 1
        fi

        # images are created as sparse files, simple cp should preserve that
        # property
        IMAGE_PATH="$(realpath "$NESTED_IMAGES_DIR/$IMAGE_NAME")"
        cp -v "$IMAGE_PATH" "$CURRENT_IMAGE"

        # Start the nested core vm
        nested_start_core_vm_unit "$CURRENT_IMAGE"

        if [ ! -f "$IMAGE_PATH.configured" ]; then
            # configure ssh for first time
            nested_prepare_ssh
            sync

            # keep a copy of the current image if it is a generic image
            if nested_is_generic_image && [ "$NESTED_CONFIGURE_IMAGES" = "true" ]; then
                # Stop the current image and compress it
                nested_shutdown

                # Save the image with the name of the original image
                cp -v "${CURRENT_IMAGE}" "$IMAGE_PATH"
                touch "$IMAGE_PATH.configured"

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

nested_start_classic_vm() {
    local IMAGE_NAME
    IMAGE_NAME="$(nested_get_image_name classic)"

    # Preserve images customized by tests between build-image and create-vm.
    if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
        cp -v "$NESTED_IMAGES_DIR/$IMAGE_NAME.pristine" "$NESTED_IMAGES_DIR/$IMAGE_NAME"
    fi

    # Give extra disk space for the image
    qemu-img resize "$NESTED_IMAGES_DIR/$IMAGE_NAME" +4G

    # HACK: convert "classic" qcow2 to raw "core" image because we need
    # to boot with OVMF, but we do this to use shared vm code
    qemu-img convert -f qcow2 -O raw \
        "$NESTED_IMAGES_DIR/$IMAGE_NAME" \
        "$NESTED_IMAGES_DIR/$IMAGE_NAME.raw"
    mv -f "$NESTED_IMAGES_DIR/$IMAGE_NAME.raw" "$NESTED_IMAGES_DIR/$IMAGE_NAME"

    nested_create_vm_service "$NESTED_IMAGES_DIR/$IMAGE_NAME" "-drive file=$NESTED_ASSETS_DIR/seed.img,if=virtio"

    if ! nested_wait_vm_ready 60; then
        echo "failed to wait for the vm becomes ready to receive connections"
        return 1
    fi

    # Copy tools to be used on tests
    nested_wait_for_ssh
    nested_wait_for_snap_command 120 1
    nested_wait_for_snap_seeded
    nested_prepare_tools
    nested_setup_vm
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

