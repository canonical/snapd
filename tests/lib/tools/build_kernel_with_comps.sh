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

    VERSION="$(tests.nested show version)"
    snap download --channel="$VERSION"/beta --basename=pc-kernel pc-kernel
    unsquashfs -d kernel pc-kernel.snap
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
    rm pc-kernel.snap
    # append component meta-information
    printf 'components:\n  %s:\n    type: kernel-modules\n' "$comp_name" >> kernel/meta/snap.yaml
    snap pack --filename=pc-kernel.snap kernel
}

build_kernel_with_comp "$@"
