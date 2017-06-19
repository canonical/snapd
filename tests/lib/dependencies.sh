#!/bin/bash

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

pkg_apt_dependencies_generic(){
    echo autoconf
    echo automake
    echo autotools-dev
    echo build-essential
    echo curl
    echo devscripts
    echo expect
    echo gdebi-core
    echo git
    echo indent
    echo jq
    echo libapparmor-dev
    echo libglib2.0-dev
    echo libseccomp-dev
    echo libudev-dev
    echo netcat-openbsd
    echo pkg-config
    echo python3-docutils
    echo rng-tools
    echo udev
}

pkg_apt_dependencies_classic(){
    echo dbus-x11
    echo jq
    echo man
    echo python3-yaml
    echo upower

    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            echo cups-pdf
            echo linux-image-extra-$(uname -r)
            echo pollinate
            ;;
        ubuntu-16.04-32)
            echo linux-image-extra-$(uname -r)
            echo pollinate
            echo printer-driver-cups-pdf
            ;;
        ubuntu-16.04-64)
            echo gccgo-6
            echo kpartx
            echo libvirt-bin
            echo linux-image-extra-$(uname -r)
            echo pollinate
            echo printer-driver-cups-pdf
            echo qemu
            echo x11-utils
            echo xvfb
            ;;
        ubuntu-*)
            echo linux-image-extra-$(uname -r)
            echo pollinate
            echo printer-driver-cups-pdf
            ;;
        debian-*)
            echo printer-driver-cups-pdf
            ;;
    esac 
}

pkg_apt_dependencies_core(){
    echo linux-image-extra-$(uname -r)
    echo pollinate
}

pkg_dependency_fedora(){
    echo curl
    echo expect
    echo git
    echo golang
    echo mock
    echo redhat-lsb-core
    echo rpm-build
}

pkg_dependency_opensuse(){
    echo curl
    echo expect
    echo git
    echo golang-packaging
    echo jq
    echo lsb-release
    echo netcat-openbsd
    echo osc
    echo rng-tools
}

pkg_dependencies(){
    case "$SPREAD_SYSTEM" in
        ubuntu-core-16-*)
            pkg_apt_dependencies_generic
            pkg_apt_dependencies_core
            ;;
        ubuntu-*|debian-*)
            pkg_apt_dependencies_generic
            pkg_apt_dependencies_classic
            ;;
        fedora-*)
            pkg_dependency_fedora
            ;;
        opensuse-*)
            pkg_dependency_opensuse
            ;;
        *)
            ;;
    esac  
}

install_dependencies(){
    pcks=$(pkg_dependencies | tr "\n" " ")
    echo "Installing the following packages: $DEPENDENCY_PACKAGES"
    distro_install_package $pcks
}
