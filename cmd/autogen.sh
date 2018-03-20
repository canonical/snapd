#!/bin/sh
# Welcome to the Happy Maintainer's Utility Script
#
# Set BUILD_DIR to the directory where the build will happen, otherwise $PWD
# will be used
set -eux

BUILD_DIR=${BUILD_DIR:-.}
selfdir=$(dirname "$0")
SRC_DIR=$(readlink -f "$selfdir")

# Configure the build
extra_opts=
# shellcheck disable=SC1091
. /etc/os-release
case "$ID" in
	arch)
		extra_opts="libexecdir=/usr/lib SNAP_MOUNT_DIR=/var/lib/snapd/snap APPARMOR=0 NVIDIA_BIARCH=1 MERGED_USR=1"
		;;
	debian)
		extra_opts="libexecdir=/usr/lib"
		;;
	ubuntu)
		extra_opts="libexecdir=/usr/lib NVIDIA_MULTIARCH=1 STATIC_LIBCAP=1 STATIC_LIBAPPARMOR=1 STATIC_LIBSECCOMP=1 APPARMOR=1 HOST_ARCH_TRIPLET=$(dpkg-architecture -qDEB_HOST_MULTIARCH)"
		if [ "$(dpkg-architecture -qDEB_HOST_ARCH)" = "amd64" ]; then
			extra_opts="$extra_opts HOST_ARCH_32BIT_TRIPLET=$(dpkg-architecture -ai386 -qDEB_HOST_MULTIARCH)"
		fi
		;;
	fedora|centos|rhel)
		extra_opts="libexecdir=/usr/libexec SNAP_MOUNT_DIR=/var/lib/snapd/snap MERGED_USR=1 APPARMOR=0"
		;;
	opensuse)
		extra_opts="libexecdir=/usr/lib"
		;;
	solus)
		extra_opts="NVIDIA_BIARCH=1"
		;;
esac

echo "Configuring in build directory $BUILD_DIR with: $extra_opts"
# shellcheck disable=SC2086
(cd .. && make configure prefix=/usr $extra_opts "$@")
