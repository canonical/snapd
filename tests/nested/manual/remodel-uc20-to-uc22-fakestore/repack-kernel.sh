#!/bin/bash

set -eu

version=$1
branch=$2

tmpd=$(mktemp -d)
cleanup() {
    rm -rf "${tmpd}"
}
trap cleanup EXIT

# For some reason network is not yet available
sleep 5

add-apt-repository ppa:snappy-dev/image -y
apt-get install -y golang ubuntu-core-initramfs

snap download pc-kernel --channel="${version}/${branch}" --basename=pc-kernel --target-directory="${tmpd}"
unsquashfs -d "${tmpd}/pc-kernel" "${tmpd}/pc-kernel.snap"

objcopy -O binary -j .initrd "${tmpd}/pc-kernel/kernel.efi" "${tmpd}/initrd"
objcopy -O binary -j .linux "${tmpd}/pc-kernel/kernel.efi" "${tmpd}/linux"
objcopy -O binary -j .uname "${tmpd}/pc-kernel/kernel.efi" "${tmpd}/kver"

mkdir "${tmpd}/early"
mkdir "${tmpd}/main"
( (cd "${tmpd}/early"; cpio -id) ; (cd "${tmpd}/main"; zstdcat | cpio -id) ) <"${tmpd}/initrd"

if [ "${BUILD_FDE_HOOK-}" = 1 ]; then
    go build -o "${tmpd}/main/usr/bin/fde-reveal-key" /project/tests/lib/fde-setup-hook
fi

go build -tags 'nomanagers withtestkeys faultinject' -o "${tmpd}/main/usr/lib/snapd/snap-bootstrap" /project/cmd/snap-bootstrap

(cd "${tmpd}/early"; find . | cpio --create --quiet --format=newc --owner=0:0) >"${tmpd}/new-initrd"
(cd "${tmpd}/main"; find . | cpio --create --quiet --format=newc --owner=0:0 | zstd -1 -T0) >>"${tmpd}/new-initrd"

ubuntu-core-initramfs create-efi \
                      --kernelver "" \
                      --initrd "${tmpd}/new-initrd" \
                      --kernel "${tmpd}/linux" \
                      --key "${SNAKEOIL_KEY}" \
                      --cert "${SNAKEOIL_CERT}" \
                      --output "${tmpd}/pc-kernel/kernel.efi"


if [ "${BUILD_FDE_HOOK-}" = 1 ]; then
    go build -o "${tmpd}/pc-kernel/meta/hooks/fde-setup" /project/tests/lib/fde-setup-hook
fi

snap pack "${tmpd}/pc-kernel" --filename="pc-kernel-modified.snap"
