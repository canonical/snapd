#!/bin/bash

debian_name_package() {
    #shellcheck source=tests/lib/tools/tests.pkgs.apt.sh
    . "$TESTSLIB/tools/tests.pkgs.apt.sh"
    #shellcheck disable=SC2317
    for i in "$@"; do
        remap_one "$i"
    done
}

ubuntu_14_04_name_package() {
    #shellcheck source=tests/lib/tools/tests.pkgs.apt.sh
    . "$TESTSLIB/tools/tests.pkgs.apt.sh"
    #shellcheck disable=SC2317
    for i in "$@"; do
        remap_one "$i"
    done
}

fedora_name_package() {
    #shellcheck source=tests/lib/tools/tests.pkgs.dnf-yum.sh
    . "$TESTSLIB/tools/tests.pkgs.dnf-yum.sh"
    #shellcheck disable=SC2317
    for i in "$@"; do
        remap_one "$i"
    done
}

amazon_name_package() {
    #shellcheck source=tests/lib/tools/tests.pkgs.dnf-yum.sh
    . "$TESTSLIB/tools/tests.pkgs.dnf-yum.sh"
    #shellcheck disable=SC2317
    for i in "$@"; do
        remap_one "$i"
    done
}

opensuse_name_package() {
    #shellcheck source=tests/lib/tools/tests.pkgs.zypper.sh
    . "$TESTSLIB/tools/tests.pkgs.zypper.sh"
    #shellcheck disable=SC2317
    for i in "$@"; do
        remap_one "$i"
    done
}

arch_name_package() {
    #shellcheck source=tests/lib/tools/tests.pkgs.pacman.sh
    . "$TESTSLIB/tools/tests.pkgs.pacman.sh"
    #shellcheck disable=SC2317
    for i in "$@"; do
        remap_one "$i"
    done
}

distro_name_package() {
    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            ubuntu_14_04_name_package "$@"
            ;;
        ubuntu-*|debian-*)
            debian_name_package "$@"
            ;;
        amazon-*|centos-7-*)
            amazon_name_package "$@"
            ;;
        fedora-*|centos-*)
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
            eatmydata dpkg -i --force-depends --auto-deconfigure --force-depends-version "$@"
            eatmydata apt-get -f install -y
            ;;
        ubuntu-*)
            flags="-y --no-install-recommends"
            if [ "$allow_downgrades" = "true" ]; then
                flags="$flags --allow-downgrades"
            fi
            # shellcheck disable=SC2086
            apt install $flags "$@"
            ;;
        amazon-*|centos-7-*)
            quiet yum -y localinstall "$@"
            ;;
        fedora-*|centos-*)
            quiet dnf -y install --setopt=install_weak_deps=False "$@"
            ;;
        opensuse-*)
            quiet zypper in -y --no-recommends --allow-unsigned-rpm "$@"
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
    orig_xtrace=$(set -o | awk '/xtrace / { print $2 }')
    set +x
    echo "distro_install_package $*"
    # Parse additional arguments; once we find the first unknown
    # part we break argument parsing and process all further
    # arguments as package names.
    APT_FLAGS=
    DNF_FLAGS=
    if os.query is-fedora; then
        # Fedora images we use come with a number of preinstalled package, among
        # them gtk3. Those packages are needed to run the tests. The
        # xdg-desktop-portal-gtk package uses this in the spec:
        #
        #   Supplements:    (gtk3 and (flatpak or snapd))
        #
        # As a result, when snapd is installed, we will unintentionally pull in
        # xdg-desktop-portal-gtk and its dependencies breaking tests. For this
        # reason, disable weak deps altogether.
        DNF_FLAGS="--setopt=install_weak_deps=False"
    fi
    YUM_FLAGS=
    ZYPPER_FLAGS=
    while [ -n "$1" ]; do
        case "$1" in
            --no-install-recommends)
                APT_FLAGS="$APT_FLAGS --no-install-recommends"
                DNF_FLAGS="$DNF_FLAGS --setopt=install_weak_deps=False"
                ZYPPER_FLAGS="$ZYPPER_FLAGS --no-recommends"
                # TODO no way to set this for yum?
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
            eatmydata apt-get install -y --only-upgrade systemd
        fi
        ;;
    esac

    # shellcheck disable=SC2207
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
            quiet eatmydata apt-get install $APT_FLAGS -y "${pkg_names[@]}"
            retval=$?
            ;;
        amazon-linux-2-*|centos-7-*)
            # shellcheck disable=SC2086
            quiet yum -y install $YUM_FLAGS "${pkg_names[@]}"
            retval=$?
            ;;
        fedora-*|centos-*|amazon-linux-2023-*)
            # shellcheck disable=SC2086
            quiet dnf -y --refresh install $DNF_FLAGS "${pkg_names[@]}"
            retval=$?
            ;;
        opensuse-*)
            # packages may be downgraded in the repositories, which would be
            # picked up next time we ran `zypper dup` and applied locally;
            # however we only update the images periodically, in the meantime,
            # when downgrades affect packages we need or have installed, `zypper
            # in` may stop with the prompt asking the user about either breaking
            # the installed packages or allowing downgrades, passing
            # --allow-downgrade will make the installation proceed

            # shellcheck disable=SC2086
            quiet zypper install -y --allow-downgrade --force-resolution $ZYPPER_FLAGS "${pkg_names[@]}"
            retval=$?
            ;;
        arch-*)
            # shellcheck disable=SC2086
            pacman -Suq --needed --noconfirm "${pkg_names[@]}"
            retval=$?
            ;;
        *)
            echo "ERROR: Unsupported distribution $SPREAD_SYSTEM"
            exit 1
            ;;
    esac
    test "$orig_xtrace" = on && set -x
    # pass any errors up
    if [ "$retval" != "0" ]; then
        return $retval
    fi
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
            # TODO reenable quiet once we have dealt with files being left
            # behind while purging in prepare
            eatmydata apt-get remove -y --purge -y "$@"
            ;;
        amazon-*|centos-7-*)
            quiet yum -y remove "$@"
            ;;
        fedora-*|centos-*)
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
            quiet eatmydata apt-get update
            ;;
        amazon-*|centos-7-*)
            quiet yum clean all
            quiet yum makecache
            ;;
        fedora-*|centos-*)
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
            quiet eatmydata apt-get clean
            ;;
        amazon-*|centos-7-*)
            yum clean all
            ;;
        fedora-*|centos-*)
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
            quiet eatmydata apt-get -y autoremove
            ;;
        amazon-*|centos-7-*)
            quiet yum -y autoremove
            ;;
        fedora-*|centos-*)
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
        amazon-*|centos-7-*)
            yum info "$1"
            ;;
        fedora-*|centos-*)
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
        if os.query is-ubuntu-ge 23.10; then
            apt install -y --only-upgrade -t "$(lsb_release -c -s)-proposed" snapd
        else
            apt install -y --only-upgrade snapd
        fi
        mv sources.list.back /etc/apt/sources.list
        apt update

        # On trusty we may pull in a new hwe-kernel that is needed to run the
        # snapd tests. We need to reboot to actually run this kernel.
        if os.query is-trusty && [ "$SPREAD_REBOOT" = 0 ]; then
            REBOOT
        fi
    elif [ -n "$PPA_GPG_KEY" ] && [ -n "$PPA_SOURCE_LINE" ]; then
        echo "$PPA_GPG_KEY" | apt-key add -
        echo "${PPA_SOURCE_LINE//"YOUR_UBUNTU_VERSION_HERE"/"$(lsb_release -c -s)"}" >> /etc/apt/sources.list
        apt update
        apt install -y snapd

        # Double check that it really comes from the PPA
        apt show snapd | MATCH "APT-Sources: http.*private-ppa\.launchpad(content)?\.net"
    elif [ -n "$PPA_VALIDATION_NAME" ]; then
        apt install -y snapd
        add-apt-repository -y "$PPA_VALIDATION_NAME"
        apt update
        apt install -y --only-upgrade snapd

        # Double check that it really comes from the PPA
        apt show snapd | MATCH "APT-Sources: http.*ppa\.launchpad(content)?\.net"

        add-apt-repository --remove "$PPA_VALIDATION_NAME"
        apt update
    else
        packages=
        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snapd_*.deb
                ;;
            fedora-*|amazon-*|centos-*)
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snap-confine*.rpm\ "${GOPATH%%:*}"/snapd*.rpm
                ;;
            opensuse-*)
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snapd*.rpm
                ;;
            arch-*)
                # shellcheck disable=SC2125
                packages="${GOHOME}"/snapd*.pkg.tar.*
                ;;
            *)
                exit 1
                ;;
        esac

        # shellcheck disable=SC2086
        distro_install_local_package $packages

        case "$SPREAD_SYSTEM" in
            fedora-*|centos-*)
                # We need to wait until the man db cache is updated before do daemon-reexec
                # Otherwise the service fails and the system will be degraded during tests executions
                for i in $(seq 20); do
                    if ! systemctl is-active run-*.service; then
                        break
                    fi
                    sleep .5
                done

                # systemd caches SELinux policy data and subsequently attempts
                # to create sockets with incorrect context, this installation of
                # socket activated snaps fails, see:
                # https://bugzilla.redhat.com/show_bug.cgi?id=1660141
                # https://bugzilla.redhat.com/show_bug.cgi?id=1197886
                # https://github.com/systemd/systemd/issues/9997
                systemctl daemon-reexec
                ;;
        esac
        case "$SPREAD_SYSTEM" in
            fedora-*)
                # the problem with SELinux policy also affects the user instance
                # in 248, see:
                # https://bugzilla.redhat.com/show_bug.cgi?id=1960576
                # note, this fixes it for the root user only, the test user
                # session is created dynamically as needed
                systemctl --user daemon-reexec
                ;;
        esac

        if os.query is-arch-linux; then
            # Arch policy does not allow calling daemon-reloads in package
            # install scripts
            systemctl daemon-reload

            # AppArmor policy needs to be reloaded
            if systemctl show -p ActiveState apparmor.service | MATCH 'ActiveState=active'; then
                systemctl restart apparmor.service
            fi
        fi

        if os.query is-opensuse || os.query is-arch-linux; then
            # Package installation applies vendor presets only, which leaves
            # snapd.apparmor disabled.
            systemctl enable --now snapd.apparmor.service
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
        fedora-*|opensuse-*|amazon-*|centos-*)
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
        python3
        autoconf
        automake
        autotools-dev
        build-essential
        ca-certificates
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
        mtools
        netcat-openbsd
        pkg-config
        python3-docutils
        udev
        udisks2
        upower
        uuid-runtime
        "
}

pkg_dependencies_ubuntu_classic(){
    echo "
        avahi-daemon
        cups
        fish
        fontconfig
        gnome-keyring
        jq
        man
        nfs-kernel-server
        printer-driver-cups-pdf
        python3-dbus
        python3-gi
        python3-yaml
        upower
        weston        
        xdg-user-dirs
        xdg-utils
        xxd
        zsh
        "

    case "$SPREAD_SYSTEM" in
        ubuntu-14.04-*)
            pkg_linux_image_extra
            ;;
        ubuntu-16.04-64)
            echo "
                dbus-user-session
                evolution-data-server
                fwupd
                gccgo-6
                gnome-online-accounts
                kpartx
                libvirt-bin
                packagekit
                qemu
                x11-utils
                xvfb
                "
                pkg_linux_image_extra
            ;;
        ubuntu-18.04-32)
            echo "
                dbus-user-session
                gccgo-6
                evolution-data-server
                fwupd
                gnome-online-accounts
                packagekit
                "
                pkg_linux_image_extra
            ;;
        ubuntu-18.04-64)
            echo "
                dbus-user-session
                gccgo-8
                gperf
                evolution-data-server
                fwupd
                packagekit
                qemu-utils
                "
            ;;
        ubuntu-20.04-64|ubuntu-20.04-arm-64)
            # bpftool is part of linux-tools package
            echo "
                dbus-user-session
                evolution-data-server
                fwupd
                gccgo-9
                libvirt-daemon-system
                linux-tools-$(uname -r)
                packagekit
                qemu-kvm
                qemu-utils
                shellcheck
                "
            ;;
        ubuntu-22.*|ubuntu-23.*|ubuntu-24.*)
            # bpftool is part of linux-tools package
            echo "
                dbus-user-session
                fwupd
                golang
                libvirt-daemon-system
                linux-tools-$(uname -r)
                lz4
                qemu-kvm
                qemu-utils
                "
            ;;
        ubuntu-*)
            echo "
                squashfs-tools
                "
            ;;
        debian-*)
            echo "
                autopkgtest
                bpftool
                cryptsetup-bin
                debootstrap
                eatmydata
                evolution-data-server
                fwupd
                gcc-multilib
                libc6-dev-i386
                linux-libc-dev
                lsof
                net-tools
                packagekit
                sbuild
                schroot
                strace
                systemd-timesyncd
                "
            ;;
    esac
}

pkg_linux_image_extra (){
    if apt-cache show "linux-image-extra-$(uname -r)" > /dev/null 2>&1; then
        echo "linux-image-extra-$(uname -r)";
    else
        if apt-cache show "linux-modules-extra-$(uname -r)" > /dev/null 2>&1; then
            echo "linux-modules-extra-$(uname -r)";
        else
            echo "cannot find a matching kernel modules package";
            exit 1;
        fi;
    fi
}

pkg_dependencies_ubuntu_core(){
    echo "
        pollinate
        "
        pkg_linux_image_extra
}

pkg_dependencies_fedora_centos_common(){
    echo "
        python3
        bpftool
        clang
        curl
        dbus-x11
        evolution-data-server
        expect
        fontconfig
        fwupd
        git
        golang
        jq
        iptables-services
        man
        net-tools
        nmap-ncat
        nfs-utils
        PackageKit
        polkit
        python3-yaml
        python3-dbus
        python3-gobject
        rpm-build
        udisks2
        upower
        xdg-user-dirs
        xdg-utils
        strace
        zsh
        "
    if ! os.query is-centos 9; then
        echo "
            fish
            redhat-lsb-core
        "
    fi
}

pkg_dependencies_fedora(){
    echo "
         libcap-static
        "
}

pkg_dependencies_amazon(){
    if os.query is-amazon-linux 2 || os.query is-centos 7; then
        echo "
            fish
            fwupd
            system-lsb-core
            upower
            "
    fi
    if os.query is-amazon-linux 2023; then
        echo "
            bpftool
            gpg
            python-docutils
            python3-gobject
            "
    fi
    echo "
        dbus-x11
        expect
        fontconfig
        git
        golang
        grub2-tools
        jq
        iptables-services
        libcap-static
        man
        nc
        net-tools
        nfs-utils
        PackageKit
        python3
        rpm-build
        xdg-user-dirs
        xdg-utils
        udisks2
        zsh
        "
}

pkg_dependencies_opensuse(){
    echo "
        python3
        apparmor-profiles
        audit
        bash-completion
        bpftool
        clang
        curl
        dbus-1-python3
        evolution-data-server
        expect
        fish
        fontconfig
        fwupd
        git
        golang-packaging
        iptables
        jq
        lsb-release
        man
        man-pages
        nfs-kernel-server
        nss-mdns
        osc
        PackageKit
        python3-yaml
        strace
        netcat-openbsd
        rpm-build
        udisks2
        upower
        uuidd
        xdg-user-dirs
        xdg-utils
        zsh
        "
    if os.query is-opensuse tumbleweed; then
        echo "
            libfwupd2
        "
    fi
}

pkg_dependencies_arch(){
    echo "
    apparmor
    autoconf-archive
    base-devel
    bash-completion
    bpf
    clang
    curl
    evolution-data-server
    expect
    fish
    fontconfig
    fwupd
    git
    go
    go-tools
    jq
    libseccomp
    libcap
    libx11
    man
    net-tools
    nfs-utils
    openbsd-netcat
    packagekit
    python
    python-docutils
    python-dbus
    python-gobject
    python3-yaml
    squashfs-tools
    shellcheck
    strace
    udisks2
    upower
    xdg-user-dirs
    xdg-utils
    xfsprogs
    zsh
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
        amazon-*|centos-7-*)
            pkg_dependencies_amazon
            ;;
        centos-*)
            pkg_dependencies_fedora_centos_common
            ;;
        fedora-*)
            pkg_dependencies_fedora_centos_common
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
    distro_install_package --no-install-recommends $pkgs
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
