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
		extra_opts="--libexecdir=/usr/lib/snapd --with-snap-mount-dir=/var/lib/snapd/snap --disable-apparmor --enable-nvidia-arch --enable-merged-usr"
		;;
	debian)
		extra_opts="--libexecdir=/usr/lib/snapd"
		;;
	ubuntu)
		case "$VERSION_ID" in
			16.04)
				extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-ubuntu --enable-static-libcap --enable-static-libapparmor --enable-static-libseccomp"
				;;
			*)
				extra_opts="--libexecdir=/usr/lib/snapd --enable-nvidia-ubuntu --enable-static-libcap"
				;;
		esac
		;;
	fedora|centos|rhel)
		extra_opts="--libexecdir=/usr/libexec/snapd --with-snap-mount-dir=/var/lib/snapd/snap --enable-merged-usr --disable-apparmor"
		;;
	opensuse)
		# NOTE: we need to disable apparmor as the version on OpenSUSE
		# is too old to confine snap-confine and installed snaps
		# themselves. This should be changed once all the kernel
		# patches find their way into the distribution.
		extra_opts="--libexecdir=/usr/lib/snapd --disable-apparmor"
		;;
esac

echo "Configuring with: $extra_opts"
./configure --enable-maintainer-mode --prefix=/usr $extra_opts
