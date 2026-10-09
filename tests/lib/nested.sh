#!/bin/bash

: "${NESTED_WORK_DIR:=/var/tmp/work-dir}"
: "${NESTED_IMAGES_DIR:=${NESTED_WORK_DIR}/images}"
: "${NESTED_RUNTIME_DIR:=${NESTED_WORK_DIR}/runtime}"
: "${NESTED_ASSETS_DIR:=${NESTED_WORK_DIR}/assets}"
: "${NESTED_LOGS_DIR:=${NESTED_WORK_DIR}/logs}"
: "${NESTED_ARCHITECTURE:=amd64}"

: "${NESTED_VM:=nested-vm}"
: "${NESTED_SSH_PORT:=8022}"
: "${NESTED_MON_PORT:=8888}"

: "${NESTED_CUSTOM_MODEL:=}"
: "${NESTED_CUSTOM_AUTO_IMPORT_ASSERTION:=}"
: "${NESTED_FAKESTORE_BLOB_DIR:=${NESTED_WORK_DIR}/fakestore/blobs}"
: "${NESTED_SIGN_SNAPS_FAKESTORE:=false}"
: "${NESTED_REPACK_FOR_FAKESTORE:=false}"
: "${NESTED_FAKESTORE_SNAP_DECL_PC_GADGET:=}"
: "${NESTED_FAKESTORE_SNAP_DECL_PC_KERNEL:=}"
: "${NESTED_UBUNTU_IMAGE_SNAPPY_FORCE_SAS_URL:=}"
: "${NESTED_UBUNTU_IMAGE_PRESEED_KEY:=}"
: "${NESTED_UBUNTU_SEED_SIZE:=}"
: "${NESTED_KEEP_FIRMWARE_STATE:=}"
: "${NESTED_CUSTOM_FIRMWARE:=}"
: "${NESTED_ENABLE_ARM_TRUSTZONE:=false}"

: "${NESTED_DISK_PHYSICAL_BLOCK_SIZE:=512}"
: "${NESTED_DISK_LOGICAL_BLOCK_SIZE:=512}"


# shellcheck source=tests/lib/nested/system.sh
. "$TESTSLIB/nested/system.sh"
# shellcheck source=tests/lib/nested/wait.sh
. "$TESTSLIB/nested/wait.sh"
# shellcheck source=tests/lib/nested/image.sh
. "$TESTSLIB/nested/image.sh"
# shellcheck source=tests/lib/nested/snaps.sh
. "$TESTSLIB/nested/snaps.sh"
# shellcheck source=tests/lib/nested/qemu.sh
. "$TESTSLIB/nested/qemu.sh"
# shellcheck source=tests/lib/nested/lifecycle.sh
. "$TESTSLIB/nested/lifecycle.sh"
# shellcheck source=tests/lib/nested/setup.sh
. "$TESTSLIB/nested/setup.sh"
# shellcheck source=tests/lib/nested/secure-boot.sh
. "$TESTSLIB/nested/secure-boot.sh"
