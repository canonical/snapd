#!/bin/bash

# Wait helpers for guest SSH, reboot, snap readiness, and initialization.

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
            journalctl -u "${NESTED_VM}"
            return 1
        fi

        # Check if ssh connection can be established, and return if it is possible
        if nested_wait_for_ssh 1 1; then
            echo "SSH connection ready"
            return
        fi

        # Check no infinite loops during boot
        if nested_is_core_ge 20; then
            test "$(grep -c -E "Command line:.*snapd_recovery_mode=install" "$serial_log")" -le 1
            test "$(grep -c -E "Command line:.*snapd_recovery_mode=run" "$serial_log")" -le 1
        else
            test "$(grep -c -E "Command line:.*BOOT_IMAGE=\(loop\)/kernel.img" "$serial_log")" -le 1
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
    echo "Snap command is ready"
}

nested_wait_for_snap_seeded() {
    # Retry since snap may briefly disappear during the initial snapd restart.
    local attempts=0
    until remote.exec "sudo snap wait system seed.loaded"; do
        attempts=$(( attempts + 1 ))
        if [ "$attempts" = 3 ]; then
            echo "failed to wait for snap wait command to return successfully"
            return 1
        fi
        sleep 1
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
