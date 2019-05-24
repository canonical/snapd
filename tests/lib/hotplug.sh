#!/bin/bash

# shellcheck source=tests/lib/nested.sh
. "$TESTSLIB"/nested.sh


hotplug_add_dev1() {
    add_tty_chardev my-chardev1 /tmp/serialport1
    add_usb_serial_device my-usb-serial my-chardev1 1234
}

hotplug_del_dev1() {
    del_device my-usb-serial
    remove_chardev my-chardev1
}

hotplug_add_dev2() {
    add_tty_chardev my-chardev2 /tmp/serialport2
    add_usb_serial_device my-usb-serial2 my-chardev2 5678
}

hotplug_del_dev2() {
    del_device my-usb-serial2
    remove_chardev my-chardev2
}

# Check that given slot is not present in 'snap connections' output
# (note, it can still be present in the state with hotplug-gone=true)
check_slot_not_present() {
    SLOT_NAME="$1"
    for _ in $(seq 10); do
        if ! execute_remote "snap connections system" | MATCH ":$SLOT_NAME"; then
            break
        fi
        sleep 1
    done
    if execute_remote "snap connections system" | MATCH ":$SLOT_NAME "; then
        echo "slot $SLOT_NAME shouldn't be present anymore"
        exit 1
    fi
}

# Check that given slot is present in 'snap connections' output (but is not connected)
check_slot_present() {
    SLOT_NAME="$1"
    for _ in $(seq 10); do
        if execute_remote "snap connections system" | MATCH "serial-port .* - .* :$SLOT_NAME"; then
            break
        fi
        sleep 1
    done
    execute_remote "snap connections system" | MATCH "serial-port .* - .* :$SLOT_NAME"
}

# Check that given slot has hotplug-gone=true, meaning the device was unplugged but there are connections remembered for it
check_slot_gone() {
    SLOT_NAME="$1"
    execute_remote 'sudo jq -r ".data[\"hotplug-slots\"][\"'"$SLOT_NAME"'\"][\"hotplug-gone\"]" /var/lib/snapd/state.json' | MATCH "true"
}

# Check that given slot has hotplug-gone=false, meaning the device is plugged
check_slot_not_gone() {
    SLOT_NAME="$1"
    execute_remote 'sudo jq -r ".data[\"hotplug-slots\"][\"'"$SLOT_NAME"'\"][\"hotplug-gone\"]" /var/lib/snapd/state.json' | MATCH "false"
}

# Check that given slot has no record in "hotplug-slots" map in the state
check_slot_not_present_in_state() {
    SLOT_NAME="$1"
    execute_remote 'sudo jq -r ".data[\"hotplug-slots\"][\"'"$SLOT_NAME"'\"] // \"missing\"" /var/lib/snapd/state.json' | MATCH "missing"
}

check_slot_device_path() {
    SLOT_NAME="$1"
    DEVICE_PATH="$2"
    execute_remote 'sudo jq -r ".data[\"hotplug-slots\"][\"'"$SLOT_NAME"'\"][\"static-attrs\"].path" /var/lib/snapd/state.json' | MATCH "$DEVICE_PATH"
}

# Check that given slot is connected to the serial-port-hotplug snap, per 'snap connections' output
check_slot_connected() {
    SLOT_NAME="$1"
    for _ in $(seq 10); do
        if execute_remote "snap connections" | MATCH "serial-port .*serial-port-hotplug:serial-port .*$SLOT_NAME"; then
            break
        fi
        sleep 1
    done
    execute_remote "snap connections" | MATCH "serial-port .*serial-port-hotplug:serial-port .*$SLOT_NAME"
}

# Check that apparmor profile allows rw access to given device path.
verify_apparmor_profile() {
    DEVPATH=$1
    for _ in $(seq 10); do
        if execute_remote "cat /var/lib/snapd/apparmor/profiles/snap.serial-port-hotplug.consumer" | MATCH "$DEVPATH rw,"; then
            break
        fi
        sleep 1
    done
    execute_remote "cat /var/lib/snapd/apparmor/profiles/snap.serial-port-hotplug.consumer" | MATCH "$DEVPATH rw,"
}

wait_for_all_changes() {
    for _ in $(seq 10); do
        if ! execute_remote "snap changes" | MATCH "Doing"; then
            break
        fi
        sleep 1
    done
}