#!/bin/bash

# shellcheck source=tests/lib/quiet.sh
. "$TESTSLIB/quiet.sh"

debian_name_package() {
    for i in "$@"; do
        case "$i" in
            xdelta3|curl|python3-yaml|kpartx|busybox-static|nfs-kernel-server)
                echo "$i"
                ;;
            man)
                echo "man-db"
                ;;
            *)
                echo "$i"
                ;;
        esac
    done
}

ubuntu_14_04_name_package() {
    for i in "$@"; do
        case "$i" in
            printer-driver-cups-pdf)
                echo "cups-pdf"
                ;;
            *)
                debian_name_package "$i"
                ;;
        esac
    done
}

fedora_name_package() {
    for i in "$@"; do
        case "$i" in
            xdelta3|jq|curl|python3-yaml)
                echo "$i"
                ;;
            openvswitch-switch)
                echo "openvswitch"
                ;;
            printer-driver-cups-pdf)
                echo "cups-pdf"
                ;;
            *)
                echo "$i"
                ;;
        esac
    done
}

opensuse_name_package() {
    for i in "$@"; do
        case "$i" in
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
                echo "$i"
                ;;
        esac
    done
}

arch_name_package() {
    case "$1" in
        python3-yaml)
            echo "python-yaml"
            ;;
        dbus-x11)
            # no separate dbus-x11 package in arch
            echo "dbus"
            ;;
        printer-driver-cups-pdf)
            echo "cups-pdf"
            ;;
        openvswitch-switch)
            echo "openvswitch"
            ;;
        man)
            echo "man-db"
            ;;
        *)
            echo "$1"
            ;;
    esac
}

distro_name_package() {
    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            ubuntu_14_04_name_package "$@"
            ;;
        ubuntu-*|debian-*)
            debian_name_package "$@"
            ;;
        fedora-*)
            fedora_name_package "$@"
            ;;
        opensuse-*)
            opensuse_name_package "$@"
            ;;
        arch-*)
            arch_name_package "$1"
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
            quiet dnf -y install "$@"
            ;;
        opensuse-*)
            quiet rpm -i "$@"
            ;;
        arch-*)
            pacman -U --noconfirm "$@"
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

    # fix dependency issue where libp11-kit0 needs to be downgraded to 
    # install gnome-keyring
    case "$SPREAD_SYSTEM" in
        debian-9-*)
        if [[ "$*" =~ "gnome-keyring" ]]; then
            apt-get remove -y libp11-kit0
        fi
        ;;
    esac

    pkg_names=($(
        for pkg in "$@" ; do
            package_name=$(distro_name_package "$pkg")
            # When we could not find a different package name for the distribution
            # we're running on we try the package name given as last attempt
            if [ -z "$package_name" ]; then
                package_name="$pkg"
            fi
            echo "$package_name"
        done
    ))

    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            # shellcheck disable=SC2086
            quiet apt-get install $APT_FLAGS -y "${pkg_names[@]}"
            ;;
        fedora-*)
            # shellcheck disable=SC2086
            quiet dnf -y --refresh install $DNF_FLAGS "${pkg_names[@]}"
            ;;
        opensuse-*)
            # shellcheck disable=SC2086
            quiet zypper install -y $ZYPPER_FLAGS "${pkg_names[@]}"
            ;;
        arch-*)
            # shellcheck disable=SC2086
            pacman -Suq --needed --noconfirm "${pkg_names[@]}"
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_purge_package() {
    # shellcheck disable=SC2046
    set -- $(
        for pkg in "$@" ; do
            package_name=$(distro_name_package "$pkg")
            # When we could not find a different package name for the distribution
            # we're running on we try the package name given as last attempt
            if [ -z "$package_name" ]; then
                package_name="$pkg"
            fi
            echo "$package_name"
        done
        )

    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            quiet apt-get remove -y --purge -y "$@"
            ;;
        fedora-*)
            quiet dnf -y remove "$@"
            quiet dnf clean all
            ;;
        opensuse-*)
            quiet zypper remove -y "$@"
            ;;
        arch-*)
            pacman -Rnsc --noconfirm "$@"
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
}

distro_update_package_db() {
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            quiet apt-get update
            ;;
        fedora-*)
            quiet dnf clean all
            quiet dnf makecache
            ;;
        opensuse-*)
            quiet zypper --gpg-auto-import-keys refresh
            ;;
        arch-*)
            pacman -Syq
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
        fedora-*)
            dnf clean all
            ;;
        opensuse-*)
            zypper -q clean --all
            ;;
        arch-*)
            pacman -Sccq --noconfirm
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
            quiet dnf -y autoremove
            ;;
        opensuse-*)
            ;;
        arch-*)
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
        arch-*)
            pacman -Si "$1"
            ;;
        *)
            echo "ERROR: Unsupported distribution '$SPREAD_SYSTEM'"
            exit 1
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
            arch-*)
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snapd*.pkg.tar.xz
                ;;
            *)
                exit 1
                ;;
        esac

        # shellcheck disable=SC2086
        distro_install_local_package $packages

        if [[ "$SPREAD_SYSTEM" == arch-* ]]; then
            # Arch policy does not allow calling daemon-reloads in package
            # install scripts
            systemctl daemon-reload
        fi

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
        arch-*)
            # default /etc/makepkg.conf setting
            echo "pkg.tar.xz"
            ;;
    esac
}

pkg_dependencies_ubuntu_generic(){
    echo "
        autoconf
        automake
        autotools-dev
        build-essential
        clang
        curl
        devscripts
        expect
        gdb
        gdebi-core
        git
        indent
        jq
        apparmor-utils
        libapparmor-dev
        libglib2.0-dev
        libseccomp-dev
        libudev-dev
        man
        netcat-openbsd
        pkg-config
        python3-docutils
        udev
        upower
        uuid-runtime
        "
}

pkg_dependencies_ubuntu_classic(){
    echo "
        avahi-daemon
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
                "
            ;;
        ubuntu-16.04-32)
            echo "
                evolution-data-server
                gnome-online-accounts
                linux-image-extra-$(uname -r)
                "
            ;;
        ubuntu-16.04-64)
            echo "
                evolution-data-server
                gccgo-6
                gnome-online-accounts
                kpartx
                libvirt-bin
                linux-image-extra-$(uname -r)
                nfs-kernel-server
                qemu
                x11-utils
                xvfb
                "
            ;;
        ubuntu-17.10-64)
            echo "
                linux-image-extra-4.13.0-16-generic
                "
            ;;
        ubuntu-18.04-64)
            echo "
                evolution-data-server
                "
            ;;
        ubuntu-*)
            echo "
                squashfs-tools
                "
            ;;
        debian-*)
            echo "
                evolution-data-server
                net-tools
                "
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
        clang
        curl
        dbus-x11
        evolution-data-server
        expect
        git
        golang
        jq
        iptables-services
        man
        mock
        net-tools
        python3-yaml
        redhat-lsb-core
        rpm-build
        xdg-user-dirs
        "
}

pkg_dependencies_opensuse(){
    echo "
        apparmor-profiles
        clang
        curl
        evolution-data-server
        expect
        git
        golang-packaging
        jq
        lsb-release
        man
        python3-yaml
        netcat-openbsd
        osc
        uuidd
        xdg-utils
        xdg-user-dirs
        "
}

pkg_dependencies_arch(){
    echo "
    base-devel
    bash-completion
    clang
    curl
    evolution-data-server
    expect
    git
    go
    go-tools
    jq
    libseccomp
    libcap
    libx11
    net-tools
    openbsd-netcat
    python
    python-docutils
    python3-yaml
    squashfs-tools
    shellcheck
    strace
    xdg-user-dirs
    xfsprogs
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
        arch-*)
            pkg_dependencies_arch
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

# upgrade distribution and indicate if reboot is needed by outputting 'reboot'
# to stdout
distro_upgrade() {
    case "$SPREAD_SYSTEM" in
        arch-*)
            # Arch does not support partial upgrades. On top of this, the image
            # we are running in may have been built some time ago and we need to
            # upgrade so that the tests are run with the same package versions
            # as the users will have. We basically need to run pacman -Syu.
            # Since there is no way to tell if we can continue after upgrading
            # (eg. the kernel package or systemd got updated ) issue a reboot
            # instead.
            #
            # pacman -Syu --noconfirm on an updated system:
            # :: Synchronizing package databases...
            #  core is up to date
            #  extra is up to date
            #  community is up to date
            #  multilib is up to date
            # :: Starting full system upgrade...
            #  there is nothing to do  <--- needle
            if ! pacman -Syu --noconfirm 2>&1 | grep -q "there is nothing to do" ; then
                echo "reboot"
            fi
            ;;
        *)
            echo "WARNING: distro upgrade not supported on $SPREAD_SYSTEM"
            ;;
    esac
}
