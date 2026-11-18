#!/bin/bash

# Secure Boot, OVMF, TPM state, credential, and EFI signing helpers.

nested_get_snakeoil_key() {
    nested_ensure_ovmf >/dev/null

    cp "${NESTED_ASSETS_DIR}/ovmf/secboot/DB.key" DB.key
    cp "${NESTED_ASSETS_DIR}/ovmf/secboot/DB.crt" DB.pem
    echo DB
}

nested_secboot_remove_signature() {
    local FILE="$1"
    while sbverify --list "$FILE" | grep "^signature [0-9]*$"; do
        sbattach --remove "$FILE"
    done
}

nested_secboot_sign_file() {
    local keep_signatures
    args=()
    while [ "${#}" -gt 0 ]; do
        case "${1}" in
            --keep-signatures)
                keep_signatures=1
                ;;
            *)
                args+=("${1}")
        esac
        shift
    done
    local FILE="${args[0]}"
    local KEY="${args[1]}"
    local CERT="${args[2]}"
    if [ "${keep_signatures+set}" != set ]; then
        nested_secboot_remove_signature "$FILE"
    fi
    sbsign --key "$KEY" --cert "$CERT" --output "$FILE" "$FILE"
}

nested_secboot_sign_gadget() {
    local GADGET_DIR="$1"
    local KEY="$2"
    local CERT="$3"
    if [ -f "$GADGET_DIR/fb.efi" ]; then
        nested_secboot_sign_file "$GADGET_DIR/fb.efi" "$KEY" "$CERT"
    fi
    nested_secboot_sign_file "$GADGET_DIR/shim.efi.signed" "$KEY" "$CERT"
}

nested_secboot_sign_kernel() {
    local KERNEL_DIR="$1"
    local KEY="$2"
    local CERT="$3"
    nested_secboot_sign_file "$KERNEL_DIR/kernel.efi" "$KEY" "$CERT"
}

nested_ensure_ovmf() {
    if [ -d "${NESTED_ASSETS_DIR}/ovmf" ]; then
        return
    fi
    if ! [ -f "${NESTED_ASSETS_DIR}/test-snapd-ovmf.snap" ]; then
        snap download --channel=latest/edge test-snapd-ovmf --basename=test-snapd-ovmf --target-directory="${NESTED_ASSETS_DIR}"
    fi
    unsquashfs -d "${NESTED_ASSETS_DIR}/ovmf" "${NESTED_ASSETS_DIR}/test-snapd-ovmf.snap"
    
    if os.query is-arm; then
        cp /usr/share/AAVMF/* "${NESTED_ASSETS_DIR}/ovmf/fw"
    fi
}

nested_vm_clear_tpm() {
    snap stop test-snapd-swtpm > /dev/null
    rm /var/snap/test-snapd-swtpm/current/tpm2-00.permall || true
    snap start test-snapd-swtpm > /dev/null
}

nested_vm_clear_uefi() {
    if os.query is-arm; then
        OVMF=QEMU
    else
        OVMF=OVMF
    fi
    rm -f "${NESTED_ASSETS_DIR}/ovmf/fw/${OVMF}_VARS.current.fd"
}

nested_vm_set_passphrase() {
    echo -n "${1}" >"${NESTED_ASSETS_DIR}/qemu-creds/snapd.fde.password"
}

nested_vm_set_recovery_key() {
    echo -n "${1}" >"${NESTED_ASSETS_DIR}/qemu-creds/snapd.fde.password"
}

