#!/bin/bash

# System and backend predicates used to select version-specific nested behavior.

nested_is_kvm_enabled() {
    if [ -n "$NESTED_ENABLE_KVM" ]; then
        [ "$NESTED_ENABLE_KVM" = true ]
    fi
    return 0
}

nested_is_kvm_supported() {
    test -e /dev/kvm
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
                if os.query is-arm; then
                    # TPM disabled by default on arm
                    return 1
                else 
                    # TPM enabled by default on 20.04 and later
                    return 0
                fi
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
                if os.query is-arm; then
                    # secure boot disabled by default on arm
                    return 1
                else 
                    # secure boot enabled by default on 20.04 and later
                    return 0
                fi
                ;;
            *)
                echo "unsupported system"
                exit 1
                ;;
        esac
    fi
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

nested_is_core_ge() {
    local VERSION=$1
    os.query is-ubuntu-ge "${VERSION}.04"
}

nested_is_core_gt() {
    local VERSION=$1
    os.query is-ubuntu-gt "${VERSION}.04"
}

nested_is_core_le() {
    local VERSION=$1
    os.query is-ubuntu-le "${VERSION}.04"
}

nested_is_core_lt() {
    local VERSION=$1
    os.query is-ubuntu-lt "${VERSION}.04"
}

nested_is_core_26_system() {
    os.query is-resolute
}

nested_is_core_24_system() {
    os.query is-noble
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

nested_get_version() {
    if nested_is_core_16_system; then
        echo "16"
    elif nested_is_core_18_system; then
        echo "18"
    elif nested_is_core_20_system; then
        echo "20"
    elif nested_is_core_22_system; then
        echo "22"
    elif nested_is_core_24_system; then
        echo "24"
    elif nested_is_core_26_system; then
        echo "26"
    fi
}

