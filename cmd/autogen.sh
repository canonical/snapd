#!/bin/sh
# Welcome to the Happy Maintainer's Utility Script
#
# Set BUILD_DIR to the directory where the build will happen, otherwise $PWD
# will be used
set -eux

BUILD_DIR=${BUILD_DIR:-.}
selfdir=$(dirname "$0")
SRC_DIR=$(readlink -f "$selfdir")

# We need the VERSION file to configure
if [ ! -e VERSION ]; then
	( cd .. && ./mkversion.sh )
fi

# Precondition check, are we in the right directory?
test -f configure.ac

# Regenerate the build system
rm -f config.status
autoreconf -i -f

# Configure the build
extra_opts=
# shellcheck disable=SC1091
. /etc/os-release
case "$ID" in
	arch)
		extra_opts="--libexecdir=/usr/lib/snapd --with-snap-mount-dir=/var/lib/snapd/snap --enable-apparmor --enable-nvidia-biarch --enable-merged-usr"
		;;
	debian)
		extra_opts="--libexecdir=/usr/lib/snapd"
		;;
	gentoo)
		extra_opts="--libexecdir=/usr/lib/snapd --with-snap-mount-dir=/var/lib/snapd/snap --enable-apparmor --enable-nvidia-biarch --enable-merged-usr"
		;;
	ubuntu)
		extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-multiarch --enable-static-libcap --enable-static-libapparmor --with-host-arch-triplet=$(dpkg-architecture -qDEB_HOST_MULTIARCH)"
		if [ "$(dpkg-architecture -qDEB_HOST_ARCH)" = "amd64" ]; then
			extra_opts="$extra_opts --with-host-arch-32bit-triplet=$(dpkg-architecture -ai386 -qDEB_HOST_MULTIARCH)"
		fi
		;;
	fedora|centos|rhel)
		extra_opts="--libexecdir=/usr/libexec/snapd --with-snap-mount-dir=/var/lib/snapd/snap --enable-merged-usr --disable-apparmor --enable-selinux"
		;;
	opensuse-tumbleweed)
		  extra_opts="--libexecdir=/usr/libexec/snapd --enable-nvidia-biarch --with-32bit-libdir=/usr/lib --enable-merged-usr"
		  ;;
	opensuse)
		extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-biarch --with-32bit-libdir=/usr/lib --enable-merged-usr"
		;;
	solus)
		extra_opts="--enable-nvidia-biarch"
		;;
	altlinux)
		extra_opts="--libexecdir=/usr/lib/snapd --with-snap-mount-dir=/var/lib/snapd/snap --disable-apparmor --enable-selinux --enable-nvidia-biarch --with-32bit-libdir=/usr/lib"
		;;
esac

echo "Configuring in build directory $BUILD_DIR with: $extra_opts"
mkdir -p "$BUILD_DIR" && cd "$BUILD_DIR"
# shellcheck disable=SC2086
"${SRC_DIR}/configure" --enable-maintainer-mode --prefix=/usr $extra_opts "$@"
