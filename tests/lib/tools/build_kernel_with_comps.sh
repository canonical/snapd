#!/bin/bash

set -uxe

# Modify kernel and create a component
build_kernel_with_comp() {
    local module_name="$1"
    local component_name="$2"
    local kernel_snap_file="${3:-}"
    local kernel_name
    local nested_assets_dir
    local use_provided_kernel=true

    if [ -z "$kernel_snap_file" ]; then
        use_provided_kernel=false
        tests.nested prepare-kernel
        cp "$(tests.nested get extra-snaps-path)/pc-kernel.snap" pc-kernel.snap
        kernel_snap_file="pc-kernel.snap"
    fi

    kernel_name=$(unsquashfs -cat "$kernel_snap_file" meta/snap.yaml | awk '$1 == "name:" { print $2; exit }')
    "$TESTSTOOLS"/repack-kernel \
        --mode component \
        --orig-snap "$kernel_snap_file" \
        --output-snap "$kernel_snap_file" \
        --component-module "$module_name" \
        --component-name "$component_name" \
        --output-component "$kernel_name+$component_name.comp"

    if [ "$use_provided_kernel" = false ]; then
        nested_assets_dir=$(tests.nested get assets-path)
        cp "$kernel_snap_file" "$nested_assets_dir/pc-kernel.snap"
    fi
}

build_kernel_with_comp "$@"
