#!/bin/bash

# shellcheck source=tests/lib/quiet.sh
. "$TESTSLIB/quiet.sh"

debian_name_package() {
    case "$1" in
        xdelta3|curl|python3-yaml|kpartx|busybox-static)
            echo "$1"
            ;;
        man)
            echo "man-db"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

ubuntu_14_04_name_package() {
    case "$1" in
        printer-driver-cups-pdf)
            echo "cups-pdf"
            ;;
        *)
            debian_name_package "$1"
            ;;
    esac
}

fedora_name_package() {
    case "$1" in
        xdelta3|jq|curl|python3-yaml)
            echo "$1"
            ;;
        openvswitch-switch)
            echo "openvswitch"
            ;;
        printer-driver-cups-pdf)
            echo "cups-pdf"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

opensuse_name_package() {
    case "$1" in
        python3-yaml)
            echo "python3-PyYAML"
            ;;
        dbus-x11)
            echo "dbus-1-x11"
            ;;
        printer-driver-cups-pdf)
            echo "cups-pdf"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

distro_name_package() {
    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            ubuntu_14_04_name_package "$1"
            ;;
        ubuntu-*|debian-*)
            debian_name_package "$1"
            ;;
        fedora-*)
            fedora_name_package "$1"
            ;;
        opensuse-*)
            opensuse_name_package "$1"
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_install_local_package() {
    allow_downgrades=false
    while [ -n "$1" ]; do
        case "$1" in
            --allow-downgrades)
                allow_downgrades=true
                shift
                ;;
            *)
                break
        esac
    done

    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*|debian-*)
            # relying on dpkg as apt(-get) does not support installation from local files in trusty.
            dpkg -i --force-depends --auto-deconfigure --force-depends-version "$@"
            apt-get -f install -y
            ;;
        ubuntu-*)
            flags="-y"
            if [ "$allow_downgrades" = "true" ]; then
                flags="$flags --allow-downgrades"
            fi
            # shellcheck disable=SC2086
            apt install $flags "$@"
            ;;
        fedora-*)
            dnf -q -y install "$@"
            ;;
        opensuse-*)
            zypper -q install -y "$@"
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_install_package() {
    # Parse additional arguments; once we find the first unknown
    # part we break argument parsing and process all further
    # arguments as package names.
    APT_FLAGS=
    DNF_FLAGS=
    ZYPPER_FLAGS=
    while [ -n "$1" ]; do
        case "$1" in
            --no-install-recommends)
                APT_FLAGS="$APT_FLAGS --no-install-recommends"
                DNF_FLAGS="$DNF_FLAGS --setopt=install_weak_deps=False"
                ZYPPER_FLAGS="$ZYPPER_FLAGS --no-recommends"
                shift
                ;;
            *)
                break
                ;;
        esac
    done

    for pkg in "$@" ; do
        package_name=$(distro_name_package "$pkg")
        # When we could not find a different package name for the distribution
        # we're running on we try the package name given as last attempt
        if [ -z "$package_name" ]; then
            package_name="$pkg"
        fi

        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                quiet apt-get install $APT_FLAGS -y "$package_name"
                ;;
            fedora-*)
                dnf -q -y install $DNF_FLAGS "$package_name"
                ;;
            opensuse-*)
                zypper -q install -y $ZYPPER_FLAGS "$package_name"
                ;;
            *)
                echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
                exit 1
                ;;
        esac
    done
}

distro_purge_package() {
    for pkg in "$@" ; do
        package_name=$(distro_name_package "$pkg")
        # When we could not find a different package name for the distribution
        # we're running on we try the package name given as last attempt
        if [ -z "$package_name" ]; then
            package_name="$pkg"
        fi

        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                quiet apt-get remove -y --purge -y "$package_name"
                ;;
            fedora-*)
                dnf -y -q remove "$package_name"
                ;;
            opensuse-*)
                zypper -q remove -y "$package_name"
                ;;
            *)
                echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
                exit 1
                ;;
        esac
    done
}

distro_update_package_db() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            quiet apt-get update
            ;;
        fedora-*)
            dnf -q makecache
            ;;
        opensuse-*)
            zypper -q refresh
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_clean_package_cache() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            quiet apt-get clean
            ;;
        opensuse-*)
            zypper -q clean --all
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_auto_remove_packages() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            quiet apt-get -y autoremove
            ;;
        fedora-*)
            dnf -q -y autoremove
            ;;
        opensuse-*)
            ;;
        *)
            echo "ERROR: Unsupported distribution '$SPREAD_SYSTEM'"
            exit 1
            ;;
    esac
}

distro_query_package_info() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            apt-cache policy "$1"
            ;;
        fedora-*)
            dnf info "$1"
            ;;
        opensuse-*)
            zypper info "$1"
            ;;
    esac
}

distro_install_build_snapd(){
    if [ "$SRU_VALIDATION" = "1" ]; then
        apt install -y snapd
        cp /etc/apt/sources.list sources.list.back
        echo "deb http://archive.ubuntu.com/ubuntu/ $(lsb_release -c -s)-proposed restricted main multiverse universe" | tee /etc/apt/sources.list -a
        apt update
        apt install -y --only-upgrade snapd
        mv sources.list.back /etc/apt/sources.list
        apt update
        # On trusty we may pull in a new hwe-kernel that is needed to run the
        # snapd tests. We need to reboot to actually run this kernel.
        if [[ "$SPREAD_SYSTEM" = ubuntu-14.04-* ]] && [ "$SPREAD_REBOOT" = 0 ]; then
            REBOOT
        fi
    else
        packages=
        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                packages="${GOHOME}"/snapd_*.deb
                ;;
            fedora-*)
                packages="${GOHOME}"/snap-confine*.rpm\ "${GOPATH}"/snapd*.rpm
                ;;
            opensuse-*)
                packages="${GOHOME}"/snapd*.rpm
                ;;
            *)
                exit 1
                ;;
        esac

        distro_install_local_package $packages

        # On some distributions the snapd.socket is not yet automatically
        # enabled as we don't have a systemd present configuration approved
        # by the distribution for it in place yet.
        if ! systemctl is-enabled snapd.socket ; then
            # Can't use --now here as not all distributions we run on support it
            systemctl enable snapd.socket
            systemctl start snapd.socket
        fi
    fi
}


pkg_dependency_ubuntu_generic(){
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

pkg_dependency_ubuntu_classic(){
    echo cups
    echo dbus-x11
    echo jq
    echo man
    echo printer-driver-cups-pdf
    echo python3-yaml
    echo upower

    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            echo "linux-image-extra-$(uname -r)"
            echo pollinate
            ;;
        ubuntu-16.04-32)
            echo "linux-image-extra-$(uname -r)"
            echo pollinate
            ;;
        ubuntu-16.04-64)
            echo gccgo-6
            echo kpartx
            echo libvirt-bin
            echo "linux-image-extra-$(uname -r)"
            echo pollinate
            echo qemu
            echo x11-utils
            echo xvfb
            ;;
        ubuntu-*)
            echo "linux-image-extra-$(uname -r)"
            echo pollinate
            ;;
        debian-*)
            ;;
    esac
}

pkg_dependency_ubuntu_core(){
    echo "linux-image-extra-$(uname -r)"
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
            pkg_dependency_ubuntu_generic
            pkg_dependency_ubuntu_core
            ;;
        ubuntu-*|debian-*)
            pkg_dependency_ubuntu_generic
            pkg_dependency_ubuntu_classic
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
    pkgs=$(pkg_dependencies | tr "\n" " ")

    # ensure systemd is up-to-date, if there is a mismatch libudev-dev
    # will fail to install because the poor apt resolver does not get it
    case "$SPREAD_SYSTEM" in
        debian-*|ubuntu-*)
            apt-get install -y --only-upgrade systemd
    esac

    echo "Installing the following packages: $pkgs"
    distro_install_package $pkgs
}
