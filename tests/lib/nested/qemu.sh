#!/bin/bash

# QEMU argument, firmware, logging, service, and runtime device helpers.

nested_qemu_name() {
    if os.query is-arm; then
        command -v qemu-system-aarch64
    else
        command -v qemu-system-x86_64
    fi

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

nested_configure_vm_resources() {
    if [ "$SPREAD_BACKEND" = "qemu-nested" ] || [ "$SPREAD_BACKEND" = "garden" ]; then
        PARAM_MEM="-m ${NESTED_MEM:-2048}"
        PARAM_SMP="-smp ${NESTED_CPUS:-1}"
    elif [[ "$SPREAD_BACKEND" = openstack-arm-ext* ]]; then
        PARAM_MEM="-m ${NESTED_MEM:-8192}"
        PARAM_SMP="-smp ${NESTED_CPUS:-6}"
    elif [[ "$SPREAD_BACKEND" = openstack-ext* ]] || [[ "$SPREAD_BACKEND" = "openstack-validation" ]]; then
        PARAM_MEM="-m ${NESTED_MEM:-4096}"
        PARAM_SMP="-smp ${NESTED_CPUS:-3}"
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi
}

nested_configure_legacy_vm_machine() {
    if [[ "$SPREAD_BACKEND" = openstack* ]]; then
        PARAM_MACHINE="-machine ubuntu${ATTR_KVM}"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ] || [ "$SPREAD_BACKEND" = "garden" ]; then
        if [ "$(cat /sys/module/kvm_*/parameters/nested)" = "1" ]; then
            PARAM_MACHINE="-machine ubuntu${ATTR_KVM}"
        else
            PARAM_MACHINE=""
            PARAM_CPU=""
            ATTR_KVM=""
        fi
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi
}

nested_configure_vm_firmware() {
    if [ -n "$NESTED_CUSTOM_FIRMWARE" ]; then
        PARAM_BIOS="-drive file=${NESTED_CUSTOM_FIRMWARE},if=pflash,format=raw,readonly=on"
        return
    fi

    nested_ensure_ovmf
    local OVMF_CODE OVMF_VARS OVMF_VARS_SECBOOT OVMF_VARS_CURRENT OVMF
    if os.query is-arm; then
        OVMF=AAVMF
        OVMF_VARS_SECBOOT="${NESTED_ASSETS_DIR}/ovmf/fw/${OVMF}_VARS.ms.fd"
    else
        OVMF=OVMF
        OVMF_VARS_SECBOOT="${NESTED_ASSETS_DIR}/ovmf/fw/${OVMF}_VARS.enrolled.fd"
    fi
    OVMF_CODE="${NESTED_ASSETS_DIR}/ovmf/fw/${OVMF}_CODE.fd"
    OVMF_VARS="${NESTED_ASSETS_DIR}/ovmf/fw/${OVMF}_VARS.fd"
    OVMF_VARS_CURRENT="${NESTED_ASSETS_DIR}/ovmf/fw/${OVMF}_VARS.current.fd"

    if [ -z "$NESTED_KEEP_FIRMWARE_STATE" ] || ! [ -e "${OVMF_VARS_CURRENT}" ]; then
        if nested_is_secure_boot_enabled; then
            cp -fv "${OVMF_VARS_SECBOOT}" "${OVMF_VARS_CURRENT}"
        else
            cp -fv "${OVMF_VARS}" "${OVMF_VARS_CURRENT}"
        fi
    fi
    PARAM_BIOS="-drive file=${OVMF_CODE},if=pflash,format=raw,readonly=on -drive file=${OVMF_VARS_CURRENT},if=pflash,format=raw"
}

nested_configure_vm_tpm() {
    if ! nested_is_tpm_enabled; then
        return
    fi

    if snap list test-snapd-swtpm >/dev/null; then
        if [ -z "${NESTED_KEEP_FIRMWARE_STATE-}" ]; then
            nested_vm_clear_tpm
        fi
    else
        snap install test-snapd-swtpm --edge
    fi
    retry -n 10 --wait 1 test -S /var/snap/test-snapd-swtpm/current/swtpm-sock
    PARAM_TPM="-chardev socket,id=chrtpm,path=/var/snap/test-snapd-swtpm/current/swtpm-sock -tpmdev emulator,id=tpm0,chardev=chrtpm"
    if os.query is-arm; then
        PARAM_TPM="$PARAM_TPM -device tpm-tis-device,tpmdev=tpm0"
    else
        PARAM_TPM="$PARAM_TPM -device tpm-tis,tpmdev=tpm0"
    fi
}

nested_create_vm_service() {
    local QEMU CURRENT_IMAGE PARAM_OPT
    CURRENT_IMAGE=$1
    PARAM_OPT="${2:-}"
    QEMU=$(nested_qemu_name)

    # Due to a bug in apparmor, on 26.04 the netcat apparmor profile is not
    # allowing access to the ports exposed by qemu, remove it for the moment.
    # Note that this function might be called for different tests running on
    # the same host, so we ensure that the call does not fail.
    # TODO remove once LP#2143151 is fixed.
    apparmor_parser -R /etc/apparmor.d/nc.openbsd || true

    # Now qemu parameters are defined

    # use only 2G of RAM for qemu-nested
    # the caller can override PARAM_MEM
    local PARAM_MEM PARAM_SMP
    nested_configure_vm_resources

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
    local ATTR_KVM PARAM_CPU PARAM_MACHINE
    ATTR_KVM=""
    if nested_is_kvm_enabled && nested_is_kvm_supported; then
        ATTR_KVM=",accel=kvm"
        # CPU can be defined just when kvm is enabled
        PARAM_CPU="-cpu host"
    fi

    local PARAM_ASSERTIONS
    PARAM_ASSERTIONS=""
    if [ "$NESTED_USE_CLOUD_INIT" != "true" ]; then
        # TODO: fix using the old way of an ext4 formatted drive w/o partitions
        #       as this used to work but has since regressed
        
        # this simulates a usb drive attached to the device, the removable=true
        # is necessary otherwise snapd will not import it, as snapd only 
        # considers removable devices for cold-plug first-boot runs
        # the nec-usb-xhci device is necessary to create the bus we attach the
        # storage to
        PARAM_ASSERTIONS="-drive if=none,id=stick,format=raw,file=$NESTED_ASSETS_DIR/assertions.disk,cache=unsafe,aio=threads,format=raw -device nec-usb-xhci,id=xhci -device usb-storage,bus=xhci.0,removable=true,drive=stick"
    fi

    local PARAM_BIOS PARAM_TPM PARAM_IMAGE
    PARAM_BIOS=""
    PARAM_TPM=""
    PARAM_REEXEC_ON_FAILURE=""

    if nested_is_core_lt 20; then
        nested_configure_legacy_vm_machine
    fi

    if nested_is_core_ge 20; then
        nested_configure_vm_firmware

        local ENABLE_ARM_TRUSTZONE
        ENABLE_ARM_TRUSTZONE=""
        if [ "$NESTED_ENABLE_ARM_TRUSTZONE" = true ]; then
            ENABLE_ARM_TRUSTZONE=",secure=on"
        fi
    
        if os.query is-arm; then
            PARAM_MACHINE="-machine virt${ENABLE_ARM_TRUSTZONE} -accel tcg,thread=multi"
            PARAM_CPU="-cpu neoverse-n1"
        else
            PARAM_MACHINE="-machine q35${ATTR_KVM}"
        fi

        nested_configure_vm_tpm
        # addr=5 is to make tests/nested/manual/install-volume-assignment have stable address
        PARAM_IMAGE="-drive file=$CURRENT_IMAGE,cache=none,format=raw,id=disk1,if=none -device virtio-blk-pci,drive=disk1,bootindex=1,addr=5"
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

    rm -rf "${NESTED_ASSETS_DIR}"/qemu-creds
    mkdir -p "${NESTED_ASSETS_DIR}"/qemu-creds
    if [ "${NESTED_FDE_PASSWORD+set}" = set ]; then
        echo -n "${NESTED_FDE_PASSWORD}" >"${NESTED_ASSETS_DIR}/qemu-creds/snapd.fde.password"
    fi

    # ensure we have a log dir
    mkdir -p "$NESTED_LOGS_DIR"
    # make sure we start with clean log file
    echo > "${NESTED_LOGS_DIR}/serial.log"
    # Systemd unit is created, it is important to respect the qemu parameters order

    tests.systemd create-and-start-unit "$NESTED_VM" "${TESTSLIB}/qemu-runner.sh ${NESTED_ASSETS_DIR}/qemu-creds ${QEMU} \
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
        ${PARAM_OPT} \
        ${PARAM_EXTRA} " "${PARAM_REEXEC_ON_FAILURE}"
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

nested_add_usb_drive() {
    local DEVICE_ID=$1
    local DRIVE_FILE=$2

    # A device_del of $DEVICE_ID also will remove the drive
    echo "drive_add 0 if=none,file=$DRIVE_FILE,format=raw,id=rawdisk" | nc -q 0 127.0.0.1 "$NESTED_MON_PORT"
    echo "device_add usb-storage,drive=rawdisk,id=$DEVICE_ID" | nc -q 0 127.0.0.1 "$NESTED_MON_PORT"
    echo "USB drive device added"
}

nested_del_device() {
    local DEVICE_ID=$1
    echo "device_del $DEVICE_ID" | nc -q 0 127.0.0.1 "$NESTED_MON_PORT"
    echo "device deleted"
}

