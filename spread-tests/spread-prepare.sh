#!/bin/sh
# This script is started by spread to prepare the execution environment
set -x
set -u
set -e

# Sanity check, are we in the top-level directory of the tree?
test -f configure.ac || ( echo 'this script must be executed from the top-level of the tree' && exit 1)

# Record where the top level directory is
top_dir=$(pwd)

# Record the current distribution release data to know what to do
release_ID="$( . /etc/os-release && echo "${ID:-linux}" )"
release_VERSION_ID="$( . /etc/os-release && echo "${VERSION_ID:-}" )"


# Create source distribution tarball and place it in the top-level directory.
create_dist_tarball() {
    # Load the version number from a dedicated file
    local pkg_version=
    pkg_version="$(cat "$top_dir/VERSION")"

    # Ensure that build system is up-to-date and ready
    autoreconf -i
    # XXX: This fixes somewhat odd error when configure below (in an empty directory) fails with:
    # configure: error: source directory already configured; run "make distclean" there first
    test -f Makefile && make distclean

    # Create a scratch space to run configure
    scratch_dir="$(mktemp -d)"
    trap 'rm -rf "$scratch_dir"' EXIT

    # Configure the project in a scratch directory
    cd "$scratch_dir"
    "$top_dir/configure" --prefix=/usr

    # Create the distribution tarball
    make dist

    # Ensure we got the tarball we were expecting to see
    test -f "snap-confine-$pkg_version.tar.gz"

    # Move it to the top-level directory
    mv "snap-confine-$pkg_version.tar.gz" "$top_dir/"
}

build_debian_or_ubuntu_package() { 
    # FIXME: error handling and friendly message about how to
    #        add new distro specific bits etc
    # source the distro specific vars
    . $top_dir/spread-tests/distros/$release_ID.common
    . $top_dir/spread-tests/distros/$release_ID.$release_VERSION_ID


    # Ensure that we have a sbuild chroot ready
    if ! schroot -l | grep "chroot:${distro_codename}-.*-sbuild"; then
        sbuild-createchroot \
            --include=eatmydata \
            "--make-sbuild-tarball=/var/lib/sbuild/${distro_codename}-amd64.tar.gz" \
            "$distro_codename" "$(mktemp -d)" \
            "$distro_archive"
    fi

    # Create a scratch space 
    scratch_dir="$(mktemp -d)"
    trap 'rm -rf "$scratch_dir"' EXIT

    # Do everything in the scratch directory
    cd "$scratch_dir"

    # Fetch the current Ubuntu packaging for the appropriate release
    git clone -b "$distro_packaging_git_branch" "$distro_packaging_git" distro-packaging

    # Install all the build dependencies declared by the package.
    apt build-dep -y ./distro-packaging/

    # Generate a new upstream tarball from the current state of the tree
    ( cd "$top_dir" && create_dist_tarball )

    # Prepare the .orig tarball and unpackaged source tree
    cp "$top_dir/snap-confine-$pkg_version.tar.gz" "snap-confine_$pkg_version.orig.tar.gz"
    tar -zxf "snap-confine_$pkg_version.orig.tar.gz" 

    # Apply the debian directory from downstream packaging to form a complete source package
    mv "distro-packaging/debian" "snap-confine-$pkg_version/debian"
    rm -rf distro-packaging

    # Add an automatically-generated changelog entry
    # The --controlmaint takes the maintainer details from debian/control
    ( cd "snap-confine-$pkg_version" && dch --controlmaint --newversion "${pkg_version}-1" "Automatic CI build")

    # Build an unsigned source package
    ( cd "snap-confine-$pkg_version" && dpkg-buildpackage -uc -us -S )

    # Copy source package files to the top-level directory (this helps for
    # interactive debugging since the package is available right there)
    cp ./*.dsc ./*.debian.tar.* ./*.orig.tar.gz "$top_dir/"

    # Build a binary package in a clean chroot.
    # NOTE: nocheck is because the package still includes old unit tests that
    # are deeply integrated into how ubuntu apparmor denials are logged. This
    # should be removed once those test are migrated to spread testes.
    DEB_BUILD_OPTIONS=nocheck sbuild \
        --arch-all \
        --dist="$distro_codename" \
        --batch \
        "$sbuild_args" \
        "snap-confine_${pkg_version}-1.dsc"

    # Copy all binary packages to the top-level directory
    cp ./*.deb "$top_dir/"
}


# Apply tweaks
case "$release_ID" in
    ubuntu)
        # apt update is hanging on security.ubuntu.com with IPv6.
        sysctl -w net.ipv6.conf.all.disable_ipv6=1
        trap "sysctl -w net.ipv6.conf.all.disable_ipv6=0" EXIT
        ;;
esac


# Install all the build dependencies
case "$release_ID" in
    ubuntu|debian)
        apt-get update
        # On Debian and derivatives we need the following things:
        # - sbuild -- to build the binary package with extra hygiene
        # - devscripts -- to modify the changelog automatically
        # - git -- to clone native downstream packaging
        apt-get install --quiet -y sbuild devscripts git
        # XXX: Taken from https://wiki.debian.org/sbuild
        mkdir -p /root/.gnupg
        # NOTE: We cannot use sbuild-update --keygen as virtual machines lack
        # the necessary entropy to generate keys before the spread timeout
        # kicks in. Instead we just copy pre-made, insecure keys from the
        # source repository.
        mkdir -p /var/lib/sbuild/apt-keys/
        cp -a .spread-data/apt-keys/* /var/lib/sbuild/apt-keys/
        sbuild-adduser "$LOGNAME"
        ;;
    *)
        echo "unsupported distribution: $release_ID"
        echo "patch spread-prepare to teach it about how to install build dependencies"
        exit 1
        ;;
esac

# Build and install the native package using downstream packaging and the fresh upstream tarball
case "$release_ID" in
    ubuntu|debian)
        build_debian_or_ubuntu_package "$release_ID" "$release_VERSION_ID"
        # Install the freshly-built packages
        dpkg -i snap-confine_*.deb || apt-get -f install -y
        dpkg -i ubuntu-core-launcher_*.deb || apt-get -f install -y
        # Install snapd (testes require it)
        apt-get install -y snapd
        ;;
    *)
        echo "unsupported distribution: $release_ID"
        exit 1
        ;;
esac

# Install the core snap
snap list | grep -q ubuntu-core || snap install ubuntu-core
