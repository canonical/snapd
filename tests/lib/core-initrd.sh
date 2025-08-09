#!/bin/bash -ex

# Builds the ubuntu-core-initramfs .deb for the running Ubuntu release.
# Runs in a subshell to avoid leaking directory changes.
build_initramfs_deb() (
    pushd "$PROJECT_PATH"/core-initrd

    # For dpkg-parsechangelog, dch, and ubuntu-distro-info (used by
    # mkversion.sh) and to have the tools needed to build the source package.
    quiet eatmydata apt-get install -y dpkg-dev debhelper devscripts distro-info

    codename=$(lsb_release -c -s)
    latest=$(dpkg-parsechangelog --file latest/debian/changelog --show-field Distribution)
    if [ "$codename" = "$latest" ]; then
        rel=latest
    else
        rel=$(lsb_release -r -s)
    fi

    # build source packages using local code
    TEST_BUILD=1 ./build-source-pkgs.sh "$rel"

    # build binary package
    pushd "$rel"
    quiet eatmydata apt-get build-dep -y ./
    dpkg-buildpackage -tc -us -uc
    popd

    popd
)

# Builds and installs the ubuntu-core-initramfs package for the Ubuntu
# release running in the system. Runs in subshell to prevent changes
# of working directory on failures.
build_and_install_initramfs_deb() (
    build_initramfs_deb

    # install the produced .deb (lives in core-initrd/ after dpkg-buildpackage)
    quiet eatmydata apt-get install -y "$PROJECT_PATH"/core-initrd/ubuntu-core-initramfs_*.deb
)
