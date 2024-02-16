#!/bin/bash

set -uxe

run_muinstaller() {
    # shellcheck source=tests/lib/prepare.sh
    . "${TESTSLIB}/prepare.sh"

    # shellcheck source=tests/lib/nested.sh
    . "${TESTSLIB}/nested.sh"

    local model_assertion="${1}"
    # since store_dir is optional, we need to make sure it's set to an empty
    # string to prevent using an unbound variable
    local store_dir="${2:-}"
    local gadget_snap="${3}"
    local gadget_assertion="${4}"
    local kernel_snap="${5}"
    local kernel_assertion="${6}"
    local label="${7}"
    local disk="${8}"

    # ack the needed assertions
    snap ack "${kernel_assertion}"
    snap ack "${gadget_assertion}"

    local key_name
    key_name=$(nested_get_snakeoil_key)
    local snakeoil_key="${PWD}/${key_name}.key"
    local snakeoil_cert="${PWD}/${key_name}.pem"

    # unpack the gadget snap
    unsquashfs -d pc-gadget "${gadget_snap}"

    # sign the shim binary
    tests.nested secboot-sign gadget pc-gadget "${snakeoil_key}" "${snakeoil_cert}"
    snap pack --filename=pc.snap pc-gadget/

    # prepare a classic seed
    # TODO:
    # - repacked snapd snap
    snap prepare-image --classic \
        --channel=edge \
        --snap "${kernel_snap}" \
        --snap pc.snap \
        "${model_assertion}" \
        ./classic-seed

    mv ./classic-seed/system-seed/systems/* "./classic-seed/system-seed/systems/${label}"
    cp -a ./classic-seed/system-seed/ /var/lib/snapd/seed

    if [ -n "${store_dir}" ]; then
        # if we have a store setup, then we should take it down for now
        "${TESTSTOOLS}/store-state" teardown-fake-store "${store_dir}"
    fi

    # build the muinstaller snap
    snap install snapcraft --candidate --classic
    "${TESTSTOOLS}/lxd-state" prepare-snap
    (cd "${TESTSLIB}/muinstaller" && snapcraft)

    local muinstaller_snap
    muinstaller_snap="$(find "${TESTSLIB}/muinstaller/" -maxdepth 1 -name '*.snap')"

    # create a VM and mount a cloud image
    tests.nested build-image classic

    # TODO: nested classic images do not support secure boot today so
    #       this will not work to test the secure boot installer. So for
    #       now the workaround is to boot classic to create user/ssh
    #       keys, shutdown down, convert disk from qcow2->raw and rename
    #       from classic->core and use nested_start_core_vm (like below)
    #
    # start it so that cloud-init creates ssh keys and user
    # We set a serial for our disk to easily locate it when invoking muinstaller (virtio-target)
    NESTED_PARAM_EXTRA="-drive file=${disk},if=none,snapshot=off,format=raw,id=disk2 \
        -device virtio-blk-pci,drive=disk2,serial=target"
    tests.nested create-vm classic --extra-param "${NESTED_PARAM_EXTRA}"

    # make sure classic image is bootable with snakeoil keys
    # TODO: move to nested_create_classic_image
    # XXX: use assets from gadget instead?
    for s in BOOT/BOOTX64.EFI ubuntu/shimx64.efi; do
        remote.exec "sudo cp -a /boot/efi/EFI/${s} /tmp"
        remote.exec "sudo chmod 755 /tmp/$(basename ${s})"
        remote.pull /tmp/"$(basename ${s})" .
        nested_secboot_sign_file "$(basename ${s})" "${snakeoil_key}" "${snakeoil_cert}"
        remote.push "$(basename ${s})"
        remote.exec "sudo mv $(basename ${s}) /boot/efi/EFI/${s}"
    done

    remote.exec "sudo sh -c 'echo SNAPD_DEBUG=1 >> /etc/environment'"
    # push our snap down
    # TODO: this abuses /var/lib/snapd to store the deb so that mk-initramfs-classic
    # can pick it up. the real installer will also need a very recent snapd
    # in its on disk-image to support seeding
    remote.push "${SPREAD_PATH}"/../snapd_*.deb
    remote.exec "sudo mv snapd_*.deb /var/lib/snapd/"
    remote.exec "sudo apt install -y /var/lib/snapd/snapd_*.deb"

    # push our seed down
    # TODO: merge with classic /var/lib/snapd/seed eventually
    # XXX: port scp -r to remote.push
    #remote.push ./classic-seed/system-seed/ '~/'
    sshpass -p ubuntu scp -r -P 8022 -o ConnectTimeout=10 \
        -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no \
        ./classic-seed/system-seed/ user1@localhost:~/install-seed
    remote.exec "sudo mv /home/user1/install-seed /var/lib/snapd/"

    # shutdown the classic vm to install with a core VM that supports
    # secboot/tpm
    tests.nested vm stop
    sync

    # HACK: convert "classic" qcow2 to raw "core" image because we need
    # to boot with OVMF we really should fix this so that classic and
    # core VMs are more similar
    qemu-img convert -f qcow2 -O raw \
        "${NESTED_IMAGES_DIR}/$(nested_get_image_name classic)" \
        "${NESTED_IMAGES_DIR}/$(nested_get_image_name core)"
    # and we don't need the classic image anymore
    # TODO: uncomment
    # rm -f  "$NESTED_IMAGES_DIR/$(nested_get_image_name classic)"
    # TODO: this prevents "nested_prepare_ssh" inside nested_start_core_vm
    #       from running, we already have a user so this is not needed
    local image_name
    image_name="$(nested_get_image_name core)"
    touch "${NESTED_IMAGES_DIR}/${image_name}.configured"
    tests.nested create-vm core --extra-param "${NESTED_PARAM_EXTRA}"

    # bind mount new seed
    remote.exec "sudo mount -o bind /var/lib/snapd/install-seed /var/lib/snapd/seed"
    # push and install muinstaller
    remote.push "${muinstaller_snap}"
    remote.exec "sudo snap install --classic --dangerous $(basename "${muinstaller_snap}")"

    # run installation
    local install_disk
    install_disk=$(remote.exec "readlink -f /dev/disk/by-id/virtio-target")
    remote.exec "sudo muinstaller classic \
        ${install_disk} /snap/muinstaller/current/bin/mk-classic-rootfs.sh"

    remote.exec "sudo sync"

    # Stop and remove the classic vm now that the attached disk (${disk})
    # contains a just installed UC image.
    tests.nested vm remove
    sync

    # HACK: rename to "core" image because we need to boot with OVMF
    # we really should fix this so that classic and core VMs are more similar
    mv "${disk}" "${NESTED_IMAGES_DIR}/${image_name}"

    # Change seed part label to capitals so we cover that use case
    image_path="${NESTED_IMAGES_DIR}/${image_name}"
    kpartx -asv "${image_path}"
    fatlabel /dev/disk/by-label/ubuntu-seed UBUNTU-SEED
    kpartx -d "${image_path}"

    if [ -n "${store_dir}" ]; then
        # if we had a store setup, then we should bring it back up
        "${TESTSTOOLS}/store-state" setup-fake-store "${store_dir}"
    fi

    # Start installed image
    tests.nested create-vm core --tpm-no-restart
}

main() {
    local model_assertion=""
    local store_dir=""
    local gadget_snap=""
    local gadget_assertion=""
    local kernel_snap=""
    local kernel_assertion=""
    local label="classic"
    local disk=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --model)
                model_assertion="${2}"
                shift 2
                ;;
            --store-dir)
                store_dir="${2}"
                shift 2
                ;;
            --gadget)
                gadget_snap="${2}"
                shift 2
                ;;
            --gadget-assertion)
                gadget_assertion="${2}"
                shift 2
                ;;
            --kernel)
                kernel_snap="${2}"
                shift 2
                ;;
            --kernel-assertion)
                kernel_assertion="${2}"
                shift 2
                ;;
            --label)
                label="${2}"
                shift 2
                ;;
            --disk)
                disk="${2}"
                shift 2
                ;;
            --*|-*)
                echo "Unknown option ${1}"
                exit 1
                ;;
            *)
                shift
                ;;
        esac
    done

    if [ -z "${model_assertion}" ]; then
        echo "--model is required"
        exit 1
    fi

    if [ -n "${kernel_snap}" ] && [ -z "${kernel_assertion}" ]; then
        echo "--kernel-assertion is required when --kernel is provided"
        exit 1
    fi

    if [ -n "${kernel_assertion}" ] && [ -z "${kernel_snap}" ]; then
        echo "--kernel is required when --kernel-assertion is provided"
        exit 1
    fi

    if [ -n "${gadget_snap}" ] && [ -z "${gadget_assertion}" ]; then
        echo "--gadget-assertion is required when --gadget is provided"
        exit 1
    fi

    if [ -n "${gadget_assertion}" ] && [ -z "${gadget_snap}" ]; then
        echo "--gadget is required when --gadget-assertion is provided"
        exit 1
    fi

    # since we change directories below, we need to make sure we have absolute
    # paths for all inputs
    model_assertion="$(realpath "${model_assertion}")"

    if [ -n "${disk}" ]; then
        disk="$(realpath "${disk}")"
    fi

    if [ -n "${store_dir}" ]; then
        store_dir="$(realpath "${store_dir}")"
    fi

    if [ -n "${gadget_snap}" ]; then
        gadget_snap="$(realpath "${gadget_snap}")"
        gadget_assertion="$(realpath "${gadget_assertion}")"
    fi

    if [ -n "${kernel_snap}" ]; then
        kernel_snap="$(realpath "${kernel_snap}")"
        kernel_assertion="$(realpath "${kernel_assertion}")"
    fi

    # start a subshell and change directories so that we can change directories
    # to keep all of our generated files together
    (
    cd "$(mktemp -d --tmpdir="${PWD}")"

    # create new disk (if the caller didn't provide one) for the installer to
    # work on and attach to the VM
    if [ -z "${disk}" ]; then
        disk="${PWD}/disk.img"
        truncate --size=6G "${disk}"
    fi

    # if a gadget wasn't provided, download one we know should work for hybrid
    # systems
    if [ -z "${gadget_snap}" ]; then
        snap download --channel="classic-23.10/stable" --basename=pc pc
        gadget_snap="${PWD}/pc.snap"
        gadget_assertion="${PWD}/pc.assert"
    fi

    # if a kernel wasn't provided, download one
    if [ -z "${kernel_snap}" ]; then
        snap download --channel="23.10/stable" --basename=pc-kernel pc-kernel
        kernel_snap="${PWD}/pc-kernel.snap"
        kernel_assertion="${PWD}/pc-kernel.assert"
    fi

    run_muinstaller "${model_assertion}" "${store_dir}" "${gadget_snap}" \
        "${gadget_assertion}" "${kernel_snap}" "${kernel_assertion}" "${label}" \
        "${disk}"

    )
}

main "$@"
