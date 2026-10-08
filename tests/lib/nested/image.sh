#!/bin/bash

# Image creation, naming, download, assertion disk, and cloud-init helpers.

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
        VERSION="$(nested_get_version)"
        # shellcheck disable=SC2001
        AUTO_IMPORT_ASSERT="$(echo "$NESTED_CUSTOM_AUTO_IMPORT_ASSERTION" | sed "s/{VERSION}/$VERSION/g")"
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

nested_prepare_env() {
    mkdir -p "$NESTED_IMAGES_DIR"
    mkdir -p "$NESTED_RUNTIME_DIR"
    mkdir -p "$NESTED_ASSETS_DIR"
    mkdir -p "$NESTED_LOGS_DIR"
    mkdir -p "$(nested_get_extra_snaps_path)"
}

nested_cleanup_env() {
    rm -rf "$NESTED_RUNTIME_DIR"
    rm -rf "$NESTED_ASSETS_DIR"
    rm -rf "$NESTED_LOGS_DIR"
    rm -rf "$NESTED_IMAGES_DIR"/*.img
    rm -rf "$(nested_get_extra_snaps_path)"
}

nested_get_image_channel() {
    echo "${NESTED_CORE_CHANNEL}"
}

nested_get_base_channel() {
    if nested_is_core_26_system; then
        # TODO: use $CORE_CHANNEL for the risk when core26 is released
        if [ "$NESTED_USE_CLOUD_INIT" = "true" ]; then
            echo "cloud-init/edge"
        else
            echo "edge"
        fi
    else
        echo "${CORE_CHANNEL:-edge}"
    fi
}

nested_get_kernel_channel() {
    echo "${NESTED_KERNEL_CHANNEL}"
}

nested_get_gadget_channel() {
    echo "${NESTED_GADGET_CHANNEL}"
}

nested_get_image_name_base() {
    local TYPE="$1"
    local SOURCE
    SOURCE="$(nested_get_image_channel)"
    local NAME="${NESTED_IMAGE_ID:-generic}"
    local VERSION

    VERSION="$(nested_get_version)"
    # Use task name to build the image in case the NESTED_IMAGE_ID is unset
    # This scenario is valid on manual tests when it is required to set the NESTED_IMAGE_ID
    if [ "$NAME" = "unset" ]; then
        NAME="$(basename "$SPREAD_TASK")"
        if [ -n "$SPREAD_VARIANT" ]; then
            NAME="${NAME}_${SPREAD_VARIANT}"
        fi
    fi

    if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
        SOURCE="custom"
    fi
    if [ "$(nested_get_extra_snaps | wc -l)" != "0" ]; then
        SOURCE="custom"
    fi
    echo "ubuntu-${TYPE}-${VERSION}-${SOURCE}-${NAME}"
}

nested_get_image_name() {
    local BASE_NAME
    BASE_NAME="$(nested_get_image_name_base "$1")"
    echo "${BASE_NAME}.img"
}

nested_is_generic_image() {
    test -z "${NESTED_IMAGE_ID:-}"
}

nested_get_extra_snaps_path() {
    echo "${NESTED_WORK_DIR}/extra-snaps"
}

nested_get_assets_path() {
    echo "$NESTED_ASSETS_DIR"
}

nested_get_images_path() {
    echo "$NESTED_IMAGES_DIR"
}

nested_get_extra_containers() {
    local SUFFIX=$1
    local EXTRA_SNAPS_PATH
    EXTRA_SNAPS_PATH="$(nested_get_extra_snaps_path)"

    if [ -d "$EXTRA_SNAPS_PATH" ]; then
        while IFS= read -r mysnap; do
            echo "$mysnap"
        done < <(find "$EXTRA_SNAPS_PATH" -name "*.$SUFFIX")
    fi
}

nested_get_extra_snaps() {
    nested_get_extra_containers snap
}

nested_get_extra_comps() {
    nested_get_extra_containers comp
}

nested_download_image() {
    local IMAGE_URL=$1
    local IMAGE_NAME=$2

    curl -C - -L -o "${NESTED_IMAGES_DIR}/${IMAGE_NAME}" "$IMAGE_URL"

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
        VERSION="$(nested_get_version)"
        # shellcheck disable=SC2001
        echo "$NESTED_CUSTOM_MODEL" | sed "s/{VERSION}/$VERSION/g"
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
        ubuntu-22.04-64)
            echo "$TESTSLIB/assertions/nested-22-amd64.model"
            ;;
        ubuntu-22.04-arm-64)
            echo "$TESTSLIB/assertions/nested-22-arm64.model"
            ;;
        ubuntu-24.04-64)
            echo "$TESTSLIB/assertions/nested-24-amd64.model"
            ;;
        ubuntu-24.04-arm-64)
            echo "$TESTSLIB/assertions/nested-24-arm64.model"
            ;;
        ubuntu-26.04-64)
            echo "$TESTSLIB/assertions/nested-26-amd64.model"
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

nested_configure_default_user() {
    local IMAGE_NAME
    local IMAGE_PATH
    
    IMAGE_NAME="$(nested_get_image_name core)"
    IMAGE_PATH="$(realpath "$NESTED_IMAGES_DIR/$IMAGE_NAME")"

    # Configure the user for the vm
    if [ "$NESTED_USE_CLOUD_INIT" = "true" ]; then
        if nested_is_core_ge 20; then
            nested_configure_cloud_init_on_core20_vm "$IMAGE_PATH"
        else
            nested_configure_cloud_init_on_core_vm "$IMAGE_PATH"
        fi
    else
        nested_create_assertions_disk
    fi

    # Save a copy of the primary image
    cp -v "$IMAGE_PATH" "$IMAGE_PATH.pristine"
}

nested_restore_core_image() {
    local IMAGE_PATH="$1"

    if [ ! -f "$IMAGE_PATH" ]; then
        return 1
    fi

    IMAGE_PATH="$(realpath "$IMAGE_PATH")"
    if [ ! -f "$IMAGE_PATH.pristine" ]; then
        return 1
    fi

    cp -v "$IMAGE_PATH.pristine" "$IMAGE_PATH"
    if [ ! "$NESTED_USE_CLOUD_INIT" = "true" ]; then
        nested_create_assertions_disk
    fi
}

nested_generate_core_image() {
    local IMAGE_NAME="$1"

    if [ "$NESTED_BUILD_SNAPD_FROM_CURRENT" = "true" ]; then
        nested_prepare_snapd
        nested_prepare_kernel
        nested_prepare_gadget
        nested_prepare_base
    fi

    local base_channel=""
    local image_channel
    local -a image_generator_args
    image_channel="$(nested_get_image_channel)"

    # Core 26 may need a base channel (for example cloud-init/edge) that
    # differs from the image-wide channel used by earlier Core versions.
    if nested_is_core_26_system; then
        base_channel="$(nested_get_base_channel)"
    fi
    image_generator_args=(
        --core-version "$(nested_get_version)"
        --model "$(nested_get_model)"
        --output-dir "$NESTED_IMAGES_DIR"
        --image-name "$IMAGE_NAME"
        --image-base-name "$(nested_get_image_name_base core)"
        --log-file "$NESTED_LOGS_DIR/ubuntu-image.log"
        --sector-size "$NESTED_DISK_LOGICAL_BLOCK_SIZE"
        --store-url "$NESTED_UBUNTU_IMAGE_SNAPPY_FORCE_SAS_URL"
        --debug
    )
    if [ -n "$base_channel" ]; then
        image_generator_args+=(--base-channel "$base_channel")
    fi
    if [ -n "$image_channel" ]; then
        image_generator_args+=(--channel "$image_channel")
    fi
    if [ -n "$NESTED_UBUNTU_IMAGE_PRESEED_KEY" ]; then
        image_generator_args+=(--preseed-sign-key "$NESTED_UBUNTU_IMAGE_PRESEED_KEY")
    fi
    while IFS= read -r mysnap; do
        image_generator_args+=(--snap "$mysnap")
    done < <(nested_get_extra_snaps)
    while IFS= read -r mycomp; do
        image_generator_args+=(--component "$mycomp")
    done < <(nested_get_extra_comps)
    if [ -n "$NESTED_KERNEL_MODULES_COMP" ] && [ "$(nested_get_version)" -ge "24" ]; then
        image_generator_args+=(--component "pc-kernel+${NESTED_KERNEL_MODULES_COMP}.comp")
    fi
    if nested_is_core_ge 20 && [ -e pc-gadget/meta/gadget.yaml ]; then
        image_generator_args+=(--gadget-yaml pc-gadget/meta/gadget.yaml)
    fi
    "$TESTSTOOLS"/image-generator "${image_generator_args[@]}"
}

nested_create_core_vm() {
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    # shellcheck source=tests/lib/snaps.sh
    . "$TESTSLIB"/snaps.sh

    local IMAGE_NAME IMAGE_PATH
    IMAGE_NAME="$(nested_get_image_name core)"
    IMAGE_PATH="$NESTED_IMAGES_DIR/$IMAGE_NAME"
    mkdir -p "$NESTED_IMAGES_DIR"

    if nested_restore_core_image "$IMAGE_PATH"; then
        return
    fi

    if [ ! -f "$IMAGE_PATH" ]; then
        if [ -n "$NESTED_CUSTOM_IMAGE_URL" ]; then
            nested_download_image "$NESTED_CUSTOM_IMAGE_URL" "$IMAGE_NAME"
        else
            nested_generate_core_image "$IMAGE_NAME"
        fi
    fi

    nested_configure_default_user
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
  datasource_list: [ NoCloud, None ]
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
datasource_list: [ None ]
users:
  - name: user1
    sudo: "ALL=(ALL) NOPASSWD:ALL"
    lock_passwd: false
    plain_text_passwd: "ubuntu"
EOF
}

nested_add_file_to_image() {
    local IMAGE=$1
    local FILE=$2
    local devloop ubuntuSeedDev tmp
    # Bind the image the a free loop device
    devloop="$(retry -n 3 --wait 1 losetup -f --show -P --sector-size "${NESTED_DISK_LOGICAL_BLOCK_SIZE}" "${IMAGE}")"

    # we add cloud-init data to the 2nd partition, which is ubuntu-seed
    ubuntuSeedDev="${devloop}p2"
    if os.query is-arm; then
        # In arm the BIOS partition does not exist, so ubuntu-seed is the p1
        ubuntuSeedDev="${devloop}p1"
    fi

    # Wait for the partition to show up
    retry -n 2 --wait 1 test -b "${ubuntuSeedDev}" || true

    # losetup does not set the right block size on LOOP_CONFIGURE
    # but with LOOP_SET_BLOCK_SIZE later. So the block size might have
    # been wrong during the part scan. In this case we need to rescan
    # manually.
    if ! [ -b "${ubuntuSeedDev}" ]; then
        partx -u "${devloop}"
        # Wait for the partition to show up
        retry -n 2 --wait 1 test -b "${ubuntuSeedDev}"
    fi

    tmp=$(mktemp -d)
    retry -n 5 --wait 2 mount "$ubuntuSeedDev" "$tmp"
    mkdir -p "$tmp/data/etc/cloud/cloud.cfg.d/"
    cp -f "$FILE" "$tmp/data/etc/cloud/cloud.cfg.d/"
    sync
    umount "$tmp"
    losetup -d "${devloop}"
}

nested_configure_cloud_init_on_core20_vm() {
    local IMAGE=$1
    nested_create_cloud_init_uc20_config "$NESTED_ASSETS_DIR/data.cfg"

    nested_add_file_to_image "$IMAGE" "$NESTED_ASSETS_DIR/data.cfg"
}

nested_create_classic_vm() {
    local IMAGE_NAME
    IMAGE_NAME="$(nested_get_image_name classic)"

    mkdir -p "$NESTED_IMAGES_DIR"
    if [ ! -f "$NESTED_IMAGES_DIR/$IMAGE_NAME" ]; then
        # shellcheck source=tests/lib/image.sh
        . "$TESTSLIB"/image.sh

        # Get the cloud image
        local IMAGE_URL
        IMAGE_URL="$(get_image_url_for_vm)"
        wget -q -P "$NESTED_IMAGES_DIR" "$IMAGE_URL"
        nested_download_image "$IMAGE_URL" "$IMAGE_NAME"

        # Prepare the cloud-init configuration and configure image
        nested_create_cloud_init_config "$NESTED_ASSETS_DIR/seed"
        cloud-localds -H "$(hostname)" "$NESTED_ASSETS_DIR/seed.img" "$NESTED_ASSETS_DIR/seed"
    fi

    # Save a copy of the image
    cp -v "$NESTED_IMAGES_DIR/$IMAGE_NAME" "$NESTED_IMAGES_DIR/$IMAGE_NAME.pristine"
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

