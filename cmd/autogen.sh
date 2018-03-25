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

# Sanity check, are we in the right directory?
test -f configure.ac

# Regenerate the build system
rm -f config.status
autoreconf -i -f -v

# Configure the build
extra_opts=
# shellcheck disable=SC1091
. /etc/os-release
case "$ID" in
	arch)
		extra_opts="--libexecdir=/usr/lib --with-snap-mount-dir=/var/lib/snapd/snap --disable-apparmor --enable-nvidia-biarch --enable-merged-usr --with-snapd-environment-file=/etc/default/snapd"
		;;
	debian)
		extra_opts="--libexecdir=/usr/lib"
		;;
	ubuntu)
		case "$VERSION_ID" in
			16.04)
				extra_opts="--libexecdir=/usr/lib --enable-nvidia-multiarch --enable-static-libcap --enable-static-libapparmor --enable-static-libseccomp"
				;;
			*)
				extra_opts="--libexecdir=/usr/lib --enable-nvidia-multiarch --enable-static-libcap"
				;;
		esac
		;;
	fedora|centos|rhel)
		extra_opts="--libexecdir=/usr/libexec --with-snap-mount-dir=/var/lib/snapd/snap --enable-merged-usr --disable-apparmor --with-snapd-environment-file=/etc/sysconfig/snapd"
		;;
	opensuse)
		extra_opts="--libexecdir=/usr/lib"
		;;
	solus)
		extra_opts="--enable-nvidia-biarch"
		;;
esac

echo "Configuring in build directory $BUILD_DIR with: $extra_opts"
mkdir -p "$BUILD_DIR" && cd "$BUILD_DIR"
# shellcheck disable=SC2086
"${SRC_DIR}/configure" --enable-maintainer-mode --prefix=/usr --sysconfdir=/etc $extra_opts
