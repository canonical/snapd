#!/bin/bash
set -xeu

case "$1" in
prepare)
	# Skip some permutations of system and driver version.
	# This is done for three reasons explained below.

	# First, we list all the driver versions in task.yaml, even though many of them
	# are just non-existent on a given system - this is a limitation of the spread
	# variant system where variant cannot be excluded only for a given system Skip
	# permutations that are not installable on a given system.
	if [ "$(apt-cache show nvidia-driver-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}" | wc -l)" -eq 0 ]; then
		echo "No driver is available, expecting: skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-} to say: no-driver"
		test "$(cat "skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-}")" = "no-driver"
		exit 0
	fi

	# Second, some drivers are only transitional support packages that don't
	# actually ship any files.
	if apt-cache show nvidia-driver-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}" | grep -i transitional; then
		echo "Transitional driver is in use, expecting: skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-} to say: transitional-driver"
		test "$(cat "skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-}")" = "transitional-driver"
		exit 0
	fi

	# Third, some combinations are really buggy.
	case "$SPREAD_SYSTEM/$PACKAGE_VERSION/${PACKAGE_SUFFIX:-}" in
	ubuntu-18.04-64/515/* | ubuntu-18.04-64/390/* | ubuntu-2[02].04-64/390/*)
		# This fails with:
		# + exec /snap/test-snapd-nvidia/x1/bin/dlopen-tool.64
		# /var/lib/snapd/lib/gl/libEGL_nvidia.so.0
		# /var/lib/snapd/lib/gl/libEGL_nvidia.so.390.157
		# /var/lib/snapd/lib/gl/libGLESv1_CM_nvidia.so.1
		# /var/lib/snapd/lib/gl/libGLESv1_CM_nvidia.so.390.157
		# /var/lib/snapd/lib/gl/libGLESv2_nvidia.so.2
		# /var/lib/snapd/lib/gl/libGLESv2_nvidia.so.390.157
		# /var/lib/snapd/lib/gl/libGLX_nvidia.so.0
		# /var/lib/snapd/lib/gl/libGLX_nvidia.so.390.157
		# /var/lib/snapd/lib/gl/libcuda.so /var/lib/snapd/lib/gl/libcuda.so.1
		# /var/lib/snapd/lib/gl/libcuda.so.390.157
		# /var/lib/snapd/lib/gl/libnvcuvid.so
		# /var/lib/snapd/lib/gl/libnvcuvid.so.1
		# /var/lib/snapd/lib/gl/libnvcuvid.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-compiler.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-eglcore.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-encode.so
		# /var/lib/snapd/lib/gl/libnvidia-encode.so.1
		# /var/lib/snapd/lib/gl/libnvidia-encode.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-fatbinaryloader.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-fbc.so
		# /var/lib/snapd/lib/gl/libnvidia-fbc.so.1
		# /var/lib/snapd/lib/gl/libnvidia-fbc.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-glcore.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-glsi.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-ml.so
		# /var/lib/snapd/lib/gl/libnvidia-ml.so.1
		# /var/lib/snapd/lib/gl/libnvidia-ml.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-opencl.so.1
		# /var/lib/snapd/lib/gl/libnvidia-opencl.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-ptxjitcompiler.so
		# /var/lib/snapd/lib/gl/libnvidia-ptxjitcompiler.so.1
		# /var/lib/snapd/lib/gl/libnvidia-ptxjitcompiler.so.390.157
		# /var/lib/snapd/lib/gl/libnvidia-tls.so.390.157
		# *** stack smashing detected ***: terminated
		echo "Broken driver is in use, expecting: skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-} to say: broken-driver"
		test "$(cat "skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-}")" = "broken-driver"
		exit 0
		;;
	ubuntu-24.04-64/470/*)
		# This fails with:
		# + exec /snap/test-snapd-nvidia/2/bin/dlopen-tool.64
		# /var/lib/snapd/lib/gl/libEGL_nvidia.so.0
		# /var/lib/snapd/lib/gl/libEGL_nvidia.so.470.256.02
		# /var/lib/snapd/lib/gl/libGLESv1_CM_nvidia.so.1
		# /var/lib/snapd/lib/gl/libGLESv1_CM_nvidia.so.470.256.02
		# /var/lib/snapd/lib/gl/libGLESv2_nvidia.so.2
		# /var/lib/snapd/lib/gl/libGLESv2_nvidia.so.470.256.02
		# /var/lib/snapd/lib/gl/libGLX_nvidia.so.0
		# /var/lib/snapd/lib/gl/libGLX_nvidia.so.470.256.02
		# /var/lib/snapd/lib/gl/libcuda.so /var/lib/snapd/lib/gl/libcuda.so.1
		# /var/lib/snapd/lib/gl/libcuda.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvcuvid.so
		# /var/lib/snapd/lib/gl/libnvcuvid.so.1
		# /var/lib/snapd/lib/gl/libnvcuvid.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-cbl.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-compiler.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-egl-wayland.so.1
		# /var/lib/snapd/lib/gl/libnvidia-egl-wayland.so.1.1.13
		# /var/lib/snapd/lib/gl/libnvidia-eglcore.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-encode.so
		# /var/lib/snapd/lib/gl/libnvidia-encode.so.1
		# /var/lib/snapd/lib/gl/libnvidia-encode.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-fbc.so
		# /var/lib/snapd/lib/gl/libnvidia-fbc.so.1
		# /var/lib/snapd/lib/gl/libnvidia-fbc.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-glcore.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-glsi.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-glvkspirv.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-ml.so
		# /var/lib/snapd/lib/gl/libnvidia-ml.so.1
		# /var/lib/snapd/lib/gl/libnvidia-ml.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-ngx.so.1
		# /var/lib/snapd/lib/gl/libnvidia-ngx.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-nvvm.so
		# /var/lib/snapd/lib/gl/libnvidia-nvvm.so.4
		# /var/lib/snapd/lib/gl/libnvidia-nvvm.so.4.0.0
		# /var/lib/snapd/lib/gl/libnvidia-opencl.so.1
		# /var/lib/snapd/lib/gl/libnvidia-opencl.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-opticalflow.so
		# /var/lib/snapd/lib/gl/libnvidia-opticalflow.so.1
		# /var/lib/snapd/lib/gl/libnvidia-opticalflow.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-ptxjitcompiler.so
		# /var/lib/snapd/lib/gl/libnvidia-ptxjitcompiler.so.1
		# /var/lib/snapd/lib/gl/libnvidia-ptxjitcompiler.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-rtcore.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-tls.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-vulkan-producer.so
		# /var/lib/snapd/lib/gl/libnvidia-vulkan-producer.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvoptix.so.1
		# /var/lib/snapd/lib/gl/libnvoptix.so.470.256.02
		# /var/lib/snapd/lib/gl/libnvidia-vulkan-producer.so: undefined symbol: wlEglInitializeSurfaceExport: No such file or directory
		echo "Broken driver is in use, expecting: skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-} to say: broken-driver"
		test "$(cat "skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-}")" = "broken-driver"
		exit 0
		;;
	esac

	# At this step, we we expect this test to work, and no skip file to exist.
	echo "Everything is good, expecting skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-} not to exist"
	test ! -e "skip.d/$SPREAD_SYSTEM/$PACKAGE_VERSION${PACKAGE_SUFFIX:-}"

	# We will need to install i386 libraries. This is specifically done on an
	# amd64 system as there are cases of 32bit programs running through
	# otherwise 64bit snap, running on 64bit host.
	dpkg --add-architecture i386
	apt-get update

	# Install Nvidia userspace libraries at the designated version.
	apt-get install -y \
		libnvidia-common-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}" \
		libnvidia-compute-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":amd64 \
		libnvidia-compute-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":i386 \
		libnvidia-decode-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":amd64 \
		libnvidia-decode-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":i386 \
		libnvidia-encode-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":amd64 \
		libnvidia-encode-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":i386 \
		libnvidia-fbc1-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":amd64 \
		libnvidia-fbc1-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":i386 \
		libnvidia-gl-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":amd64 \
		libnvidia-gl-"$PACKAGE_VERSION${PACKAGE_SUFFIX:-}":i386

	# Look at the canary file libnvidia-glcore.so.* to get the exact version of
	# the driver. This file is also used by snap-confine, as a pre-condition
	# that the libraries are installed.
	DRIVER_VERSION="$(find /usr/lib/x86_64-linux-gnu/ -name 'libnvidia-glcore.so.*' | sed -e 's,.*/libnvidia-glcore\.so\.,,')"

	# Pretend we have Nvidia kernel module loaded, so that snap-confine enables
	# special logic. The actual version we pretend to have is set later, as it
	# must match installed libraries so that the right canary file is detected by
	# snap-confine.
	mkdir -p /tmp/sys-module/nvidia
	echo "$DRIVER_VERSION" >/tmp/sys-module/nvidia/version
	if os.query is-xenial || os.query is-bionic; then
		mount -o bind /tmp/sys-module/ /sys/module
	else
		mkdir -p /tmp/sys-module-work
		LO="/sys/module/"
		UP="/tmp/sys-module"
		WORK="/tmp/sys-module-work"
		mount -t overlay overlay \
			-olowerdir="$LO",upperdir="$UP",workdir="$WORK" /sys/module
	fi
	tests.cleanup defer rm -rf /tmp/sys-module
	tests.cleanup defer umount /sys/module

	if [ -f test-snapd-nvidia_0.1.0_amd64.snap ]; then
		# If a snap is available locally then install it directly. This allows
		# for local development/iteration without having to publish development
		# builds in the store.
		snap install --dangerous ./test-snapd-nvidia_0.1.0_amd64.snap
	else
		snap install test-snapd-nvidia
	fi
	# Remove the snap so that we discard the namespace. Currently the code in
	# snap-confine does not support updating the Nvidia driver without
	# effectively rebooting the host.
	tests.cleanup defer snap remove test-snapd-nvidia

	# Indicate that we are ready to execute.
	tests.cleanup defer rm -f ready
	touch ready
	;;
execute)
	test -f ready || exit 0

	test-snapd-nvidia.64 >log-64.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl/libEGL_nvidia.so.*' <log-64.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl/libGLESv2_nvidia.so.*' <log-64.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl/libGLX_nvidia.so.*' <log-64.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl/libcuda.so*' <log-64.txt

	test-snapd-nvidia.32 >log-32.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl32/libEGL_nvidia.so.*' <log-32.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl32/libGLESv2_nvidia.so.*' <log-32.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl32/libGLX_nvidia.so.*' <log-32.txt
	MATCH 'dlopen /var/lib/snapd/lib/gl32/libcuda.so*' <log-32.txt
	;;
restore)
	tests.cleanup restore
	;;
debug)
	if [ -f log-32.txt ]; then cat log-32.txt; fi
	if [ -f log-64.txt ]; then cat log-64.txt; fi
	;;
*)
	exit 1
	;;
esac
