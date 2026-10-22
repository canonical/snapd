#!/bin/bash

set -eu

snap_yaml="tests/lib/snaps/test-snapd-policy-app-consumer/meta/snap.yaml"
core_snap_yaml="tests/main/interfaces-many-snap-provided/test-snapd-policy-app-provider-core/meta/snap.yaml"
classic_snap_yaml="tests/main/interfaces-many-snap-provided/test-snapd-policy-app-provider-classic/meta/snap.yaml"

for iface in $(go run ./tests/lib/list-interfaces.go); do
    search="plugs: \\[ $iface \\]"
    case "$iface" in
        bool-file | gpio | pwm | dsp | netlink-driver | hidraw | i2c | iio | serial-port | spi | confdb)
            # skip gadget provided interfaces for now
            continue
            ;;
        cuda-driver-libs | egl-driver-libs | gbm-driver-libs | nvidia-video-driver-libs | opengl-driver-libs | opengles-driver-libs | vulkan-driver-libs)
            # skip interfaces with plug side only in rootfs for now
            continue
            ;;
        dbus | content)
            search="interface: $iface"
            ;;
        autopilot)
            search='plugs: \[ autopilot-introspection \]'
            ;;
    esac

    # check if a standalone test already exists and that it at least
    # connects and disconnects the interface
    dedicated_test=$(find tests/main/ -maxdepth 1 -name "interfaces-$iface")
    if [ -n "$dedicated_test" ]; then
        if grep -q "$search" "$snap_yaml"; then
            echo "Dedicated test '$dedicated_test' found for '$iface'." >&2
            echo "Please remove '$iface' from '$snap_yaml'." >&2
            exit 1
        fi
        # dedicated test already exists, skip high-level test check below
        continue
    fi

    # check if high-level minimal test exists for interface
    if ! grep -q "$search" "$snap_yaml"; then
        echo "Missing high-level test for interface '$iface'. Please add to:" >&2
        echo "* $snap_yaml" >&2
        echo "* $core_snap_yaml (if needed)" >&2
        echo "* $classic_snap_yaml (if needed)" >&2
        exit 1
    fi
done
