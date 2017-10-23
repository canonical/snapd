#!/bin/sh
# Welcome to the Happy Maintainer's Utility Script
set -eux

# We need the VERSION file to configure
if [ ! -e VERSION ]; then
	( cd .. && ./mkversion.sh )
fi

# Sanity check, are we in the right directory?
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
		extra_opts="--libexecdir=/usr/lib/snapd --with-snap-mount-dir=/var/lib/snapd/snap --disable-apparmor --enable-nvidia-biarch --enable-merged-usr"
		;;
	debian)
		extra_opts="--libexecdir=/usr/lib/snapd"
		;;
	ubuntu)
		case "$VERSION_ID" in
			16.04)
				extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-multiarch --enable-static-libcap --enable-static-libapparmor --enable-static-libseccomp"
				;;
			*)
				extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-multiarch --enable-static-libcap"
				;;
		esac
		;;
	fedora|centos|rhel)
		extra_opts="--libexecdir=/usr/libexec/snapd --with-snap-mount-dir=/var/lib/snapd/snap --enable-merged-usr --disable-apparmor"
		;;
	opensuse)
		extra_opts="--libexecdir=/usr/lib/snapd"
		;;
	solus)
		extra_opts="--enable-nvidia-biarch"
		;;
esac

echo "Configuring with: $extra_opts"
# shellcheck disable=SC2086
./configure --enable-maintainer-mode --prefix=/usr $extra_opts
