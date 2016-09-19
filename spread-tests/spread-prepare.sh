#!/bin/sh
# This script is started by spread to prepare the execution environment
set -xue

# Sanity check, are we in the top-level directory of the tree?
test -f configure.ac || ( echo 'this script must be executed from the top-level of the tree' && exit 1)

# Record where the top level directory is
top_dir=$(pwd)

# Record the current distribution release data to know what to do
release_ID="$( . /etc/os-release && echo "${ID:-linux}" )"
release_VERSION_ID="$( . /etc/os-release && echo "${VERSION_ID:-}" )"


build_debian_or_ubuntu_package() {
    local pkg_version
    local distro_packaging_git_branch
    local distro_packaging_git
    local distro_archive
    local distro_codename
    local sbuild_createchroot_extra=""
    pkg_version="$(cat "$top_dir/VERSION")"
    
    if [ ! -f "$top_dir/spread-tests/distros/$release_ID.$release_VERSION_ID" ] || \
       [ ! -f "$top_dir/spread-tests/distros/$release_ID.common" ]; then
        echo "Distribution: $release_ID (release $release_VERSION_ID) is not supported"
        echo "please read this script and create new files in spread-test/distros"
        exit 1
    fi

    # source the distro specific vars
    . "$top_dir/spread-tests/distros/$release_ID.$release_VERSION_ID"
    . "$top_dir/spread-tests/distros/$release_ID.common"

    # sanity check, ensure that essential variables were defined
    test -n "$distro_packaging_git_branch"
    test -n "$distro_packaging_git"
    test -n "$distro_archive"
    test -n "$distro_codename"

    # Create a scratch space 
    scratch_dir="$(mktemp -d)"
    trap 'rm -rf "$scratch_dir"' EXIT

    # Do everything in the scratch directory
    cd "$scratch_dir"

    # Fetch the current Ubuntu packaging for the appropriate release
    git clone -b "$distro_packaging_git_branch" "$distro_packaging_git" distro-packaging

    # Install all the build dependencies declared by the package.
    apt-get install --quiet -y gdebi-core
    apt-get install --quiet -y $(gdebi --quiet --apt-line ./distro-packaging/debian/control)

    # Generate a new upstream tarball from the current state of the tree
    ( cd "$top_dir" && spread-tests/release.sh )

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

    # Ensure that we have a sbuild chroot ready
    if ! schroot -l | grep "chroot:${distro_codename}-.*-sbuild"; then
        sbuild-createchroot \
            --include=eatmydata \
            "--make-sbuild-tarball=/var/lib/sbuild/${distro_codename}-amd64.tar.gz" \
            "$sbuild_createchroot_extra" \
            "$distro_codename" "$(mktemp -d)" \
            "$distro_archive"
    fi

    # Build a binary package in a clean chroot.
    # NOTE: nocheck is because the package still includes old unit tests that
    # are deeply integrated into how ubuntu apparmor denials are logged. This
    # should be removed once those test are migrated to spread testes.
    DEB_BUILD_OPTIONS=nocheck sbuild \
        --arch-all \
        --dist="$distro_codename" \
        --batch \
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
        # treat APT_PROXY as a location of apt-cacher-ng to use
        if [ -n "${APT_PROXY:-}" ]; then
            printf 'Acquire::http::Proxy "%s";\n' "$APT_PROXY" > /etc/apt/apt.conf.d/00proxy
        fi
        # cope with unexpected /etc/apt/apt.conf.d/95cloud-init-proxy that may be in the image
        rm -f /etc/apt/apt.conf.d/95cloud-init-proxy || :
        # trusty support is under development right now
        # we special-case the release until we have officially landed
        if [ "$release_ID" = "ubuntu" ] && [ "$release_VERSION_ID" = "14.04" ]; then
            add-apt-repository ppa:thomas-voss/trusty
        fi
        apt-get update
        apt-get dist-upgrade -y
        if [ "$release_ID" = "ubuntu" ] && [ "$release_VERSION_ID" = "14.04" ]; then
            apt-get install -y systemd
            # starting systemd manually is working around
            # systemd not running as PID 1 on trusty systems.
            service systemd start
        fi
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
        cp -a "$top_dir/spread-tests/data/apt-keys/"* /var/lib/sbuild/apt-keys/
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
