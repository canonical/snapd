#!/bin/bash
set -x
# NOTE: We must set -e so that any failures coming out of the various
# statements we execute stops the build. The code is not (yet) written to
# handle errors in general.
set -e
# Set pipefail option so that "foo | bar" behaves with fewer surprises by
# failing if foo fails, not just if bar fails.
set -o pipefail

# shellcheck source=tests/lib/quiet.sh
. "$TESTSLIB/quiet.sh"

# XXX: boot.sh has side-effects
# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"

# XXX: dirs.sh has side-effects
# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

# shellcheck source=tests/lib/random.sh
. "$TESTSLIB/random.sh"

# shellcheck source=tests/lib/spread-funcs.sh
. "$TESTSLIB/spread-funcs.sh"

# shellcheck source=tests/lib/journalctl.sh
. "$TESTSLIB/journalctl.sh"

# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"

# shellcheck source=tests/lib/systems.sh
. "$TESTSLIB/systems.sh"


###
### Utility functions reused below.
###

create_test_user(){
   if ! id test >& /dev/null; then
        quiet groupadd --gid 12345 test
        case "$SPREAD_SYSTEM" in
            ubuntu-*)
                # manually setting the UID and GID to 12345 because we need to
                # know the numbers match for when we set up the user inside
                # the all-snap, which has its own user & group database.
                # Nothing special about 12345 beyond it being high enough it's
                # unlikely to ever clash with anything, and easy to remember.
                quiet adduser --uid 12345 --gid 12345 --disabled-password --gecos '' test
                ;;
            debian-*|fedora-*|opensuse-*|arch-*|amazon-*)
                quiet useradd -m --uid 12345 --gid 12345 test
                ;;
            *)
                echo "ERROR: system $SPREAD_SYSTEM not yet supported!"
                exit 1
        esac
    fi

    owner=$( stat -c "%U:%G" /home/test )
    if [ "$owner" != "test:test" ]; then
        echo "expected /home/test to be test:test but it's $owner"
        exit 1
    fi
    unset owner

    echo 'test ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers

    chown test.test -R "$SPREAD_PATH"
    chown test.test "$SPREAD_PATH/../"
}

build_deb(){
    # Use fake version to ensure we are always bigger than anything else
    dch --newversion "1337.$(dpkg-parsechangelog --show-field Version)" "testing build"

    su -l -c "cd $PWD && DEB_BUILD_OPTIONS='nocheck testkeys' dpkg-buildpackage -tc -b -Zgzip" test
    # put our debs to a safe place
    cp ../*.deb "$GOHOME"
}

build_rpm() {
    distro=$(echo "$SPREAD_SYSTEM" | awk '{split($0,a,"-");print a[1]}')
    release=$(echo "$SPREAD_SYSTEM" | awk '{split($0,a,"-");print a[2]}')
    if [[ "$SPREAD_SYSTEM" == amazon-linux-2-* ]]; then
        distro=amzn
        release=2
    fi
    arch=x86_64
    base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
    version="1337.$base_version"
    packaging_path=packaging/$distro-$release
    archive_name=snapd-$version.tar.gz
    archive_compression=z
    extra_tar_args=
    rpm_dir=$(rpm --eval "%_topdir")

    case "$SPREAD_SYSTEM" in
        fedora-*|amazon-*)
            extra_tar_args="$extra_tar_args --exclude=vendor/*"
            ;;
        opensuse-*)
            archive_name=snapd_$version.vendor.tar.xz
            archive_compression=J
            ;;
        *)
            echo "ERROR: RPM build for system $SPREAD_SYSTEM is not yet supported"
            exit 1
    esac

    sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"

    # Create a source tarball for the current snapd sources
    mkdir -p "/tmp/pkg/snapd-$version"
    cp -ra -- * "/tmp/pkg/snapd-$version/"
    mkdir -p "$rpm_dir/SOURCES"
    # shellcheck disable=SC2086
    (cd /tmp/pkg && tar "-c${archive_compression}f" "$rpm_dir/SOURCES/$archive_name" $extra_tar_args "snapd-$version")
    if [[ "$SPREAD_SYSTEM" == amazon-linux-2-* ]]; then
        # need to build the vendor tree
        (cd /tmp/pkg && tar "-cJf" "$rpm_dir/SOURCES/snapd_${version}.only-vendor.tar.xz" "snapd-$version/vendor")
    fi
    cp "$packaging_path"/* "$rpm_dir/SOURCES/"

    # Cleanup all artifacts from previous builds
    rm -rf "$rpm_dir"/BUILD/*

    # Build our source package
    rpmbuild --with testkeys -bs "$packaging_path/snapd.spec"

    # .. and we need all necessary build dependencies available
    deps=()
    IFS=$'\n'
    for dep in $(rpm -qpR "$rpm_dir"/SRPMS/snapd-1337.*.src.rpm); do
      if [[ "$dep" = rpmlib* ]]; then
         continue
      fi
      deps+=("$dep")
    done
    distro_install_package "${deps[@]}"

    # And now build our binary package
    rpmbuild \
        --with testkeys \
        --nocheck \
        -ba \
        "$packaging_path/snapd.spec"

    cp "$rpm_dir"/RPMS/$arch/snap*.rpm "$GOPATH"
    if [[ "$SPREAD_SYSTEM" = fedora-* ]]; then
        # On Fedora we have an additional package for SELinux
        cp "$rpm_dir"/RPMS/noarch/snap*.rpm "$GOPATH"
    fi
}

build_arch_pkg() {
    base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
    version="1337.$base_version"
    packaging_path=packaging/arch
    archive_name=snapd-$version.tar

    rm -rf /tmp/pkg
    mkdir -p /tmp/pkg/sources/snapd
    cp -ra -- * /tmp/pkg/sources/snapd/

    # shellcheck disable=SC2086
    tar -C /tmp/pkg/sources -cf "/tmp/pkg/$archive_name" "snapd"
    cp "$packaging_path"/* "/tmp/pkg"

    # fixup PKGBUILD which builds a package named snapd-git with dynamic version
    #  - update pkgname to use snapd
    #  - kill dynamic version
    #  - packaging functions are named package_<pkgname>(), update it to package_snapd()
    #  - update source path to point to local archive instead of git
    #  - fix package version to $version
    sed -i \
        -e "s/^source=.*/source=(\"$archive_name\")/" \
        -e "s/pkgname=snapd.*/pkgname=snapd/" \
        -e "s/pkgver=.*/pkgver=$version/" \
        -e "s/package_snapd-git()/package_snapd()/" \
        /tmp/pkg/PKGBUILD
    # comment out automatic package version update block `pkgver() { ... }` as
    # it's only useful when building the package manually
    awk '
    /BEGIN/ { strip = 0; last = 0 }
    /pkgver\(\)/ { strip = 1 }
    /^}/ { if (strip) last = 1 }
    // { if (strip) { print "#" $0; if (last) { last = 0; strip = 0}} else { print $0}}
    ' < /tmp/pkg/PKGBUILD > /tmp/pkg/PKGBUILD.tmp
    mv /tmp/pkg/PKGBUILD.tmp /tmp/pkg/PKGBUILD

    chown -R test:test /tmp/pkg
    su -l -c "cd /tmp/pkg && WITH_TEST_KEYS=1 makepkg -f --nocheck" test

    cp /tmp/pkg/snapd*.pkg.tar.xz "$GOPATH"
}

download_from_published(){
    local published_version="$1"

    curl -s -o pkg_page "https://launchpad.net/ubuntu/+source/snapd/$published_version"

    arch=$(dpkg --print-architecture)
    build_id=$(sed -n 's|<a href="/ubuntu/+source/snapd/'"$published_version"'/+build/\(.*\)">'"$arch"'</a>|\1|p' pkg_page | sed -e 's/^[[:space:]]*//')

    # we need to download snap-confine and ubuntu-core-launcher for versions < 2.23
    for pkg in snapd snap-confine ubuntu-core-launcher; do
        file="${pkg}_${published_version}_${arch}.deb"
        curl -L -o "$GOHOME/$file" "https://launchpad.net/ubuntu/+source/snapd/${published_version}/+build/${build_id}/+files/${file}"
    done
}

install_dependencies_from_published(){
    local published_version="$1"

    for dep in snap-confine ubuntu-core-launcher; do
        dpkg -i "$GOHOME/${dep}_${published_version}_$(dpkg --print-architecture).deb"
    done
}

###
### Prepare / restore functions for {project,suite}
###

prepare_project() {
    # Check if running inside a container.
    # The testsuite will not work in such an environment
    if systemd-detect-virt -c; then
        echo "Tests cannot run inside a container"
        exit 1
    fi

    # Set REUSE_PROJECT to reuse the previous prepare when also reusing the server.
    [ "$REUSE_PROJECT" != 1 ] || exit 0
    echo "Running with SNAP_REEXEC: $SNAP_REEXEC"

    # check that we are not updating
    if [ "$(bootenv snap_mode)" = "try" ]; then
        echo "Ongoing reboot upgrade process, please try again when finished"
        exit 1
    fi

    # Prepare the state directories for execution
    prepare_state

    # declare the "quiet" wrapper

    if [ "$SPREAD_BACKEND" = external ]; then
        chown test.test -R "$PROJECT_PATH"
        exit 0
    fi

    if [ "$SPREAD_BACKEND" = qemu ]; then
        if [ -d /etc/apt/apt.conf.d ]; then
            # qemu images may be built with pre-baked proxy settings that can be wrong
            rm -f /etc/apt/apt.conf.d/90cloud-init-aptproxy
            rm -f /etc/apt/apt.conf.d/99proxy
            if [ -n "${HTTP_PROXY:-}" ]; then
                printf 'Acquire::http::Proxy "%s";\n' "$HTTP_PROXY" >> /etc/apt/apt.conf.d/99proxy
            fi
            if [ -n "${HTTPS_PROXY:-}" ]; then
                printf 'Acquire::https::Proxy "%s";\n' "$HTTPS_PROXY" >> /etc/apt/apt.conf.d/99proxy
            fi
        fi
        if [ -f /etc/dnf/dnf.conf ]; then
            if [ -n "${HTTP_PROXY:-}" ]; then
                echo "proxy=$HTTP_PROXY" >> /etc/dnf/dnf.conf
            fi
        fi
        # TODO: zypper proxy, yum proxy
    fi

    create_test_user

    distro_update_package_db

    if [[ "$SPREAD_SYSTEM" == arch-* ]]; then
        # perform system upgrade on Arch so that we run with most recent kernel
        # and userspace
        if [[ "$SPREAD_REBOOT" == 0 ]]; then
            if distro_upgrade | MATCH "reboot"; then
                echo "system upgraded, reboot required"
                REBOOT
            fi
            # arch uses a single kernel package which could have gotten updated
            # just now, reboot in case we're still on the old kernel
            if [ ! -d "/lib/modules/$(uname -r)" ]; then
                echo "rebooting to new kernel"
                REBOOT
            fi
        fi
        # double check we are running the installed kernel
        # NOTE: LOCALVERSION is set by scripts/setlocalversion and loos like
        # 4.17.11-arch1, since this may not match pacman -Qi output, we'll list
        # the files within the package instead
        # pacman -Ql linux output:
        # ...
        # linux /usr/lib/modules/4.17.11-arch1/modules.alias
        if [[ "$(pacman -Ql linux | cut -f2 -d' ' |grep '/usr/lib/modules/.*/modules'|cut -f5 -d/ | uniq)" != "$(uname -r)" ]]; then
            echo "running unexpected kernel version $(uname -r)"
            exit 1
        fi
    fi

    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        if [ ! -d packaging/ubuntu-14.04 ]; then
            echo "no packaging/ubuntu-14.04/ directory "
            echo "broken test setup"
            exit 1
        fi

        # 14.04 has its own packaging
        ./generate-packaging-dir

        quiet apt-get install -y software-properties-common

        echo 'deb http://archive.ubuntu.com/ubuntu/ trusty-proposed main universe' >> /etc/apt/sources.list
        quiet add-apt-repository ppa:snappy-dev/image
        quiet apt-get update

        quiet apt-get install -y --install-recommends linux-generic-lts-xenial
        quiet apt-get install -y --force-yes apparmor libapparmor1 seccomp libseccomp2 systemd cgroup-lite util-linux
    fi

    distro_purge_package snapd || true
    install_pkg_dependencies

    # We take a special case for Debian/Ubuntu where we install additional build deps
    # base on the packaging. In Fedora/Suse this is handled via mock/osc
    case "$SPREAD_SYSTEM" in
        debian-*|ubuntu-*)
            # in 16.04: apt build-dep -y ./
            gdebi --quiet --apt-line ./debian/control | quiet xargs -r apt-get install -y
            ;;
    esac

    # update vendoring
    if [ -z "$(command -v govendor)" ]; then
        rm -rf "$GOPATH/src/github.com/kardianos/govendor"
        go get -u github.com/kardianos/govendor
    fi
    quiet govendor sync
    # govendor runs as root and will leave strange permissions
    chown test.test -R "$SPREAD_PATH"

    if [ -z "$SNAPD_PUBLISHED_VERSION" ]; then
        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                build_deb
                ;;
            fedora-*|opensuse-*|amazon-*)
                build_rpm
                ;;
            arch-*)
                build_arch_pkg
                ;;
            *)
                echo "ERROR: No build instructions available for system $SPREAD_SYSTEM"
                exit 1
                ;;
        esac
    else
        download_from_published "$SNAPD_PUBLISHED_VERSION"
        install_dependencies_from_published "$SNAPD_PUBLISHED_VERSION"
    fi

    # Build fakestore.
    fakestore_tags=
    if [ "$REMOTE_STORE" = staging ]; then
        fakestore_tags="-tags withstagingkeys"
    fi

    # eval to prevent expansion errors on opensuse (the variable keeps quotes)
    eval "go get $fakestore_tags ./tests/lib/fakestore/cmd/fakestore"

    # Build additional utilities we need for testing
    go get ./tests/lib/fakedevicesvc
    go get ./tests/lib/systemd-escape

    # Disable journald rate limiting
    mkdir -p /etc/systemd/journald.conf.d
    # The RateLimitIntervalSec key is not supported on some systemd versions causing
    # the journal rate limit could be considered as not valid and discarded in concecuence.
    # RateLimitInterval key is supported in old systemd versions and in new ones as well,
    # maintaining backward compatibility.
    cat <<-EOF > /etc/systemd/journald.conf.d/no-rate-limit.conf
    [Journal]
    RateLimitInterval=0
    RateLimitBurst=0
EOF
    systemctl restart systemd-journald.service
}

prepare_project_each() {
    # Clear the kernel ring buffer.
    dmesg -c > /dev/null

    fixup_dev_random
}

prepare_suite() {
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if is_core_system; then
        prepare_ubuntu_core
    else
        prepare_classic
    fi
}

prepare_suite_each() {
    # save the job which is going to be exeuted in the system
    echo -n "$SPREAD_JOB " >> "$RUNTIME_STATE_PATH/runs"
    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --reuse-core
    # Reset systemd journal cursor.
    start_new_journalctl_log
    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if is_classic_system; then
        prepare_each_classic
    fi
    # Check if journalctl is ready to run the test
    check_journalctl_ready
}

restore_suite_each() {
    true
}

restore_suite() {
    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --store
    if is_classic_system; then
        # shellcheck source=tests/lib/pkgdb.sh
        . "$TESTSLIB"/pkgdb.sh
        distro_purge_package snapd
        if [[ "$SPREAD_SYSTEM" != opensuse-* && "$SPREAD_SYSTEM" != arch-* ]]; then
            # A snap-confine package never existed on openSUSE or Arch
            distro_purge_package snap-confine
        fi
    fi
}

restore_project_each() {
    restore_dev_random

    # Udev rules are notoriously hard to write and seemingly correct but subtly
    # wrong rules can pass review. Whenever that happens udev logs an error
    # message. As a last resort from lack of a better mechanism we can try to
    # pick up such errors.
    if grep "invalid .*snap.*.rules" /var/log/syslog; then
        echo "Invalid udev file detected, test most likely broke it"
        exit 1
    fi

    # Check if the OOM killer got invoked - if that is the case our tests
    # will most likely not function correctly anymore. It looks like this
    # happens with: https://forum.snapcraft.io/t/4101 and is a source of
    # failure in the autopkgtest environment.
    if dmesg|grep "oom-killer"; then
        echo "oom-killer got invoked during the tests, this should not happen."
        echo "Dmesg debug output:"
        dmesg
        echo "Meminfo debug output:"
        cat /proc/meminfo
        exit 1
    fi

    # check if there is a shutdown pending, no test should trigger this
    # and it leads to very confusing test failures
    if [ -e /run/systemd/shutdown/scheduled ]; then
        echo "Test triggered a shutdown, this should not happen"
        snap changes
        exit 1
    fi

    # Check for kernel oops during the tests
    if dmesg|grep "Oops: "; then
        echo "A kernel oops happened during the tests, test results will be unreliable"
        echo "Dmesg debug output:"
        dmesg
        exit 1
    fi
}

restore_project() {
    # Delete the snapd state used to accelerate prepare/restore code in certain suites. 
    delete_snapd_state

    # Remove all of the code we pushed and any build results. This removes
    # stale files and we cannot do incremental builds anyway so there's little
    # point in keeping them.
    if [ -n "$GOPATH" ]; then
        rm -rf "${GOPATH%%:*}"
    fi

    rm -rf /etc/systemd/journald.conf.d/no-rate-limit.conf
    rmdir /etc/systemd/journald.conf.d || true
}

case "$1" in
    --prepare-project)
        prepare_project
        ;;
    --prepare-project-each)
        prepare_project_each
        ;;
    --prepare-suite)
        prepare_suite
        ;;
    --prepare-suite-each)
        prepare_suite_each
        ;;
    --restore-suite-each)
        restore_suite_each
        ;;
    --restore-suite)
        restore_suite
        ;;
    --restore-project-each)
        restore_project_each
        ;;
    --restore-project)
        restore_project
        ;;
    *)
        echo "unsupported argument: $1"
        echo "try one of --{prepare,restore}-{project,suite}{,-each}"
        exit 1
        ;;
esac
