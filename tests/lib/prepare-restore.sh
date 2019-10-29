#!/bin/bash
set -x
# NOTE: We must set -e so that any failures coming out of the various
# statements we execute stops the build. The code is not (yet) written to
# handle errors in general.
set -e

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
            debian-*|fedora-*|opensuse-*|arch-*|amazon-*|centos-*)
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
    rpm_dir=$(rpm --eval "%_topdir")
    pack_args=
    case "$SPREAD_SYSTEM" in
        fedora-*|amazon-*|centos-*)
            ;;
        opensuse-*)
            # use bundled snapd*.vendor.tar.xz archive
            pack_args=-s
            ;;
        *)
            echo "ERROR: RPM build for system $SPREAD_SYSTEM is not yet supported"
            exit 1
    esac

    sed -i -e "s/^Version:.*$/Version: $version/g" "$packaging_path/snapd.spec"

    # Create a source tarball for the current snapd sources
    mkdir -p "$rpm_dir/SOURCES"
    cp "$packaging_path"/* "$rpm_dir/SOURCES/"
    # shellcheck disable=SC2086
    ./packaging/pack-source -v "$version" -o "$rpm_dir/SOURCES" $pack_args

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

    find "$rpm_dir"/RPMS -name '*.rpm' -exec cp -v {} "${GOPATH%%:*}" \;
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

    cp /tmp/pkg/snapd*.pkg.tar.xz "${GOPATH%%:*}"
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
    if [[ "$SPREAD_SYSTEM" == ubuntu-* ]] && [[ "$SPREAD_SYSTEM" != ubuntu-core-* ]]; then
        apt-get remove --purge -y lxd lxcfs || true
        apt-get autoremove --purge -y
        lxd-tool undo-lxd-mount-changes
    fi

    # Check if running inside a container.
    # The testsuite will not work in such an environment
    if systemd-detect-virt -c; then
        echo "Tests cannot run inside a container"
        exit 1
    fi

    # no need to modify anything further for autopkgtest
    # we want to run as pristine as possible
    if [ "$SPREAD_BACKEND" = autopkgtest ]; then
        exit 0
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

    if ! distro_update_package_db; then
        echo "Error updating the package db, continue with the system preparation"
    fi

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

    # debian-sid packaging is special
    if [[ "$SPREAD_SYSTEM" == debian-sid-* ]]; then
        if [ ! -d packaging/debian-sid ]; then
            echo "no packaging/debian-sid/ directory "
            echo "broken test setup"
            exit 1
        fi

        # remove etckeeper
        apt purge -y etckeeper

        # debian has its own packaging
        rm -f debian
        # the debian dir must be a real dir, a symlink will make
        # dpkg-buildpackage choke later.
        mv packaging/debian-sid debian

        # get the build-deps
        apt build-dep -y ./

        # and ensure we don't take any of the vendor deps
        rm -rf vendor/*/

        # and create a fake upstream tarball
        tar -c -z -f ../snapd_"$(dpkg-parsechangelog --show-field Version|cut -d- -f1)".orig.tar.gz --exclude=./debian --exclude=./.git .

        # and build a source package - this will be used during the sbuild test
        dpkg-buildpackage -S --no-sign
    fi

    # so is ubuntu-14.04
    if [[ "$SPREAD_SYSTEM" == ubuntu-14.04-* ]]; then
        if [ ! -d packaging/ubuntu-14.04 ]; then
            echo "no packaging/ubuntu-14.04/ directory "
            echo "broken test setup"
            exit 1
        fi

        # 14.04 has its own packaging
        ./generate-packaging-dir

        quiet eatmydata apt-get install -y software-properties-common

	# FIXME: trusty-proposed disabled because there is an inconsistency
	#        in the trusty-proposed archive:
	# linux-generic-lts-xenial : Depends: linux-image-generic-lts-xenial (= 4.4.0.143.124) but 4.4.0.141.121 is to be installed
        #echo 'deb http://archive.ubuntu.com/ubuntu/ trusty-proposed main universe' >> /etc/apt/sources.list
        quiet add-apt-repository ppa:snappy-dev/image
        quiet eatmydata apt-get update

        quiet eatmydata apt-get install -y --install-recommends linux-generic-lts-xenial
        quiet eatmydata apt-get install -y --force-yes apparmor libapparmor1 seccomp libseccomp2 systemd cgroup-lite util-linux
    fi

    # WORKAROUND for older postrm scripts that did not do
    # "rm -rf /var/cache/snapd"
    rm -rf /var/cache/snapd/aux
    case "$SPREAD_SYSTEM" in
        ubuntu-*)
            # Ubuntu is the only system where snapd is preinstalled
            distro_purge_package snapd
            ;;
        *)
            # snapd state directory must not exist when the package is not
            # installed
            test ! -d /var/lib/snapd
            ;;
    esac

    if ! install_pkg_dependencies; then
        echo "Error installing test dependencies, continue with the system preparation"
    fi

    # We take a special case for Debian/Ubuntu where we install additional build deps
    # base on the packaging. In Fedora/Suse this is handled via mock/osc
    case "$SPREAD_SYSTEM" in
        debian-*|ubuntu-*)
            # in 16.04: apt build-dep -y ./
            if [[ "$SPREAD_SYSTEM" == debian-9-* ]]; then
                best_golang="$(python3 ./tests/lib/best_golang.py)"
                test -n "$best_golang"
                sed -i -e "s/golang-1.10/$best_golang/" ./debian/control
            else
                best_golang=golang-1.10
            fi
            gdebi --quiet --apt-line ./debian/control | quiet xargs -r eatmydata apt-get install -y
            # The go 1.10 backport is not using alternatives or anything else so
            # we need to get it on path somehow. This is not perfect but simple.
            if [ -z "$(command -v go)" ]; then
                # the path filesystem path is: /usr/lib/go-1.10/bin
                ln -s "/usr/lib/${best_golang/lang/}/bin/go" /usr/bin/go
            fi
            ;;
    esac

    # update vendoring
    if [ -z "$(command -v govendor)" ]; then
        rm -rf "${GOPATH%%:*}/src/github.com/kardianos/govendor"
        go get -u github.com/kardianos/govendor
    fi
    # Retry govendor sync to minimize the number of connection errors during the sync
    for _ in $(seq 10); do
        if quiet govendor sync; then
            break
        fi
        sleep 1
    done
    # govendor runs as root and will leave strange permissions
    chown test.test -R "$SPREAD_PATH"

    if [ -z "$SNAPD_PUBLISHED_VERSION" ]; then
        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                build_deb
                ;;
            fedora-*|opensuse-*|amazon-*|centos-*)
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

    # On core systems, the journal service is configured once the final core system
    # is created and booted what is done during the first test suite preparation
    if is_classic_system; then
        # shellcheck source=tests/lib/prepare.sh
        . "$TESTSLIB"/prepare.sh
        disable_journald_rate_limiting
    fi
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

install_snap_profiler(){
    echo "install snaps profiler"

    if [ "$PROFILE_SNAPS" = 1 ]; then
        profiler_snap="$(get_snap_for_system test-snapd-profiler)"
        rm -f "/var/snap/${profiler_snap}/common/profiler.log"
        snap install "${profiler_snap}"
        snap connect "${profiler_snap}":system-observe
    fi
}

prepare_suite_each() {
    # back test directory to be restored during the restore
    tar cf "${PWD}.tar" "$PWD"

    # WORKAROUND for memleak https://github.com/systemd/systemd/issues/11502
    if [[ "$SPREAD_SYSTEM" == debian-sid* ]]; then
        systemctl restart systemd-journald
    fi

    # save the job which is going to be executed in the system
    echo -n "$SPREAD_JOB " >> "$RUNTIME_STATE_PATH/runs"
    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --reuse-core
    # Restart journal log and reset systemd journal cursor.
    # It is not being used the restart command to avoid reaching the start-limit
    systemctl stop systemd-journald.service
    sync
    systemctl start systemd-journald.service
    start_new_journalctl_log

    echo "Install the snaps profiler snap"
    install_snap_profiler

    # shellcheck source=tests/lib/prepare.sh
    . "$TESTSLIB"/prepare.sh
    if is_classic_system; then
        prepare_each_classic
    fi
    # Check if journalctl is ready to run the test
    check_journalctl_ready

    case "$SPREAD_SYSTEM" in
        fedora-*|centos-*|amazon-*)
            ausearch -i -m AVC --checkpoint "$RUNTIME_STATE_PATH/audit-stamp" || true
            ;;
    esac
}

restore_suite_each() {
    rm -f "$RUNTIME_STATE_PATH/audit-stamp"

    # restore test directory saved during prepare
    if [ -f "${PWD}.tar" ]; then
        rm -rf "$PWD"
        tar -C/ -xf "${PWD}.tar"
        rm -rf "${PWD}.tar"
    fi

    if [ "$PROFILE_SNAPS" = 1 ]; then
        echo "Save snaps profiler log"
        local logs_id logs_dir logs_file
        logs_dir="$RUNTIME_STATE_PATH/logs"
        logs_id=$(find "$logs_dir" -maxdepth 1 -name '*.journal.log' | wc -l)
        logs_file=$(echo "${logs_id}_${SPREAD_JOB}" | tr '/' '_' | tr ':' '__')

        profiler_snap="$(get_snap_for_system test-snapd-profiler)"

        mkdir -p "$logs_dir"
        if [ -e "/var/snap/${profiler_snap}/common/profiler.log" ]; then
            cp -f "/var/snap/${profiler_snap}/common/profiler.log" "${logs_dir}/${logs_file}.profiler.log"
        fi
        get_journalctl_log > "${logs_dir}/${logs_file}.journal.log"
    fi

    # On Arch it seems that using sudo / su for working with the test user
    # spawns the /run/user/12345 tmpfs for XDG_RUNTIME_DIR which asynchronously
    # cleans up itself sometime after the test but not instantly, leading to
    # random failures in the mount leak detector. Give it a moment but don't
    # clean it up ourselves, this should report actual test errors, if any.
    for i in $(seq 10); do
        if not mountinfo-tool /run/user/12345 .fs_type=tmpfs; then
            break
        fi
        sleep 1
    done
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

    if getent passwd snap_daemon; then
        echo "Test left the snap_daemon user behind, this should not happen"
        exit 1
    fi
    if getent group snap_daemon; then
        echo "Test left the snap_daemon group behind, this should not happen"
        exit 1
    fi

    # Something is hosing the filesystem so look for signs of that
    not grep -F "//deleted /etc" /proc/self/mountinfo

    if journalctl -u snapd.service | grep -F "signal: terminated"; then
        exit 1;
    fi

    case "$SPREAD_SYSTEM" in
        fedora-*|centos-*)
            # Make sure that we are not leaving behind incorrectly labeled snap
            # files on systems supporting SELinux
            (
                find /root/snap -printf '%Z\t%H/%P\n' || true
                find /home -regex '/home/[^/]*/snap\(/.*\)?' -printf '%Z\t%H/%P\n' || true
            ) | grep -c -v snappy_home_t | MATCH "0"

            find /var/snap -printf '%Z\t%H/%P\n' | grep -c -v snappy_var_t  | MATCH "0"
            ;;
    esac
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
