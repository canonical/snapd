#!/bin/bash -ex

# Builds and installs the ubuntu-core-initramfs package for the Ubuntu
# release running in the system.
build_and_install_initramfs_deb() {
    pushd "$PROJECT_PATH"/core-initrd

    # For dpkg-parsechangelog (used by mkversion.sh too) and to have
    # the tools needed to build the source package.
    quiet eatmydata apt-get install -y dpkg-dev debhelper
    codename=$(lsb_release -c -s)
    latest=$(dpkg-parsechangelog --file latest/debian/changelog --show-field Distribution)
    if [ "$codename" = "$latest" ]; then
        rel=latest
    else
        rel=$(lsb_release -r -s)
    fi

    # build source packages using local code
    TEST_BUILD=1 ./build-source-pkgs.sh "$rel"

    # build and install binary package

    pushd "$rel"
    quiet eatmydata apt-get build-dep -y ./
    dpkg-buildpackage -tc -us -uc
    popd

    quiet eatmydata apt-get install -y ./ubuntu-core-initramfs_*.deb

    popd
}
