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
. /etc/os-release
case "$ID" in
	arch)
		extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-arch"
		;;
	debian)
		extra_opts="--libexecdir=/usr/lib/snapd"
		;;
	ubuntu)
		extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-ubuntu"
		;;
	fedora|centos|rhel)
		extra_opts="--libexecdir=/usr/libexec/snapd --with-snap-mount-dir=/var/lib/snapd/snap --enable-merged-usr --disable-apparmor"
		;;
esac

./configure --enable-maintainer-mode --prefix=/usr $extra_opts
