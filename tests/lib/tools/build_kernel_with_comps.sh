#!/bin/bash

set -uxe

# shellcheck source=tests/lib/prepare.sh
. "$TESTSLIB/prepare.sh"
#shellcheck source=tests/lib/nested.sh
. "$TESTSLIB"/nested.sh

# Modify kernel and create a component
build_kernel_with_comp() {
    mod_name=$1
    comp_name=$2
    kernel_snap_file=$3

    if [ -z "${kernel_snap_file}" ]; then
        VERSION="$(tests.nested show version)"
        snap download --channel="$VERSION"/beta --basename=pc-kernel pc-kernel
        kernel_snap_file="pc-kernel.snap"
    fi
    unsquashfs -d kernel "${kernel_snap_file}"
    kern_ver=$(find kernel/modules/* -maxdepth 0 -printf "%f\n")
    comp_ko_dir=$comp_name/modules/"$kern_ver"/kmod/
    mkdir -p "$comp_ko_dir"
    mkdir -p "$comp_name"/meta/
    cat << EOF > "$comp_name"/meta/component.yaml
component: pc-kernel+$comp_name
type: kernel-modules
version: 1.0
summary: kernel component
description: kernel component for testing purposes
EOF
    # Replace _ or - with [_-], as it can be any of these
    glob_mod_name=$(printf '%s' "$mod_name" | sed -r 's/[-_]/[-_]/g')
    module_path=$(find kernel -name "${glob_mod_name}.ko*")
    cp "$module_path" "$comp_ko_dir"
    snap pack --filename=pc-kernel+"$comp_name".comp "$comp_name"

    # Create kernel without the kernel module
    rm "$module_path"
    # depmod wants a lib subdir, fake it and remove after invocation
    mkdir kernel/lib
    ln -s ../modules kernel/lib/modules
    depmod -b kernel/ "$kern_ver"
    rm -rf kernel/lib
    rm "${kernel_snap_file}"
    # append component meta-information
    printf 'components:\n  %s:\n    type: kernel-modules\n' "$comp_name" >> kernel/meta/snap.yaml
    snap pack --filename="${kernel_snap_file}" kernel
}

build_kernel_with_comp "$@"
