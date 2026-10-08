#!/bin/bash

# Snap preparation and repacking helpers for snapd, kernel, gadget, and base.

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
        gojq '.results[0].snap.revision'
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

        if nested_is_core_ge 18; then
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

nested_prepare_snapd() {
    if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
        echo "Repacking snapd snap"
        local snap_name output_name snap_id
        if nested_is_core_16_system; then
            if [ ! -f "$NESTED_ASSETS_DIR/core-from-snapd-deb.snap" ]; then
                "$TESTSTOOLS"/snaps-state repack_snapd_deb_into_snap core "$NESTED_ASSETS_DIR"
                cp "$NESTED_ASSETS_DIR/core-from-snapd-deb.snap" "$(nested_get_extra_snaps_path)/core-from-snapd-deb.snap"
            fi
            # sign the snapd snap with fakestore if requested
            if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                "$TESTSTOOLS"/store-state make-snap-installable --noack "$NESTED_FAKESTORE_BLOB_DIR" "$(nested_get_extra_snaps_path)/core-from-snapd-deb.snap" "99T7MUlRhtI3U0QFgl5mXXESAiSwt776"
            fi
        else
            for f in "${NESTED_ASSETS_DIR}"/snapd_*.snap; do
                snap_name="$(basename "${f}")"
                break
            done
            if [ ! -f "${NESTED_ASSETS_DIR}/${snap_name}" ]; then
                # shellcheck source=tests/lib/prepare.sh
                . "$TESTSLIB"/prepare.sh
                build_snapd_snap "$NESTED_ASSETS_DIR"
                for f in "${NESTED_ASSETS_DIR}"/snapd_*.snap; do
                    snap_name="$(basename "${f}")"
                    break
                done
                cp "${NESTED_ASSETS_DIR}/${snap_name}" "$(nested_get_extra_snaps_path)/"
            fi
            # sign the snapd snap with fakestore if requested
            if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                "$TESTSTOOLS"/store-state make-snap-installable --noack "$NESTED_FAKESTORE_BLOB_DIR" "$(nested_get_extra_snaps_path)/${snap_name}" "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4"
            fi
        fi
    fi
}

nested_prepare_kernel() {
    # allow repacking the kernel
    if [ "$NESTED_REPACK_KERNEL_SNAP" = "true" ]; then
        echo "Repacking kernel snap"
        local kernel_snap output_name snap_id version core_branch
        output_name="pc-kernel.snap"
        snap_id="pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza"
        version="$(nested_get_version)"
        if [ "$version" = 16 ]; then
            core_branch=latest
        else
            core_branch="$version"
        fi

        if [ ! -f "$NESTED_ASSETS_DIR/$output_name" ]; then
            local epoch_bump_time kernel_channel
            local -a repack_kernel_args
            kernel_snap="$NESTED_ASSETS_DIR/$output_name"

            kernel_channel="$(nested_get_kernel_channel)"
            repack_kernel_args=(
                --mode nested
                --core-version "$version"
                --kernel-branch "$core_branch"
                --kernel-channel "$kernel_channel"
                --output-snap "$kernel_snap"
            )

            # For UC20+, pass epoch bump when configured.
            if nested_is_core_ge 20; then
                epoch_bump_time=${NESTED_CORE20_INITRAMFS_EPOCH_TIMESTAMP:-}
                if [ -n "$epoch_bump_time" ]; then
                    repack_kernel_args+=(--epoch-bump-time "$epoch_bump_time")
                fi
            fi

            "$TESTSTOOLS"/repack-kernel "${repack_kernel_args[@]}"
        fi
        cp "$NESTED_ASSETS_DIR/$output_name" "$(nested_get_extra_snaps_path)/$output_name"

        # sign the pc-kernel snap with fakestore if requested
        if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
            local extra_decl_args=""
            local kernel_decl="$NESTED_FAKESTORE_SNAP_DECL_PC_KERNEL"
            if [ -z "$kernel_decl" ] ; then
                kernel_decl="$TESTSLIB/assertions/pc-kernel-snap-decl-extras.json"
            fi
            if [ -n "$kernel_decl" ]; then
                extra_decl_args="--extra-decl-json $kernel_decl"
            fi
            # shellcheck disable=SC2086
            "$TESTSTOOLS"/store-state make-snap-installable --noack $extra_decl_args "$NESTED_FAKESTORE_BLOB_DIR" "$(nested_get_extra_snaps_path)/$output_name" "$snap_id"
        fi
    fi
}

nested_get_gadget_extra_cmdline() {
    local extra_cmdline=""
    if [ "$NESTED_SNAPD_DEBUG_TO_SERIAL" = "true" ]; then
        extra_cmdline="console=ttyS0 snapd.debug=1 systemd.journald.forward_to_console=1"
    elif os.query is-arm; then
        extra_cmdline="console=ttyAMA0 snapd.debug=1 systemd.journald.forward_to_console=1"
    fi
    if [ -n "$TAG_FEATURES" ]; then
        extra_cmdline="$extra_cmdline tag.features=1"
    fi
    if [ -n "$NESTED_EXTRA_CMDLINE" ]; then
        extra_cmdline="ds=nocloud $extra_cmdline $NESTED_EXTRA_CMDLINE"
    fi
    echo "$extra_cmdline"
}

nested_sign_gadget_snap() {
    local gadget_snap="$1"
    local snap_id="UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH"

    if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
        "$TESTSTOOLS"/store-state make-snap-installable --noack --extra-decl-json "$NESTED_FAKESTORE_SNAP_DECL_PC_GADGET" "$NESTED_FAKESTORE_BLOB_DIR" "$gadget_snap" "$snap_id"
    fi
}

nested_repack_core_gadget() {
    local key_name snakeoil_key snakeoil_cert gadget_extra_cmdline
    key_name="$(nested_get_snakeoil_key)"
    snakeoil_key="$PWD/$key_name.key"
    snakeoil_cert="$PWD/$key_name.pem"
    gadget_extra_cmdline="$(nested_get_gadget_extra_cmdline)"

    local -a repack_gadget_args=(
        --gadget-branch "$(nested_get_version)"
        --gadget-channel "$(nested_get_gadget_channel)"
        --output-snap "$NESTED_ASSETS_DIR/pc_repacked.snap"
        --sign-key "$snakeoil_key"
        --sign-cert "$snakeoil_cert"
        --persistent-journal
    )
    case "${NESTED_UBUNTU_SAVE:-}" in
        add)
            repack_gadget_args+=(--ubuntu-save add)
            touch ubuntu-save-added
            ;;
        remove)
            repack_gadget_args+=(--ubuntu-save remove)
            touch ubuntu-save-removed
            ;;
    esac
    if [ -n "$gadget_extra_cmdline" ]; then
        echo "Configuring command line parameters in the gadget snap: \"$gadget_extra_cmdline\""
        repack_gadget_args+=(--write-cmdline-extra "$gadget_extra_cmdline")
    fi
    if [ -n "$NESTED_UBUNTU_SEED_SIZE" ]; then
        repack_gadget_args+=(--ubuntu-seed-size "$NESTED_UBUNTU_SEED_SIZE")
    fi
    if [ "$NESTED_REPACK_FOR_FAKESTORE" = "true" ]; then
        repack_gadget_args+=(--prepare-device-url http://10.0.2.2:11029)
    fi

    "$TESTSTOOLS"/repack-gadget "${repack_gadget_args[@]}"
    cp "$NESTED_ASSETS_DIR/pc_repacked.snap" "$(nested_get_extra_snaps_path)/pc.snap"
    rm -f "$snakeoil_key" "$snakeoil_cert"
}

nested_prepare_gadget() {
    if [ "$NESTED_REPACK_GADGET_SNAP" != "true" ]; then
        return
    fi

    if nested_is_core_ge 20; then
        local existing_snap
        existing_snap="$(find "$(nested_get_extra_snaps_path)" -maxdepth 1 -type f \( -name pc.snap -o -name 'pc_*.snap' \) -print -quit)"
        if [ -n "$existing_snap" ]; then
            echo "Using generated pc gadget snap $existing_snap"
            nested_sign_gadget_snap "$existing_snap"
            return
        fi

        echo "Repacking pc snap"
        nested_repack_core_gadget
        nested_sign_gadget_snap "$(nested_get_extra_snaps_path)/pc.snap"
    fi

    if [ -n "$TAG_FEATURES" ] && nested_is_core_18_system; then
        local gadget_snap="$NESTED_ASSETS_DIR/pc_repacked.snap"
        "$TESTSTOOLS"/repack-gadget --gadget-branch 18 --gadget-channel "$(nested_get_gadget_channel)" --output-snap "$gadget_snap" --persistent-journal --tag-features-grub
        cp "$gadget_snap" "$(nested_get_extra_snaps_path)/pc.snap"
        nested_sign_gadget_snap "$(nested_get_extra_snaps_path)/pc.snap"
    fi
}

nested_get_base_snap_details() {
    case "$(nested_get_version)" in
        18) echo "core18 CSO04Jhav2yK0uz97cr0ipQRyqg0qQL6" ;;
        20) echo "core20 DLqre5XGLbDqg9jPtiAhRRjDuPVa5X1q" ;;
        22) echo "core22 amcUKQILKXHHTlmSa7NMdnXSx02dNeeT" ;;
        24) echo "core24 dwTAh7MZZ01zyriOZErqd1JynQLiOGvM" ;;
        26) echo "core26 cUqM61hRuZAJYmIS898Ux66VY61gBbZf" ;;
        *)
            echo "Unknown nested core version" >&2
            return 1
            ;;
    esac
}

nested_repack_base() {
    local snap_name="$1"
    local output_name="$2"
    local base_branch=latest base_channel
    base_channel="$(nested_get_base_channel)"
    if [[ "$base_channel" = */* ]]; then
        base_branch="${base_channel%/*}"
        base_channel="${base_channel##*/}"
    fi

    local -a repack_base_args=(
        --base-name "$snap_name"
        --base-branch "$base_branch"
        --base-channel "$base_channel"
        --output-snap "$NESTED_ASSETS_DIR/$output_name"
        --enable-test-logging
        --completion-file "$SPREAD_PATH/data/completion/bash/complete.sh"
    )
    if [ "$NESTED_REPACK_FOR_FAKESTORE" = true ]; then
        repack_base_args+=(--store-url http://10.0.2.2:11028)
    fi
    if [ "${SNAPD_USE_PROXY:-}" = true ]; then
        repack_base_args+=(--proxy-env /etc/environment)
    fi
    "$TESTSTOOLS"/repack-base "${repack_base_args[@]}"
}

nested_sign_base_snap() {
    local base_snap="$1"
    local snap_id="$2"
    if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
        "$TESTSTOOLS"/store-state make-snap-installable --noack "$NESTED_FAKESTORE_BLOB_DIR" "$base_snap" "$snap_id"
    fi
}

nested_prepare_base() {
    if [ "$NESTED_REPACK_BASE_SNAP" != "true" ]; then
        return
    fi
    if nested_is_core_16_system; then
        echo "No base snap to prepare in core 16"
        return
    fi

    local snap_name snap_id output_name existing_snap
    read -r snap_name snap_id < <(nested_get_base_snap_details) || return 1
    output_name="${snap_name}.snap"
    existing_snap="$(find "$(nested_get_extra_snaps_path)" -name "${snap_name}*.snap" -print -quit)"
    if [ -n "$existing_snap" ]; then
        echo "Using generated base snap $existing_snap"
        nested_sign_base_snap "$existing_snap" "$snap_id"
        return
    fi

    if [ ! -f "$NESTED_ASSETS_DIR/$output_name" ]; then
        echo "Repacking $snap_name snap"
        nested_repack_base "$snap_name" "$output_name"
    fi
    cp "$NESTED_ASSETS_DIR/$output_name" "$(nested_get_extra_snaps_path)/$output_name"
    nested_sign_base_snap "$(nested_get_extra_snaps_path)/$output_name" "$snap_id"
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

nested_get_core_revision_for_channel() {
    local CHANNEL=$1
    remote.exec "snap info core" | awk "/${CHANNEL}: / {print(\$4)}" | sed -e 's/(\(.*\))/\1/'
}

nested_get_core_revision_installed() {
    remote.exec "snap info core" | awk "/installed: / {print(\$3)}" | sed -e 's/(\(.*\))/\1/'
}

