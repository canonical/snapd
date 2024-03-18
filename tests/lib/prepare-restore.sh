#!/bin/bash
set -x
# NOTE: We must set -e so that any failures coming out of the various
# statements we execute stops the build. The code is not (yet) written to
# handle errors in general.
set -e

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

# shellcheck source=tests/lib/random.sh
. "$TESTSLIB/random.sh"

# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"


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

        # Allow the test user to access systemd journal.
        if getent group systemd-journal >/dev/null; then
            usermod -G systemd-journal -a test
            id test | MATCH systemd-journal
        fi
    fi

    owner=$( stat -c "%U:%G" /home/test )
    if [ "$owner" != "test:test" ]; then
        echo "expected /home/test to be test:test but it's $owner"
        exit 1
    fi
    unset owner

    # Add a new line first to prevent an error which happens when
    # the file has not new line, and we see this:
    # syntax error, unexpected WORD, expecting END or ':' or '\n'
    echo >> /etc/sudoers
    echo 'test ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers

    chown test.test -R "$SPREAD_PATH"
    chown test.test "$SPREAD_PATH/../"
}

build_deb(){
    # Use fake version to ensure we are always bigger than anything else
    dch --newversion "1337.$(dpkg-parsechangelog --show-field Version)" "testing build"

    if os.query is-debian sid; then
        # ensure we really build without vendored packages
        mv ./vendor /tmp
    fi

    unshare -n -- \
            su -l -c "cd $PWD && DEB_BUILD_OPTIONS='nocheck testkeys' dpkg-buildpackage -tc -b -Zgzip -uc -us" test
    # put our debs to a safe place
    cp ../*.deb "$GOHOME"

    if os.query is-debian sid; then
        # restore vendor dir, it's needed by e.g. fakestore
        mv /tmp/vendor ./
    fi
}

build_rpm() {
    distro=$(echo "$SPREAD_SYSTEM" | awk '{split($0,a,"-");print a[1]}')
    release=$(echo "$SPREAD_SYSTEM" | awk '{split($0,a,"-");print a[2]}')
    if os.query is-amazon-linux 2; then
        distro=amzn
        release=2
    fi
    if os.query is-amazon-linux 2023; then
        distro=amzn
        release=2023
    fi
    arch=x86_64
    base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
    version="1337.$base_version"
    packaging_path=packaging/$distro-$release
    rpm_dir=$(rpm --eval "%_topdir")
    pack_args=
    case "$SPREAD_SYSTEM" in
        opensuse-*)
            # use bundled snapd*.vendor.tar.xz archive
            pack_args=-s
            ;;
        fedora-*|amazon-*|centos-*)
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
    unshare -n -- \
            rpmbuild --with testkeys -bs "$rpm_dir/SOURCES/snapd.spec"

    # .. and we need all necessary build dependencies available
    install_snapd_rpm_dependencies "$rpm_dir"/SRPMS/snapd-1337.*.src.rpm

    # And now build our binary package
    unshare -n -- \
            rpmbuild \
            --with testkeys \
            --nocheck \
            -ba \
            "$rpm_dir/SOURCES/snapd.spec"

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
    unshare -n -- \
            su -l -c "cd /tmp/pkg && WITH_TEST_KEYS=1 makepkg -f --nocheck" test

    # /etc/makepkg.conf defines PKGEXT which drives the compression alg and sets
    # the package file name extension, keep it simple and try a glob instead
    cp /tmp/pkg/snapd*.pkg.tar.* "${GOPATH%%:*}"
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

download_from_gce_bucket(){
    curl -o "${SPREAD_SYSTEM}.tar" "https://storage.googleapis.com/snapd-spread-tests/snapd-tests/packages/${SPREAD_SYSTEM}.tar"
    tar -xf "${SPREAD_SYSTEM}.tar" -C "$PROJECT_PATH"/..
}

install_dependencies_from_published(){
    local published_version="$1"

    for dep in snap-confine ubuntu-core-launcher; do
        dpkg -i "$GOHOME/${dep}_${published_version}_$(dpkg --print-architecture).deb"
    done
}

install_snapd_rpm_dependencies(){
    SRC_PATH=$1
    deps=()
    IFS=$'\n'
    for dep in $(rpm -qpR "$SRC_PATH"); do
        if [[ "$dep" = rpmlib* ]]; then
            continue
        fi
        deps+=("$dep")
    done
    distro_install_package "${deps[@]}"
}

install_dependencies_gce_bucket(){
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            cp "$PROJECT_PATH"/../*.deb "$GOHOME"
            ;;
        fedora-*|opensuse-*|amazon-*|centos-*)
            install_snapd_rpm_dependencies "$PROJECT_PATH"/../snapd-1337.*.src.rpm
            # sources are not needed to run the tests
            rm "$PROJECT_PATH"/../snapd-1337.*.src.rpm
            find "$PROJECT_PATH"/.. -name '*.rpm' -exec cp -v {} "${GOPATH%%:*}" \;
            ;;
        arch-*)
            cp "$PROJECT_PATH"/../snapd*.pkg.tar.* "${GOPATH%%:*}"
            ;;
    esac
}

###
### Prepare / restore functions for {project,suite}
###

prepare_project() {
    if os.query is-ubuntu && os.query is-classic; then
        apt-get remove --purge -y lxd lxcfs || true
        apt-get autoremove --purge -y
        "$TESTSTOOLS"/lxd-state undo-mount-changes
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
    if [ "$("$TESTSTOOLS"/boot-state bootenv show snap_mode)" = "try" ]; then
        echo "Ongoing reboot upgrade process, please try again when finished"
        exit 1
    fi

    # Prepare the state directories for execution
    prepare_state

    # declare the "quiet" wrapper

    if [ "$SPREAD_BACKEND" = "external" ]; then
        chown test.test -R "$PROJECT_PATH"
        exit 0
    fi

    if [ "$SPREAD_BACKEND" = "testflinger" ]; then
        adduser --uid 12345 --extrausers --quiet --disabled-password --gecos '' test
        echo test:ubuntu | sudo chpasswd
        echo 'test ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/create-user-test
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

    if os.query is-arch-linux; then
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
    if os.query is-debian sid; then
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
        dpkg-buildpackage -S -uc -us
    fi

    # so is ubuntu-14.04
    if os.query is-trusty; then
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

    # ubuntu-16.04 is EOL so the updated go-1.18 is only available via
    # the ppa:snappy-dev/image ppa for now. if needed the package could
    # be copied from the PPA to the ESM archive.
    if os.query is-xenial; then
        quiet add-apt-repository ppa:snappy-dev/image
        quiet eatmydata apt-get update
    fi

    # WORKAROUND for older postrm scripts that did not do
    # "rm -rf /var/cache/snapd"
    rm -rf /var/cache/snapd/aux
    case "$SPREAD_SYSTEM" in
        ubuntu-*)
            # Ubuntu is the only system where snapd is preinstalled, so we have
            # to purge it

            # first mask snapd.failure so that even if we kill snapd and it 
            # dies, snap-failure doesn't run and try to revive snapd
            systemctl mask snapd.failure

            # next abort all ongoing changes and wait for them all to be done
            for chg in $(snap changes | tail -n +2 | grep Do | grep -v Done | awk '{print $1}'); do
                snap abort "$chg" || true
                snap watch "$chg" || true
            done

            # now remove all snaps that aren't a base, core or snapd
            for sn in $(snap list | tail -n +2 | awk '{print $1,$6}' | grep -Po '(.+)\s+(?!base)' | awk '{print $1}'); do
                if [ "$sn" != snapd ] && [ "$sn" != core ]; then
                    snap remove "$sn" || true
                fi
            done

            # now we can attempt to purge the actual distro package via apt
            distro_purge_package snapd
            # XXX: the original package's purge may have left socket units behind
            find /etc/systemd/system -name "snap.*.socket" | while read -r f; do
                systemctl stop "$(basename "$f")" || true
                rm -f "$f"
            done
            # double check that purge really worked
            if [ -d /var/lib/snapd ]; then
                echo "# /var/lib/snapd"
                ls -lR /var/lib/snapd || true
                journalctl --no-pager || true
                cat /var/lib/snapd/state.json || true
                snap debug state /var/lib/snapd/state.json || true
                (
                    for chg in $(snap debug state /var/lib/snapd/state.json | tail -n +2 | awk '{print $1}'); do
                        snap debug state --abs-time "--change=$chg" /var/lib/snapd/state.json || true
                    done
                ) || true
                exit 1
            fi

            # unmask snapd.failure so that it can run during tests if needed
            systemctl unmask snapd.failure 
            ;;
        *)
            # snapd state directory must not exist when the package is not
            # installed
            if [ -d /var/lib/snapd ]; then
                echo "# /var/lib/snapd"
                ls -lR /var/lib/snapd || true
                exit 1
            fi
            ;;
    esac

    restart_logind=
    if [ "$(systemctl --version | awk '/systemd [0-9]+/ { print $2 }')" -lt 246 ]; then
        restart_logind=maybe
    fi

    install_pkg_dependencies

    if [ "$restart_logind" = maybe ]; then
        if [ "$(systemctl --version | awk '/systemd [0-9]+/ { print $2 }')" -ge 246 ]; then
            restart_logind=yes
        else
            restart_logind=
        fi
    fi

    # Work around systemd / Debian bug interaction. We are installing
    # libsystemd-dev which upgrades systemd to 246-2 (from 245-*) leaving
    # behind systemd-logind.service from the old version. This is tracked as
    # Debian bug https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=919509 and
    # it really affects Desktop systems where Wayland/X don't like logind from
    # ever being restarted.
    #
    # As a workaround we tried to restart logind ourselves but this caused
    # another issue.  Restarted logind, as of systemd v245, forgets about the
    # root session and subsequent loginctl enable-linger root, loginctl
    # disable-linger stops the running systemd --user for the root session,
    # along with other services like session bus.
    #
    # In consequence all the code that restarts logind for one reason or
    # another is coalesced below and ends with REBOOT. This ensures that after
    # rebooting, we have an up-to-date, working logind and that the initial
    # session used by spread is tracked.
    if ! loginctl enable-linger test; then
        if systemctl cat systemd-logind.service | not grep -q StateDirectory; then
            mkdir -p /mnt/system-data/etc/systemd/system/systemd-logind.service.d
            # NOTE: The here-doc below must use tabs for proper operation.
            cat >/mnt/system-data/etc/systemd/system/systemd-logind.service.d/linger.conf <<-CONF
	[Service]
	StateDirectory=systemd/linger
	CONF
            mkdir -p /var/lib/systemd/linger
            test "$(command -v restorecon)" != "" && restorecon /var/lib/systemd/linger
            restart_logind=yes
        fi
    fi
    loginctl disable-linger test || true

    # FIXME: In an ideal world we'd just do this:
    #   systemctl daemon-reload
    #   systemctl restart systemd-logind.service
    # But due to this issue, restarting systemd-logind is unsafe.
    # https://github.com/systemd/systemd/issues/16685#issuecomment-671239737
    if [ "$restart_logind" = yes ]; then
        echo "logind upgraded, reboot required"
        REBOOT
    fi

    # We take a special case for Debian/Ubuntu where we install additional build deps
    # base on the packaging. In Fedora/Suse this is handled via mock/osc
    case "$SPREAD_SYSTEM" in
        debian-*|ubuntu-*)
            best_golang=golang-1.18
            # in 16.04: "apt build-dep -y ./" would also work but not on 14.04
            gdebi --quiet --apt-line ./debian/control >deps.txt
            quiet xargs -r eatmydata apt-get install -y < deps.txt
            # The go 1.18 backport is not using alternatives or anything else so
            # we need to get it on path somehow. This is not perfect but simple.
            if [ -z "$(command -v go)" ]; then
                # the path filesystem path is: /usr/lib/go-1.18/bin
                ln -s "/usr/lib/${best_golang/lang/}/bin/go" /usr/bin/go
            fi
            ;;
    esac

    # Retry go mod vendor to minimize the number of connection errors during the sync
    for _ in $(seq 10); do
        if go mod vendor; then
            break
        fi
        sleep 1
    done
    # Update C dependencies
    for _ in $(seq 10); do
        if (cd c-vendor && ./vendor.sh); then
            break
        fi
        sleep 1
    done

    # go mod runs as root and will leave strange permissions
    chown test.test -R "$SPREAD_PATH"

    if [ "$BUILD_SNAPD_FROM_CURRENT" = true ]; then
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
    elif [ -n "$SNAPD_PUBLISHED_VERSION" ]; then
        download_from_published "$SNAPD_PUBLISHED_VERSION"
        install_dependencies_from_published "$SNAPD_PUBLISHED_VERSION"
    else
        download_from_gce_bucket
        install_dependencies_gce_bucket
    fi

    # Build fakestore.
    fakestore_tags=
    if [ "$REMOTE_STORE" = staging ]; then
        fakestore_tags="-tags withstagingkeys"
    fi

    # eval to prevent expansion errors on opensuse (the variable keeps quotes)
    eval "go install $fakestore_tags ./tests/lib/fakestore/cmd/fakestore"

    # Build additional utilities we need for testing
    go install ./tests/lib/fakedevicesvc
    go install ./tests/lib/systemd-escape

    # Build the tool for signing model assertions
    go install ./tests/lib/gendeveloper1

    # and the U20 create partitions wrapper
    go install ./tests/lib/uc20-create-partitions

    # On core systems, the journal service is configured once the final core system
    # is created and booted what is done during the first test suite preparation
    if os.query is-classic; then
        # shellcheck source=tests/lib/prepare.sh
        . "$TESTSLIB"/prepare.sh
        disable_journald_rate_limiting
        disable_journald_start_limiting
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
    if os.query is-core; then
        prepare_ubuntu_core
    else
        prepare_classic
    fi

    # Make sure the suite starts with a clean environment and with the snapd state restored
    # shellcheck source=tests/lib/reset.sh
    "$TESTSLIB"/reset.sh --reuse-core
}

prepare_suite_each() {
    local variant="$1"

    # Create runtime files in case those don't exist
    # This is for the first test of the suite. We cannot perform these operations in prepare_suite
    # because not all suites are triggering it (for example the tools suite doesn't).
    touch "$RUNTIME_STATE_PATH/runs"
    touch "$RUNTIME_STATE_PATH/journalctl_cursor"

    # Clean the dmesg log
    dmesg --read-clear

    # Start fs monitor
    "$TESTSTOOLS"/fs-state start-monitor

    # Save all the installed packages
    if os.query is-classic; then
        tests.pkgs list-installed > installed-initial.pkgs
    fi

    # back test directory to be restored during the restore
    tests.backup prepare

    # save the job which is going to be executed in the system
    echo -n "$SPREAD_JOB " >> "$RUNTIME_STATE_PATH/runs"

    # Restart journal log and reset systemd journal cursor.
    systemctl reset-failed systemd-journald.service
    if ! systemctl restart systemd-journald.service; then
        systemctl status systemd-journald.service || true
        echo "Failed to restart systemd-journald.service, exiting..."
        exit 1
    fi
    "$TESTSTOOLS"/journal-state start-new-log

    # Check if journalctl is ready to run the test
    "$TESTSTOOLS"/journal-state check-log-started

    # In case of nested tests the next checks and changes are not needed
    if tests.nested is-nested; then
        return 0
    fi

    if [[ "$variant" = full ]]; then
        if os.query is-classic; then
            # shellcheck source=tests/lib/prepare.sh
            . "$TESTSLIB"/prepare.sh
            prepare_each_classic
        fi
    fi

    case "$SPREAD_SYSTEM" in
        fedora-*|centos-*|amazon-*)
            ausearch -i -m AVC --checkpoint "$RUNTIME_STATE_PATH/audit-stamp" || true
            ;;
    esac

    # Check for invariants late, in order to detect any bugs in the code above.
    if [[ "$variant" = full ]]; then
        "$TESTSTOOLS"/cleanup-state pre-invariant
    fi
    tests.invariant check
}

restore_suite_each() {
    local variant="$1"

    rm -f "$RUNTIME_STATE_PATH/audit-stamp"

    # Run the cleanup restore in case the commands have not been restored
    tests.cleanup restore

    # restore test directory saved during prepare
    tests.backup restore

    # Save all the installed packages and remove the new packages installed 
    if os.query is-classic; then
        tests.pkgs list-installed > installed-final.pkgs
        diff -u installed-initial.pkgs installed-final.pkgs | grep -E "^\+" | tail -n+2 | cut -c 2- > installed-in-test.pkgs
        diff -u installed-initial.pkgs installed-final.pkgs | grep -E "^\-" | tail -n+2 | cut -c 2- > removed-in-test.pkgs

        # shellcheck disable=SC2002
        packages="$(cat installed-in-test.pkgs | tr "\n" " ")"
        if [ -n "$packages" ]; then
            # shellcheck disable=SC2086
            tests.pkgs remove $packages
        fi
        # XXX since the package managers rarely have an option to properly
        # restore kernel or firmware related to their previous versions, use a
        # simple heuristic to skip them during restore
        # shellcheck disable=SC2002
        packages="$(cat removed-in-test.pkgs | grep -v -e kernel -e '-firmware' | tr "\n" " ")"
        if [ -n "$packages" ]; then
            # shellcheck disable=SC2086
            tests.pkgs install $packages
        fi
    fi

    # In case of nested tests the next checks and changes are not needed
    # Just is needed to cleanup the snaps installed
    if tests.nested is-nested; then
        "$TESTSTOOLS"/snaps.cleanup
        return 0
    fi

    # On Arch it seems that using sudo / su for working with the test user
    # spawns the /run/user/12345 tmpfs for XDG_RUNTIME_DIR which asynchronously
    # cleans up itself sometime after the test but not instantly, leading to
    # random failures in the mount leak detector. Give it a moment but don't
    # clean it up ourselves, this should report actual test errors, if any.
    for i in $(seq 10); do
        if not mountinfo.query /run/user/12345 .fs_type=tmpfs; then
            break
        fi
        sleep 1
    done

    if [[ "$variant" = full ]]; then
        # reset the failed status of snapd, snapd.socket, and snapd.failure.socket
        # to prevent hitting the system restart rate-limit for these services
        systemctl reset-failed snapd.service snapd.socket snapd.failure.service
    fi

    if [[ "$variant" = full ]]; then
        # shellcheck source=tests/lib/reset.sh
        "$TESTSLIB"/reset.sh --reuse-core
    fi

    # Check for invariants late, in order to detect any bugs in the code above.
    if [[ "$variant" = full ]]; then
        "$TESTSTOOLS"/cleanup-state pre-invariant
    fi
    tests.invariant check

    "$TESTSTOOLS"/fs-state check-monitor
}

restore_suite() {
    # shellcheck source=tests/lib/reset.sh
    if [ "$REMOTE_STORE" = staging ]; then
        "$TESTSTOOLS"/store-state teardown-staging-store
    fi

    if os.query is-classic; then
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
    "$TESTSTOOLS"/cleanup-state pre-invariant
    # Check for invariants early, in order not to mask bugs in tests.
    tests.invariant check
    "$TESTSTOOLS"/cleanup-state post-invariant

    # TODO: move this to tests.cleanup.
    restore_dev_random

    # TODO: move this to tests.invariant.
    # Udev rules are notoriously hard to write and seemingly correct but subtly
    # wrong rules can pass review. Whenever that happens udev logs an error
    # message. As a last resort from lack of a better mechanism we can try to
    # pick up such errors.
    if grep "invalid .*snap.*.rules" /var/log/syslog; then
        echo "Invalid udev file detected, test most likely broke it"
        exit 1
    fi

    # TODO: move this to tests.invariant.
    # Check if the OOM killer got invoked - if that is the case our tests
    # will most likely not function correctly anymore. It looks like this
    # happens with: https://forum.snapcraft.io/t/4101 and is a source of
    # failure in the autopkgtest environment.
    # Also catch a scenario when snapd service hits the MemoryMax limit set while
    # preparing the tests.
    if dmesg|grep "oom-killer"; then
        echo "oom-killer got invoked during the tests, this should not happen."
        echo "Dmesg debug output:"
        dmesg
        echo "Meminfo debug output:"
        cat /proc/meminfo
        exit 1
    fi

    # TODO: move this to tests.invariant.
    # check if there is a shutdown pending, no test should trigger this
    # and it leads to very confusing test failures
    if [ -e /run/systemd/shutdown/scheduled ]; then
        echo "Test triggered a shutdown, this should not happen"
        snap changes
        exit 1
    fi

    # TODO: move this to tests.invariant.
    # Check for kernel oops during the tests
    if dmesg|grep "Oops: "; then
        echo "A kernel oops happened during the tests, test results will be unreliable"
        echo "Dmesg debug output:"
        dmesg
        exit 1
    fi

    # TODO: move this to tests.invariant.
    if getent passwd snap_daemon; then
        echo "Test left the snap_daemon user behind, this should not happen"
        exit 1
    fi
    if getent group snap_daemon; then
        echo "Test left the snap_daemon group behind, this should not happen"
        exit 1
    fi

    # TODO: move this to tests.invariant.
    # Something is hosing the filesystem so look for signs of that
    not grep -F "//deleted /etc" /proc/self/mountinfo

    # TODO: move this to tests.invariant.
    if journalctl -u snapd.service | grep -F "signal: terminated"; then
        exit 1;
    fi

    # TODO: move this to tests.invariant.
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
        prepare_suite_each full
        ;;
    --prepare-suite-each-minimal-no-snaps)
        prepare_suite_each minimal-no-snaps
        ;;
    --restore-suite-each)
        restore_suite_each full
        ;;
    --restore-suite-each-minimal-no-snaps)
        restore_suite_each minimal-no-snaps
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
        echo "try one of --{prepare,restore}-{project,suite}{,-each} or --{prepare,restore}-suite-each-minimal-no-snaps"
        exit 1
        ;;
esac
