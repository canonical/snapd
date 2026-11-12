#!/bin/bash

# Daily desktop image download, autoinstall seed, and firmware boot helpers.
#
# The guest boots the Ubuntu desktop ISO through OVMF, the same way the
# known-good QEMU recipe does: enrolled secure-boot variables, a virtual TPM,
# the ISO as a SCSI CD, and a virtio target disk. Autoinstall is delivered on
# a second SCSI CD. After the installer powers off, the same disk is booted
# again with the OVMF variables and TPM state kept.

nested_desktop_iso_url() {
    if [ -n "${NESTED_UBUNTU_ISO_URL:-}" ]; then
        printf '%s' "$NESTED_UBUNTU_ISO_URL"
        return 0
    fi

    local codename
    case "$(nested_get_version)" in
        26)
            codename=resolute
            ;;
        *)
            echo "cannot map this Ubuntu version to a daily desktop image" >&2
            return 1
            ;;
    esac
    printf 'https://cdimage.ubuntu.com/ubuntu/%s/daily-live/current/%s-desktop-amd64.iso' \
        "$codename" "$codename"
}

nested_download_desktop_iso() {
    local url dest
    if ! url="$(nested_desktop_iso_url)"; then
        return 1
    fi
    dest="${NESTED_ASSETS_DIR}/ubuntu-desktop-daily.iso"
    mkdir -p "$NESTED_ASSETS_DIR"
    rm -f "$dest"
    echo "Downloading daily desktop image ${url}" >&2
    if ! curl -fL --retry 3 --retry-delay 2 -o "$dest" "$url"; then
        rm -f "$dest"
        echo "cannot download daily desktop image ${url}" >&2
        return 1
    fi
    printf '%s' "$dest"
}

nested_create_installer_disk() {
    local disk="${NESTED_IMAGES_DIR}/ubuntu-desktop-installed.img"
    mkdir -p "$NESTED_IMAGES_DIR"
    rm -f "$disk"
    if ! truncate --size 25G "$disk"; then
        echo "cannot create installer disk ${disk}" >&2
        return 1
    fi
    printf '%s' "$disk"
}

nested_guest_proxy() {
    local proxy="${https_proxy:-${HTTPS_PROXY:-${http_proxy:-${HTTP_PROXY:-}}}}"
    if [ -z "$proxy" ]; then
        return 0
    fi
    printf '%s' "$proxy"
}

nested_autoinstall_snippet() {
    local config="${NESTED_AUTOINSTALL_CONFIG:-}"
    if [ -z "$config" ]; then
        echo "cannot find autoinstall config" >&2
        return 1
    fi
    if [ ! -f "$config" ]; then
        echo "cannot find autoinstall config ${config}" >&2
        return 1
    fi
    # Insert the caller's snippet unchanged. The seed already has autoinstall
    # and version, so the file is only the extra keys.
    cat "$config"
}

nested_live_snapd_setup() {
    case "${NESTED_SNAPD}" in
        iso)
            cat <<'EOF'
    if printf '%s\n' '[Service]' 'Environment=SNAPPY_TESTING=1' | systemctl edit snapd.service --stdin --drop-in=99-nested-testing.conf && systemctl restart snapd.socket snapd.service; then
      echo nested-autoinstall-snapd-testing >/dev/ttyS1
    else
      echo nested-autoinstall-snapd-testing-failed >/dev/ttyS1
      systemctl list-units --all '*snapd*' >/dev/ttyS0 2>&1 || true
      exit 1
    fi
EOF
            ;;
        branch)
            # The image snapd may be newer than the branch snap, so re-exec
            # happens only when SNAP_REEXEC=force.
            cat <<'EOF'
    mkdir -p /mnt/cidata
    if ! mount -o ro /dev/disk/by-label/cidata /mnt/cidata; then
      echo nested-autoinstall-snapd-reexec-failed >/dev/ttyS1
      exit 1
    fi
    if ! snap install --dangerous /mnt/cidata/snapd.snap >/dev/ttyS0 2>&1; then
      echo nested-autoinstall-snapd-reexec-failed >/dev/ttyS1
      exit 1
    fi
    if ! printf '%s\n' '[Service]' 'Environment=SNAPPY_TESTING=1' 'Environment=SNAP_REEXEC=force' | systemctl edit snapd.service --stdin --drop-in=99-nested-testing.conf || ! systemctl restart snapd.socket snapd.service; then
      echo nested-autoinstall-snapd-testing-failed >/dev/ttyS1
      systemctl list-units --all '*snapd*' >/dev/ttyS0 2>&1 || true
      exit 1
    fi
    echo nested-autoinstall-snapd-testing >/dev/ttyS1
    pid="$(systemctl show -p MainPID --value snapd)"
    exe="$(readlink -f "/proc/${pid}/exe" 2>/dev/null || true)"
    echo "nested-autoinstall-snapd-exe ${exe}" >/dev/ttyS0
    case "$exe" in
      /snap/snapd/*)
        echo nested-autoinstall-snapd-reexec >/dev/ttyS1
        ;;
      *)
        echo nested-autoinstall-snapd-reexec-failed >/dev/ttyS1
        exit 1
        ;;
    esac
EOF
            ;;
        *)
            echo "cannot use desktop snapd ${NESTED_SNAPD}" >&2
            return 1
            ;;
    esac
}

nested_branch_snapd_snap() {
    local snap="" candidate
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if ! build_snapd_snap "$NESTED_ASSETS_DIR"; then
        echo "cannot build snapd snap" >&2
        return 1
    fi
    for candidate in "$NESTED_ASSETS_DIR"/snapd_*.snap; do
        if [ -f "$candidate" ]; then
            snap="$candidate"
            break
        fi
    done
    if [ -z "$snap" ]; then
        echo "cannot find snapd snap" >&2
        return 1
    fi
    cp "$snap" "$NESTED_ASSETS_DIR/snapd.snap"
    printf '%s' "$NESTED_ASSETS_DIR/snapd.snap"
}

nested_write_autoinstall_seed_image() {
    local seed="$1"
    local user_data="$2"
    local meta_data="$3"
    local snap tmp
    if [ "$NESTED_SNAPD" != "branch" ]; then
        if ! cloud-localds "$seed" "$user_data" "$meta_data" >&2; then
            echo "cannot create autoinstall seed ${seed}" >&2
            return 1
        fi
        return 0
    fi
    if ! snap="$(nested_branch_snapd_snap)"; then
        return 1
    fi
    tmp="$(mktemp -d)"
    cp "$user_data" "$tmp/user-data"
    cp "$meta_data" "$tmp/meta-data"
    cp "$snap" "$tmp/snapd.snap"
    # Match cloud-localds: cidata volume label, Joliet, and Rock Ridge names.
    if ! genisoimage -output "$seed" -volid cidata -joliet -rock \
        "$tmp/user-data" "$tmp/meta-data" "$tmp/snapd.snap" >&2; then
        rm -rf "$tmp"
        echo "cannot create autoinstall seed ${seed}" >&2
        return 1
    fi
    rm -rf "$tmp"
}

nested_render_autoinstall_user_data() {
    local template="${TESTSLIB}/nested/autoinstall-user-data.in"
    local document password_hash source_id proxy proxy_line storage live_snapd_setup token
    if [ ! -f "$template" ]; then
        echo "cannot find autoinstall user-data ${template}" >&2
        return 1
    fi
    if ! password_hash="$(openssl passwd -6 ubuntu)"; then
        echo "cannot hash the autoinstall password" >&2
        return 1
    fi
    source_id="$NESTED_SOURCE_ID"
    proxy="$(nested_guest_proxy)"
    if ! storage="$(nested_autoinstall_snippet)"; then
        return 1
    fi
    if ! live_snapd_setup="$(nested_live_snapd_setup)"; then
        return 1
    fi
    proxy_line=""
    if [ -n "$proxy" ]; then
        proxy_line="  proxy: \"${proxy}\"
"
    fi
    document="$(cat "$template")"
    for token in @SOURCE_ID@ @PASSWORD_HASH@ @LIVE_SNAPD_SETUP@ @PROXY_LINE@ @STORAGE@; do
        if [[ "$document" != *"$token"* ]]; then
            echo "cannot find ${token} in autoinstall user-data ${template}" >&2
            return 1
        fi
    done
    # Bash treats & in a replacement as the matched text. Escape it so a slot
    # value such as 2>&1 is copied unchanged. Inserted text is not scanned again.
    source_id=${source_id//&/\\&}
    password_hash=${password_hash//&/\\&}
    live_snapd_setup=${live_snapd_setup//&/\\&}
    proxy_line=${proxy_line//&/\\&}
    storage=${storage//&/\\&}
    document="${document/@SOURCE_ID@/${source_id}}"
    document="${document/@PASSWORD_HASH@/${password_hash}}"
    document="${document/@LIVE_SNAPD_SETUP@/${live_snapd_setup}}"
    document="${document/@PROXY_LINE@/${proxy_line}}"
    document="${document/@STORAGE@/${storage}}"
    printf '%s' "$document"
}

nested_create_autoinstall_seed() {
    local seed="${NESTED_ASSETS_DIR}/autoinstall-seed.img"
    local user_data="${NESTED_ASSETS_DIR}/autoinstall-user-data"
    local meta_data="${NESTED_ASSETS_DIR}/autoinstall-meta-data"
    local rendered
    # Subiquity waits for cloud-init before applying storage. The user-data
    # template restarts the live snapd with SNAPPY_TESTING=1 first: the secboot
    # preinstall check permits a virtual machine only in that mode.
    if ! rendered="$(nested_render_autoinstall_user_data)"; then
        return 1
    fi
    mkdir -p "$NESTED_ASSETS_DIR"
    printf '%s\n' "$rendered" > "$user_data"
    printf 'instance-id: nocloud\n' > "$meta_data"
    rm -f "$seed"
    if ! nested_write_autoinstall_seed_image "$seed" "$user_data" "$meta_data"; then
        return 1
    fi
    printf '%s' "$seed"
}

nested_remove_vm_unit() {
    if [ -e "/etc/systemd/system/${NESTED_VM}.service" ]; then
        tests.systemd stop-unit --remove "$NESTED_VM"
    fi
}

nested_installer_dump_logs() {
    echo "serial log:" >&2
    nested_print_serial_log >&2 || true
    if [ -f "${NESTED_LOGS_DIR}/installer-status.log" ]; then
        echo "installer status log:" >&2
        cat "${NESTED_LOGS_DIR}/installer-status.log" >&2 || true
    fi
    local log
    for log in "${NESTED_LOGS_DIR}/qemu.log" "${NESTED_LOGS_DIR}/qemu-install.log"; do
        if [ -f "$log" ]; then
            echo "QEMU log ${log}:" >&2
            cat "$log" >&2 || true
        fi
    done
    journalctl -u "$NESTED_VM" --no-pager -n 80 >&2 || true
}

nested_installer_fail() {
    echo "$1" >&2
    nested_installer_dump_logs
    return 1
}

nested_wait_for_qemu_exit() {
    local attempts="${1:-540}"
    local pause="${2:-10}"
    local i state serial_size last_progress size
    echo "Waiting for the desktop installer to power off"
    serial_size=0
    last_progress=0
    i=0
    while [ "$i" -lt "$attempts" ]; do
        state="$(systemctl show -p ActiveState --value "$NESTED_VM" 2>/dev/null || true)"
        if [ "$state" = "failed" ]; then
            echo "${NESTED_VM} failed while the desktop installer was running" >&2
            return 1
        fi
        if [ "$state" = "inactive" ]; then
            return 0
        fi
        if [ -f "${NESTED_LOGS_DIR}/installer-status.log" ]; then
            # Markers are whole lines on the status port. The seed contains
            # the commands that print them, so an unanchored match is not used.
            if grep -q -E '^(nested-autoinstall-failed|nested-autoinstall-snapd-testing-failed|nested-autoinstall-confirm-timeout|nested-autoinstall-snapd-reexec-failed)[[:space:]]*$' "${NESTED_LOGS_DIR}/installer-status.log"; then
                echo "desktop installer reported a failure" >&2
                return 1
            fi
        fi
        if [ -f "${NESTED_LOGS_DIR}/serial.log" ]; then
            size="$(stat -c %s "${NESTED_LOGS_DIR}/serial.log" 2>/dev/null || echo 0)"
            if [ "$size" != "$serial_size" ]; then
                serial_size="$size"
                last_progress=$i
            fi
        fi
        # 20 minutes with the default 10 second pause, from VM startup.
        # A missing serial log counts as no progress.
        if [ $((i - last_progress)) -ge 120 ]; then
            echo "desktop installer stopped reporting progress" >&2
            return 1
        fi
        i=$((i + 1))
        sleep "$pause"
    done
    echo "timed out waiting for ${NESTED_VM} to exit" >&2
    return 1
}

nested_restore_installer_vm_params() {
    local saved_keep="$1"
    local saved_image="$2"
    local saved_cd="$3"
    local saved_extra="$4"
    local saved_no_reexec="$5"

    NESTED_KEEP_FIRMWARE_STATE="$saved_keep"
    NESTED_PARAM_IMAGE="$saved_image"
    NESTED_PARAM_CD="$saved_cd"
    NESTED_PARAM_EXTRA="$saved_extra"
    NESTED_NO_REEXEC_ON_FAILURE="$saved_no_reexec"
}

nested_start_desktop_qemu() {
    local mode="$1"
    local disk="$2"
    local iso="${3:-}"
    local seed="${4:-}"
    local saved_keep="${NESTED_KEEP_FIRMWARE_STATE-}"
    local saved_image="${NESTED_PARAM_IMAGE-}"
    local saved_cd="${NESTED_PARAM_CD-}"
    local saved_extra="${NESTED_PARAM_EXTRA-}"
    local saved_no_reexec="${NESTED_NO_REEXEC_ON_FAILURE-}"
    local block_opts status

    block_opts="physical_block_size=${NESTED_DISK_PHYSICAL_BLOCK_SIZE},logical_block_size=${NESTED_DISK_LOGICAL_BLOCK_SIZE}"
    # Phase 1 must start from the enrolled template and a fresh TPM.
    # --keep-firmware-state applies only to the installed boot.
    if [ "$mode" = "install" ]; then
        NESTED_KEEP_FIRMWARE_STATE=
        # One virtio-scsi-pci device creates only bus scsi0.0. scsi0.1 makes
        # QEMU exit with "Bus 'scsi0.1' not found".
        NESTED_PARAM_CD="-device virtio-scsi-pci,id=scsi0 -drive if=none,file=${iso},readonly=on,media=cdrom,format=raw,id=installer-cd -device scsi-cd,bus=scsi0.0,drive=installer-cd,bootindex=1 -drive if=none,file=${seed},readonly=on,media=cdrom,format=raw,id=cidata-cd -device scsi-cd,bus=scsi0.0,drive=cidata-cd,bootindex=2"
        NESTED_PARAM_IMAGE="-drive file=${disk},if=none,format=raw,id=boot-disk -device virtio-blk-pci,drive=boot-disk,bootindex=3,write-cache=on,${block_opts}"
        # PARAM_EXTRA follows the console serial, so this port is ttyS1.
        if [ -n "$saved_extra" ]; then
            NESTED_PARAM_EXTRA="${saved_extra} -serial file:${NESTED_LOGS_DIR}/installer-status.log"
        else
            NESTED_PARAM_EXTRA="-serial file:${NESTED_LOGS_DIR}/installer-status.log"
        fi
    else
        NESTED_KEEP_FIRMWARE_STATE=1
        NESTED_PARAM_CD=
        NESTED_PARAM_IMAGE="-drive file=${disk},if=none,format=raw,id=boot-disk -device virtio-blk-pci,drive=boot-disk,bootindex=1,write-cache=on,${block_opts}"
    fi
    NESTED_NO_REEXEC_ON_FAILURE=1

    if nested_create_vm_service "$disk"; then
        status=0
    else
        status=$?
    fi
    nested_restore_installer_vm_params "$saved_keep" "$saved_image" "$saved_cd" "$saved_extra" "$saved_no_reexec"
    return "$status"
}

nested_boot_installed_desktop() {
    local disk="$1"
    if [ -f "${NESTED_LOGS_DIR}/qemu.log" ]; then
        cp -f "${NESTED_LOGS_DIR}/qemu.log" "${NESTED_LOGS_DIR}/qemu-install.log"
    fi
    nested_remove_vm_unit
    if ! nested_start_desktop_qemu installed "$disk"; then
        nested_installer_fail "cannot start the installed desktop image"
        return 1
    fi
}

nested_start_installer_vm() {
    if os.query is-arm; then
        echo "cannot boot the desktop installer on arm" >&2
        return 1
    fi
    if [ -n "${NESTED_PARAM_CD:-}" ]; then
        echo "tests.nested: installer image does not support --param-cdrom" >&2
        return 1
    fi

    local iso seed disk
    iso="$(nested_download_desktop_iso)"
    disk="$(nested_create_installer_disk)"
    seed="$(nested_create_autoinstall_seed)"

    nested_remove_vm_unit
    if ! nested_start_desktop_qemu install "$disk" "$iso" "$seed"; then
        nested_installer_fail "cannot start the desktop installer"
        return 1
    fi
    if ! nested_wait_for_qemu_exit 540 10; then
        nested_installer_fail "cannot finish the desktop installation"
        return 1
    fi
    if ! nested_boot_installed_desktop "$disk"; then
        return 1
    fi
    if ! nested_wait_for_ssh; then
        nested_installer_fail "cannot boot the installed desktop image"
        return 1
    fi
    nested_wait_for_snap_command 120 1
    nested_wait_for_snap_seeded
    nested_prepare_tools
    nested_setup_vm
}
