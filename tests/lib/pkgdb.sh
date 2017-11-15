#!/bin/bash

# shellcheck source=tests/lib/quiet.sh
. "$TESTSLIB/quiet.sh"

debian_name_package() {
    case "$1" in
        xdelta3|curl|python3-yaml|kpartx|busybox-static|nfs-kernel-server)
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

    # ensure systemd is up-to-date, if there is a mismatch libudev-dev
    # will fail to install because the poor apt resolver does not get it
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
        if [[ "$*" =~ "libudev-dev" ]]; then
            apt-get install -y --only-upgrade systemd
        fi
        ;;
    esac

    for pkg in "$@" ; do
        package_name=$(distro_name_package "$pkg")
        # When we could not find a different package name for the distribution
        # we're running on we try the package name given as last attempt
        if [ -z "$package_name" ]; then
            package_name="$pkg"
        fi

        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                # shellcheck disable=SC2086
                quiet apt-get install $APT_FLAGS -y "$package_name"
                ;;
            fedora-*)
                # shellcheck disable=SC2086
                dnf -q -y --refresh install $DNF_FLAGS "$package_name"
                ;;
            opensuse-*)
                # shellcheck disable=SC2086
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
                dnf -q clean all
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
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snapd_*.deb
                ;;
            fedora-*)
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snap-confine*.rpm\ "${GOPATH}"/snapd*.rpm
                ;;
            opensuse-*)
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snapd*.rpm
                ;;
            *)
                exit 1
                ;;
        esac

        # shellcheck disable=SC2086
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

distro_get_package_extension() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            echo "deb"
            ;;
        fedora-*|opensuse-*)
            echo "rpm"
            ;;
    esac
}

pkg_dependencies_ubuntu_generic(){
    echo "
        autoconf
        automake
        autotools-dev
        build-essential
        curl
        devscripts
        expect
        gdebi-core
        git
        indent
        jq
        libapparmor-dev
        libglib2.0-dev
        libseccomp-dev
        libudev-dev
        netcat-openbsd
        pkg-config
        python3-docutils
        rng-tools
        udev
        "
}

pkg_dependencies_ubuntu_classic(){
    echo "
        cups
        dbus-x11
        gnome-keyring
        jq
        man
        printer-driver-cups-pdf
        python3-yaml
        upower
        weston
        xdg-user-dirs
        xdg-utils
        "

    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            echo "
                linux-image-extra-$(uname -r)
                pollinate
                "
            ;;
        ubuntu-16.04-32)
            echo "
                linux-image-extra-$(uname -r)
                pollinate
                "
            ;;
        ubuntu-16.04-64)
            echo "
                gccgo-6
                kpartx
                libvirt-bin
                linux-image-extra-$(uname -r)
                pollinate
                qemu
                x11-utils
                xvfb
                "
            ;;
        ubuntu-*)
            echo "
                linux-image-extra-$(uname -r)
                pollinate
                "
            ;;
        debian-*)
            ;;
    esac
}

pkg_dependencies_ubuntu_core(){
    echo "
        linux-image-extra-$(uname -r)
        pollinate
        "
}

pkg_dependencies_fedora(){
    echo "
        curl
        dbus-x11
        expect
        git
        golang
        jq
        mock
        redhat-lsb-core
        rpm-build
        xdg-user-dirs
        "
}

pkg_dependencies_opensuse(){
    echo "
        curl
        expect
        git
        golang-packaging
        jq
        lsb-release
        netcat-openbsd
        osc
        rng-tools
        xdg-utils
        xdg-user-dirs
        "
}

pkg_dependencies(){
    case "$SPREAD_SYSTEM" in
        ubuntu-core-16-*)
            pkg_dependencies_ubuntu_generic
            pkg_dependencies_ubuntu_core
            ;;
        ubuntu-*|debian-*)
            pkg_dependencies_ubuntu_generic
            pkg_dependencies_ubuntu_classic
            ;;
        fedora-*)
            pkg_dependencies_fedora
            ;;
        opensuse-*)
            pkg_dependencies_opensuse
            ;;
        *)
            ;;
    esac
}

install_pkg_dependencies(){
    pkgs=$(pkg_dependencies)
    # shellcheck disable=SC2086
    distro_install_package $pkgs
}
