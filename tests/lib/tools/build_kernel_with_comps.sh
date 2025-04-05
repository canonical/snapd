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
    kernel_snap_file=${3:-}

    use_provided_kernel=true
    if [ -z "${kernel_snap_file}" ]; then
        use_provided_kernel=false
    fi

    if [ "${use_provided_kernel}" = false ]; then
        nested_prepare_kernel
        cp "$(tests.nested get extra-snaps-path)/pc-kernel.snap" "pc-kernel.snap"
        kernel_snap_file="pc-kernel.snap"
    fi

    unsquashfs -d kernel "${kernel_snap_file}"
    kernel_name="$(grep 'name:' kernel/meta/snap.yaml | awk '{ print $2 }')"
    kern_ver=$(find kernel/modules/* -maxdepth 0 -printf "%f\n")
    comp_ko_dir=$comp_name/modules/"$kern_ver"/kmod/
    mkdir -p "$comp_ko_dir"
    mkdir -p "$comp_name"/meta/
    cat << EOF > "$comp_name"/meta/component.yaml
component: ${kernel_name}+${comp_name}
type: kernel-modules
version: 1.0
summary: kernel component
description: kernel component for testing purposes
EOF
    # Replace _ or - with [_-], as it can be any of these
    glob_mod_name=$(printf '%s' "$mod_name" | sed -r 's/[-_]/[-_]/g')
    module_path=$(find kernel -name "${glob_mod_name}.ko*")
    cp "$module_path" "$comp_ko_dir"
    snap pack --filename="${kernel_name}+${comp_name}".comp "$comp_name"

    # Create kernel without the kernel module
    rm "$module_path"
    # depmod wants a lib subdir
    mkdir -p kernel/lib
    ln -s ../modules kernel/lib/modules
    depmod -b kernel/ "$kern_ver"
    rm "${kernel_snap_file}"
    # append component meta-information
    #shellcheck disable=SC2016
    gojq --arg COMP_NAME "${comp_name}" '.components = {$COMP_NAME:{"type":"kernel-modules"}}' --yaml-input kernel/meta/snap.yaml --yaml-output >kernel/meta/snap.yaml.new
    mv kernel/meta/snap.yaml.new kernel/meta/snap.yaml
    snap pack --filename="${kernel_snap_file}" kernel

    if [ "${use_provided_kernel}" = false ]; then
        # Just so that nested_prepare_kernel does not recopy the old one
        cp "${kernel_snap_file}" "${NESTED_ASSETS_DIR}/pc-kernel.snap"
    fi

    rm -r kernel
}

build_kernel_with_comp "$@"
