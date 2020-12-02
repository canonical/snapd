#!/bin/bash

# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB"/systemd.sh

# shellcheck source=tests/lib/store.sh
. "$TESTSLIB"/store.sh

NESTED_WORK_DIR="${NESTED_WORK_DIR:-/tmp/work-dir}"
NESTED_IMAGES_DIR="$NESTED_WORK_DIR/images"
NESTED_RUNTIME_DIR="$NESTED_WORK_DIR/runtime"
NESTED_ASSETS_DIR="$NESTED_WORK_DIR/assets"
NESTED_LOGS_DIR="$NESTED_WORK_DIR/logs"


NESTED_VM=nested-vm
NESTED_SSH_PORT=8022
NESTED_MON_PORT=8888

NESTED_CUSTOM_MODEL="${NESTED_CUSTOM_MODEL:-}"
NESTED_CUSTOM_AUTO_IMPORT_ASSERTION="${NESTED_CUSTOM_AUTO_IMPORT_ASSERTION:-}"
NESTED_FAKESTORE_BLOB_DIR="${NESTED_FAKESTORE_BLOB_DIR:-$NESTED_WORK_DIR/fakestore/blobs}"
NESTED_SIGN_SNAPS_FAKESTORE="${NESTED_SIGN_SNAPS_FAKESTORE:-false}"
NESTED_UBUNTU_IMAGE_SNAPPY_FORCE_SAS_URL="${NESTED_UBUNTU_IMAGE_SNAPPY_FORCE_SAS_URL:-}"

nested_wait_for_ssh() {
    # TODO:UC20: the retry count should be lowered to something more reasonable.
    nested_retry_until_success 800 1 "true"
}

nested_wait_for_no_ssh() {
    nested_retry_while_success 200 1 "true"
}

nested_wait_for_snap_command() {
    nested_retry_until_success 200 1 command -v snap
}

nested_get_boot_id() {
    nested_exec "cat /proc/sys/kernel/random/boot_id"
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
    local current_boot_id
    current_boot_id=$(nested_get_boot_id)
    nested_exec "sudo snap reboot --$mode $recovery_system"
    nested_wait_for_reboot "$current_boot_id"

    # verify we are now in the requested mode
    if ! nested_exec "cat /proc/cmdline" | MATCH "snapd_recovery_mode=$mode"; then
        return 1
    fi
}

nested_retry_while_success() {
    local retry="$1"
    local wait="$2"
    shift 2

    while nested_exec "$@"; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for command '$*' to fail. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}

nested_retry_until_success() {
    local retry="$1"
    local wait="$2"
    shift 2

    until nested_exec "$@"; do
        retry=$(( retry - 1 ))
        if [ $retry -le 0 ]; then
            echo "Timed out waiting for command '$*' to succeed. Aborting!"
            return 1
        fi
        sleep "$wait"
    done
}

nested_prepare_ssh() {
    nested_exec "sudo adduser --uid 12345 --extrausers --quiet --disabled-password --gecos '' test"
    nested_exec "echo test:ubuntu | sudo chpasswd"
    nested_exec "echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test"
    # Check we can connect with the new test user and make sudo
    nested_exec_as test ubuntu "sudo true"

    nested_exec "sudo adduser --extrausers --quiet --disabled-password --gecos '' external"
    nested_exec "echo external:ubuntu | sudo chpasswd"
    nested_exec "echo 'external ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-external"
    # Check we can connect with the new external user and make sudo
    nested_exec_as external ubuntu "sudo true"
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
        AUTO_IMPORT_ASSERT=$NESTED_CUSTOM_AUTO_IMPORT_ASSERTION
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
    case "${NESTED_ARCHITECTURE:-amd64}" in
    amd64)
        command -v qemu-system-x86_64
        ;;
    i386)
        command -v qemu-system-i386
        ;;
    *)
        echo "unsupported architecture"
        exit 1
        ;;
    esac
}

# shellcheck disable=SC2120
nested_get_google_image_url_for_vm() {
    case "${1:-$SPREAD_SYSTEM}" in
        ubuntu-16.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.10-64*)
            echo "https://storage.googleapis.com/spread-snapd-tests/images/cloudimg/groovy-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

# shellcheck disable=SC2120
nested_get_ubuntu_image_url_for_vm() {
    case "${1:-$SPREAD_SYSTEM}" in
        ubuntu-16.04-64*)
            echo "https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img"
            ;;
        ubuntu-18.04-64*)
            echo "https://cloud-images.ubuntu.com/bionic/current/bionic-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.04-64*)
            echo "https://cloud-images.ubuntu.com/focal/current/focal-server-cloudimg-amd64.img"
            ;;
        ubuntu-20.10-64*)
            echo "https://cloud-images.ubuntu.com/groovy/current/groovy-server-cloudimg-amd64.img"
            ;;
        *)
            echo "unsupported system"
            exit 1
            ;;
        esac
}

# shellcheck disable=SC2120
nested_get_image_url_for_vm() {
    if [[ "$SPREAD_BACKEND" == google* ]]; then
        nested_get_google_image_url_for_vm "$@"
    else
        nested_get_ubuntu_image_url_for_vm "$@"
    fi
}

nested_get_cdimage_current_image_url() {
    local VERSION=$1
    local CHANNEL=$2
    local ARCH=$3

    echo "http://cdimage.ubuntu.com/ubuntu-core/$VERSION/$CHANNEL/current/ubuntu-core-$VERSION-$ARCH.img.xz"
}

nested_get_snap_rev_for_channel() {
    local SNAP=$1
    local CHANNEL=$2

    curl -s \
         -H "Snap-Device-Architecture: ${NESTED_ARCHITECTURE:-amd64}" \
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
            nested_exec "sudo snap refresh core --${NEW_CHANNEL}"
            nested_exec "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi

        if nested_is_core_18_system || nested_is_core_20_system; then
            nested_exec "sudo snap refresh snapd --${NEW_CHANNEL}"
            nested_exec "snap info snapd" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        else
            CHANGE_ID=$(nested_exec "sudo snap refresh core --${NEW_CHANNEL} --no-wait")
            nested_wait_for_no_ssh
            nested_wait_for_ssh
            # wait for the refresh to be done before checking, if we check too
            # quickly then operations on the core snap like reverting, etc. may
            # fail because it will have refresh-snap change in progress
            nested_exec "snap watch $CHANGE_ID"
            nested_exec "snap info core" | grep -E "^tracking: +latest/${NEW_CHANNEL}"
        fi
    fi
}

nested_get_snakeoil_key() {
    local KEYNAME="PkKek-1-snakeoil"
    wget https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.key
    wget https://raw.githubusercontent.com/snapcore/pc-amd64-gadget/20/snakeoil/$KEYNAME.pem
    echo "$KEYNAME"
}

nested_secboot_sign_file() {
    local FILE="$1"
    local KEY="$2"
    local CERT="$3"
    sbattach --remove "$FILE"
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
    local VERSION="16"

    if nested_is_core_20_system; then
        VERSION="20"
    elif nested_is_core_18_system; then
        VERSION="18"
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
    echo "${PWD}/extra-snaps"
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

    curl -L -o "${NESTED_IMAGES_DIR}/${IMAGE_NAME}" "$IMAGE_URL"

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

nested_get_model() {
    # use custom model if defined
    if [ -n "$NESTED_CUSTOM_MODEL" ]; then
        echo "$NESTED_CUSTOM_MODEL"
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

nested_create_core_vm() {
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    # shellcheck source=tests/lib/snaps.sh
    . "$TESTSLIB"/snaps.sh

    local IMAGE_NAME
    IMAGE_NAME="$(nested_get_image_name core)"

    mkdir -p "$NESTED_IMAGES_DIR"

    if [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME".xz ]; then
        nested_uncompress_image "$IMAGE_NAME"
    elif [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then

        if [ -n "$NESTED_CUSTOM_IMAGE_URL" ]; then
            # download the ubuntu-core image from $CUSTOM_IMAGE_URL
            nested_download_image "$NESTED_CUSTOM_IMAGE_URL" "$IMAGE_NAME"
        else
            # create the ubuntu-core image
            local UBUNTU_IMAGE=/snap/bin/ubuntu-image
            local EXTRA_FUNDAMENTAL=""
            local EXTRA_SNAPS=""
            for mysnap in $(nested_get_extra_snaps); do
                EXTRA_SNAPS="$EXTRA_SNAPS --snap $mysnap"
            done

            if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
                if nested_is_core_16_system; then
                    repack_snapd_deb_into_core_snap "$NESTED_ASSETS_DIR"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $NESTED_ASSETS_DIR/core-from-snapd-deb.snap"

                elif nested_is_core_18_system; then
                    repack_snapd_deb_into_snapd_snap "$NESTED_ASSETS_DIR"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $NESTED_ASSETS_DIR/snapd-from-deb.snap"

                    snap download --channel="$CORE_CHANNEL" --basename=core18 core18
                    repack_core_snap_with_tweaks "core18.snap" "new-core18.snap"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $PWD/new-core18.snap"

                    repack_core_snap_with_tweaks "core18.snap" "new-core18.snap"

                    if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                        make_snap_installable_with_id "$NESTED_FAKESTORE_BLOB_DIR" "$PWD/new-core18.snap" "CSO04Jhav2yK0uz97cr0ipQRyqg0qQL6"
                    fi

                elif nested_is_core_20_system; then
                    snap download --basename=pc-kernel --channel="20/edge" pc-kernel
                    uc20_build_initramfs_kernel_snap "$PWD/pc-kernel.snap" "$NESTED_ASSETS_DIR"
                    rm -f "$PWD/pc-kernel.snap"

                    # Prepare the pc kernel snap
                    KERNEL_SNAP=$(ls "$NESTED_ASSETS_DIR"/pc-kernel_*.snap)

                    chmod 0600 "$KERNEL_SNAP"
                    EXTRA_FUNDAMENTAL="--snap $KERNEL_SNAP"

                    # sign the pc-kernel snap with fakestore if requested
                    if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                        make_snap_installable_with_id "$NESTED_FAKESTORE_BLOB_DIR" "$KERNEL_SNAP" "pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza"
                    fi

                    # Prepare the pc gadget snap (unless provided by extra-snaps)
                    local GADGET_SNAP
                    GADGET_SNAP=""
                    if [ -d "$(nested_get_extra_snaps_path)" ]; then
                        GADGET_SNAP=$(find extra-snaps -name 'pc_*.snap')
                    fi
                    # XXX: deal with [ "$NESTED_ENABLE_SECURE_BOOT" != "true" ] && [ "$NESTED_ENABLE_TPM" != "true" ]
                    if [ -z "$GADGET_SNAP" ]; then
                        # Get the snakeoil key and cert
                        local KEY_NAME SNAKEOIL_KEY SNAKEOIL_CERT
                        KEY_NAME=$(nested_get_snakeoil_key)
                        SNAKEOIL_KEY="$PWD/$KEY_NAME.key"
                        SNAKEOIL_CERT="$PWD/$KEY_NAME.pem"

                        snap download --basename=pc --channel="20/edge" pc
                        unsquashfs -d pc-gadget pc.snap
                        nested_secboot_sign_gadget pc-gadget "$SNAKEOIL_KEY" "$SNAKEOIL_CERT"
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
                        snap pack pc-gadget/ "$NESTED_ASSETS_DIR"

                        GADGET_SNAP=$(ls "$NESTED_ASSETS_DIR"/pc_*.snap)
                        rm -f "$PWD/pc.snap" "$SNAKEOIL_KEY" "$SNAKEOIL_CERT"
                        EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $GADGET_SNAP"
                    fi
                    # sign the pc gadget snap with fakestore if requested
                    if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                        make_snap_installable_with_id "$NESTED_FAKESTORE_BLOB_DIR" "$GADGET_SNAP" "UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH"
                    fi

                    # repack the snapd snap
                    snap download --channel="latest/edge" snapd
                    repack_snapd_deb_into_snapd_snap "$PWD"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $PWD/snapd-from-deb.snap"

                    # sign the snapd snap with fakestore if requested
                    if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                        make_snap_installable_with_id "$NESTED_FAKESTORE_BLOB_DIR" "$PWD/snapd-from-deb.snap" "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4"
                    fi

                    # which channel?
                    snap download --channel="$CORE_CHANNEL" --basename=core20 core20
                    repack_core_snap_with_tweaks "core20.snap" "new-core20.snap"
                    EXTRA_FUNDAMENTAL="$EXTRA_FUNDAMENTAL --snap $PWD/new-core20.snap"

                    # sign the snapd snap with fakestore if requested
                    if [ "$NESTED_SIGN_SNAPS_FAKESTORE" = "true" ]; then
                        make_snap_installable_with_id "$NESTED_FAKESTORE_BLOB_DIR" "$PWD/new-core20.snap" "DLqre5XGLbDqg9jPtiAhRRjDuPVa5X1q"
                    fi

                else
                    echo "unknown nested core system (host is $(lsb_release -cs) )"
                    exit 1
                fi
            fi

            # Invoke ubuntu image
            local NESTED_MODEL
            NESTED_MODEL="$(nested_get_model)"
            
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
            "$UBUNTU_IMAGE" snap --image-size 10G "$NESTED_MODEL" \
                "$UBUNTU_IMAGE_CHANNEL_ARG" \
                --output "$NESTED_IMAGES_DIR/$IMAGE_NAME" \
                "$EXTRA_FUNDAMENTAL" \
                "$EXTRA_SNAPS"
            unset SNAPPY_FORCE_SAS_URL
            unset UBUNTU_IMAGE_SNAP_CMD
        fi
    fi

    # Configure the user for the vm
    if [ "$NESTED_USE_CLOUD_INIT" = "true" ]; then
        if nested_is_core_20_system; then
            nested_configure_cloud_init_on_core20_vm "$NESTED_IMAGES_DIR/$IMAGE_NAME"
        else
            nested_configure_cloud_init_on_core_vm "$NESTED_IMAGES_DIR/$IMAGE_NAME"
        fi
    else
        nested_create_assertions_disk
    fi

    # Save a compressed copy of the image
    # TODO: analyze if it is better to compress just when the image is generic
    nested_compress_image "$IMAGE_NAME"
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
    # passwd is just "ubuntu"
    passwd: "$6$rounds=4096$PCrfo.ggdf4ubP$REjyaoY2tUWH2vjFJjvLs3rDxVTszGR9P7mhH9sHb2MsELfc53uV/v15jDDOJU/9WInfjjTKJPlD5URhX5Mix0"
EOF
}

nested_configure_cloud_init_on_core20_vm() {
    local IMAGE=$1
    nested_create_cloud_init_uc20_config "$NESTED_ASSETS_DIR/data.cfg"

    local devloop dev ubuntuSeedDev tmp
    # mount the image and find the loop device /dev/loop that is created for it
    kpartx -avs "$IMAGE"
    devloop=$(losetup --list --noheadings | grep "$IMAGE" | awk '{print $1}')
    dev=$(basename "$devloop")
    
    # we add cloud-init data to the 2nd partition, which is ubuntu-seed
    ubuntuSeedDev="/dev/mapper/${dev}p2"
    
    # wait for the loop device to show up
    retry -n 3 --wait 1 test -e "$ubuntuSeedDev"
    tmp=$(mktemp -d)
    mount "$ubuntuSeedDev" "$tmp"
    mkdir -p "$tmp/data/etc/cloud/cloud.cfg.d/"
    cp -f "$NESTED_ASSETS_DIR/data.cfg" "$tmp/data/etc/cloud/cloud.cfg.d/"
    sync
    umount "$tmp"
    kpartx -d "$IMAGE"
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
    systemctl stop nested-vm
}

nested_force_start_vm() {
    # if the nested-vm is using a swtpm, we need to wait until the file exists
    # because the file disappears temporarily after qemu exits
    if systemctl show nested-vm -p ExecStart | grep -q swtpm-mvo; then
        retry -n 10 --wait 1 test -S /var/snap/swtpm-mvo/current/swtpm-sock
    fi
    systemctl start nested-vm
}

nested_start_core_vm_unit() {
    local QEMU CURRENT_IMAGE
    CURRENT_IMAGE=$1
    QEMU=$(nested_qemu_name)

    # Now qemu parameters are defined

    # use only 2G of RAM for qemu-nested
    # the caller can override PARAM_MEM
    local PARAM_MEM PARAM_SMP
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MEM="${NESTED_PARAM_MEM:--m 4096}"
        PARAM_SMP="-smp 2"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MEM="${NESTED_PARAM_MEM:--m 2048}"
        PARAM_SMP="-smp 1"
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi

    local PARAM_DISPLAY PARAM_NETWORK PARAM_MONITOR PARAM_USB PARAM_CD PARAM_RANDOM PARAM_CPU PARAM_TRACE PARAM_LOG PARAM_SERIAL
    PARAM_DISPLAY="-nographic"
    PARAM_NETWORK="-net nic,model=virtio -net user,hostfwd=tcp::$NESTED_SSH_PORT-:22"
    PARAM_MONITOR="-monitor tcp:127.0.0.1:$NESTED_MON_PORT,server,nowait"
    PARAM_USB="-usb"
    PARAM_CD="${NESTED_PARAM_CD:-}"
    PARAM_RANDOM="-object rng-random,id=rng0,filename=/dev/urandom -device virtio-rng-pci,rng=rng0"
    PARAM_CPU=""
    PARAM_TRACE="-d cpu_reset"
    PARAM_LOG="-D $NESTED_LOGS_DIR/qemu.log"
    # Open port 7777 on the host so that failures in the nested VM (e.g. to
    # create users) can be debugged interactively via
    # "telnet localhost 7777". Also keeps the logs
    #
    # XXX: should serial just be logged to stdout so that we just need
    #      to "journalctl -u nested-vm" to see what is going on ?
    if "$QEMU" -version | grep '2\.5'; then
        # XXX: remove once we no longer support xenial hosts
        PARAM_SERIAL="-serial file:${NESTED_LOGS_DIR}/serial.log"
    else
        PARAM_SERIAL="-chardev socket,telnet,host=localhost,server,port=7777,nowait,id=char0,logfile=${NESTED_LOGS_DIR}/serial.log,logappend=on -serial chardev:char0"
    fi

    # save logs from previous runs
    nested_save_serial_log

    # Set kvm attribute
    local ATTR_KVM
    ATTR_KVM=""
    if [ "$NESTED_ENABLE_KVM" = "true" ]; then
        ATTR_KVM=",accel=kvm"
        # CPU can be defined just when kvm is enabled
        PARAM_CPU="-cpu host"
    fi

    local PARAM_MACHINE
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MACHINE="-machine ubuntu${ATTR_KVM}"
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
    if nested_is_core_20_system; then
        # use a bundle EFI bios by default
        PARAM_BIOS="-bios /usr/share/ovmf/OVMF.fd"
        local OVMF_CODE OVMF_VARS
        OVMF_CODE="secboot"
        OVMF_VARS="ms"
        # In this case the kernel.efi is unsigned and signed with snaleoil certs
        if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
            OVMF_VARS="snakeoil"
        fi

        if [ "$NESTED_ENABLE_OVMF" = "true" ]; then
            PARAM_BIOS="-bios /usr/share/OVMF/OVMF_CODE.fd"
        fi
        
        if [ "$NESTED_ENABLE_SECURE_BOOT" = "true" ]; then
            cp -f "/usr/share/OVMF/OVMF_VARS.$OVMF_VARS.fd" "$NESTED_ASSETS_DIR/OVMF_VARS.$OVMF_VARS.fd"
            PARAM_BIOS="-drive file=/usr/share/OVMF/OVMF_CODE.$OVMF_CODE.fd,if=pflash,format=raw,unit=0,readonly -drive file=$NESTED_ASSETS_DIR/OVMF_VARS.$OVMF_VARS.fd,if=pflash,format=raw"
            PARAM_MACHINE="-machine q35${ATTR_KVM} -global ICH9-LPC.disable_s3=1"
        fi

        if [ "$NESTED_ENABLE_TPM" = "true" ]; then
            if snap list swtpm-mvo; then
                # reset the tpm state
                rm /var/snap/swtpm-mvo/current/tpm2-00.permall
                snap restart swtpm-mvo
            else
                snap install swtpm-mvo --beta
            fi
            # wait for the tpm sock file to exist
            retry -n 10 --wait 1 test -S /var/snap/swtpm-mvo/current/swtpm-sock
            PARAM_TPM="-chardev socket,id=chrtpm,path=/var/snap/swtpm-mvo/current/swtpm-sock -tpmdev emulator,id=tpm0,chardev=chrtpm -device tpm-tis,tpmdev=tpm0"
        fi
        PARAM_IMAGE="-drive file=$CURRENT_IMAGE,cache=none,format=raw,id=disk1,if=none -device virtio-blk-pci,drive=disk1,bootindex=1"
    else
        PARAM_IMAGE="-drive file=$CURRENT_IMAGE,cache=none,format=raw"
    fi

    # ensure we have a log dir
    mkdir -p "$NESTED_LOGS_DIR"
    # make sure we start with clean log file
    echo > "${NESTED_LOGS_DIR}/serial.log"
    # Systemd unit is created, it is important to respect the qemu parameters order
    systemd_create_and_start_unit "$NESTED_VM" "${QEMU} \
        ${PARAM_SMP} \
        ${PARAM_CPU} \
        ${PARAM_MEM} \
        ${PARAM_TRACE} \
        ${PARAM_LOG} \
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
        ${PARAM_CD} "

    # wait for the nested-vm service to appear active
    wait_for_service "$NESTED_VM"

    # Wait until ssh is ready
    nested_wait_for_ssh
    # Wait for the snap command to be available
    nested_wait_for_snap_command
    # Wait for snap seeding to be done
    nested_exec "sudo snap wait system seed.loaded"
    # Wait for cloud init to be done
    nested_exec "cloud-init status --wait"
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
        if ! [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME.xz" ] && ! [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
            echo "No image found to be started"
            exit 1
        fi

        # First time the image is used $IMAGE_NAME exists so it is used, otherwise
        # the saved image from previous run is uncompressed
        if ! [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
            nested_uncompress_image "$IMAGE_NAME"
        fi
        mv "$NESTED_IMAGES_DIR/$IMAGE_NAME" "$CURRENT_IMAGE"

        # Start the nested core vm
        nested_start_core_vm_unit "$CURRENT_IMAGE"

        if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME.xz.configured" ]; then
            # configure ssh for first time
            nested_prepare_ssh
            sync

            # compress the current image if it is a generic image
            if nested_is_generic_image && [ "$NESTED_CONFIGURE_IMAGES" = "true" ]; then
                # Stop the current image and compress it
                nested_shutdown
                nested_compress_image "$CURRENT_NAME"

                # Save the image with the name of the original image
                mv "${CURRENT_IMAGE}.xz" "$NESTED_IMAGES_DIR/$IMAGE_NAME.xz"
                touch "$NESTED_IMAGES_DIR/$IMAGE_NAME.xz.configured"

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
    nested_exec "sync"
    nested_exec "sudo shutdown now" || true
    nested_wait_for_no_ssh
    nested_force_stop_vm
    wait_for_service "$NESTED_VM" inactive
    sync
}

nested_start() {
    nested_save_serial_log
    nested_force_start_vm
    wait_for_service "$NESTED_VM" active
    nested_wait_for_ssh
}

nested_compress_image() {
    local IMAGE_NAME=$1
    if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME".xz ]; then
        xz -k0 "$NESTED_IMAGES_DIR/$IMAGE_NAME"
    fi
}

nested_uncompress_image() {
    local IMAGE_NAME=$1
    unxz -kf "$NESTED_IMAGES_DIR/$IMAGE_NAME".xz
}

nested_create_classic_vm() {
    local IMAGE_NAME
    IMAGE_NAME="$(nested_get_image_name classic)"

    mkdir -p "$NESTED_IMAGES_DIR"
    if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
        # Get the cloud image
        local IMAGE_URL
        IMAGE_URL="$(nested_get_image_url_for_vm)"
        wget -P "$NESTED_IMAGES_DIR" "$IMAGE_URL"
        nested_download_image "$IMAGE_URL" "$IMAGE_NAME"

        # Prepare the cloud-init configuration and configure image
        nested_create_cloud_init_config "$NESTED_ASSETS_DIR/seed"
        cloud-localds -H "$(hostname)" "$NESTED_ASSETS_DIR/seed.img" "$NESTED_ASSETS_DIR/seed"
    fi

    # Save a compressed copy of the image
    nested_compress_image "$IMAGE_NAME"
}

nested_start_classic_vm() {
    local IMAGE QEMU IMAGE_NAME
    QEMU="$(nested_qemu_name)"
    IMAGE_NAME="$(nested_get_image_name classic)"

    if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ] && [ -f "$NESTED_IMAGES_DIR/$IMAGE_NAME.xz" ]; then
        nested_uncompress_image "$IMAGE_NAME"
    fi

    # Now qemu parameters are defined
    local PARAM_SMP PARAM_MEM
    PARAM_SMP="-smp 1"
    # use only 2G of RAM for qemu-nested
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
        PARAM_MEM="${NESTED_PARAM_MEM:--m 4096}"
    elif [ "$SPREAD_BACKEND" = "qemu-nested" ]; then
        PARAM_MEM="${NESTED_PARAM_MEM:--m 2048}"
    else
        echo "unknown spread backend $SPREAD_BACKEND"
        exit 1
    fi
    local PARAM_DISPLAY PARAM_NETWORK PARAM_MONITOR PARAM_USB PARAM_CPU PARAM_CD PARAM_RANDOM PARAM_SNAPSHOT
    PARAM_DISPLAY="-nographic"
    PARAM_NETWORK="-net nic,model=virtio -net user,hostfwd=tcp::$NESTED_SSH_PORT-:22"
    PARAM_MONITOR="-monitor tcp:127.0.0.1:$NESTED_MON_PORT,server,nowait"
    PARAM_USB="-usb"
    PARAM_CPU=""
    PARAM_CD="${NESTED_PARAM_CD:-}"
    PARAM_RANDOM="-object rng-random,id=rng0,filename=/dev/urandom -device virtio-rng-pci,rng=rng0"
    PARAM_SNAPSHOT="-snapshot"

    local PARAM_MACHINE PARAM_IMAGE PARAM_SEED PARAM_SERIAL PARAM_BIOS PARAM_TPM
    if [ "$SPREAD_BACKEND" = "google-nested" ]; then
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

    PARAM_IMAGE="-drive file=$NESTED_IMAGES_DIR/$IMAGE_NAME,if=virtio"
    PARAM_SEED="-drive file=$NESTED_ASSETS_DIR/seed.img,if=virtio"
    # Open port 7777 on the host so that failures in the nested VM (e.g. to
    # create users) can be debugged interactively via
    # "telnet localhost 7777". Also keeps the logs
    #
    # XXX: should serial just be logged to stdout so that we just need
    #      to "journalctl -u nested-vm" to see what is going on ?
    if "$QEMU" -version | grep '2\.5'; then
        # XXX: remove once we no longer support xenial hosts
        PARAM_SERIAL="-serial file:${NESTED_LOGS_DIR}/serial.log"
    else
        PARAM_SERIAL="-chardev socket,telnet,host=localhost,server,port=7777,nowait,id=char0,logfile=${NESTED_LOGS_DIR}/serial.log,logappend=on -serial chardev:char0"
    fi
    PARAM_BIOS=""
    PARAM_TPM=""

    # ensure we have a log dir
    mkdir -p "$NESTED_LOGS_DIR"
    # save logs from previous runs
    nested_save_serial_log

    # Systemd unit is created, it is important to respect the qemu parameters order
    systemd_create_and_start_unit "$NESTED_VM" "${QEMU}  \
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
        ${PARAM_CD} "

    nested_wait_for_ssh
}

nested_destroy_vm() {
    systemd_stop_and_remove_unit "$NESTED_VM"

    local CURRENT_IMAGE
    CURRENT_IMAGE="$NESTED_IMAGES_DIR/$(nested_get_current_image_name)" 
    rm -f "$CURRENT_IMAGE"
}

nested_exec() {
    sshpass -p ubuntu ssh -p "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost "$@"
}

nested_exec_as() {
    local USER="$1"
    local PASSWD="$2"
    shift 2
    sshpass -p "$PASSWD" ssh -p "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$USER"@localhost "$@"
}

nested_copy() {
    sshpass -p ubuntu scp -P "$NESTED_SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no "$@" user1@localhost:~
}

nested_copy_from_remote() {
    sshpass -p ubuntu scp -P "$SSH_PORT" -o ConnectTimeout=10 -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no user1@localhost:"$1" "$2"
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
    nested_exec "snap info core" | awk "/${CHANNEL}: / {print(\$4)}" | sed -e 's/(\(.*\))/\1/'
}

nested_get_core_revision_installed() {
    nested_exec "snap info core" | awk "/installed: / {print(\$3)}" | sed -e 's/(\(.*\))/\1/'
}

nested_fetch_spread() {
    if [ ! -f "$NESTED_WORK_DIR/spread" ]; then
        mkdir -p "$NESTED_WORK_DIR"
        curl https://niemeyer.s3.amazonaws.com/spread-amd64.tar.gz | tar -xzv -C "$NESTED_WORK_DIR"
        # make sure spread really exists
        test -x "$NESTED_WORK_DIR/spread"
        echo "$NESTED_WORK_DIR/spread"
    fi
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
