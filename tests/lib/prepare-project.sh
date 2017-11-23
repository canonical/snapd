#!/bin/bash

set -e

# shellcheck source=tests/lib/quiet.sh
. "$TESTSLIB/quiet.sh"

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
            debian-*|fedora-*|opensuse-*)
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

    chown test.test -R ..
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
    arch=x86_64
    base_version="$(head -1 debian/changelog | awk -F '[()]' '{print $2}')"
    version="1337.$base_version"
    packaging_path=packaging/$distro-$release
    archive_name=snapd-$version.tar.gz
    archive_compression=z
    extra_tar_args=
    rpm_dir=$(rpm --eval "%_topdir")

    case "$SPREAD_SYSTEM" in
        fedora-*)
            extra_tar_args="$extra_tar_args --exclude=vendor/"
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
    (cd /tmp/pkg && tar c${archive_compression}f "$rpm_dir/SOURCES/$archive_name" "snapd-$version" $extra_tar_args)
    cp "$packaging_path"/* "$rpm_dir/SOURCES/"

    # Cleanup all artifacts from previous builds
    rm -rf "$rpm_dir"/BUILD/*

    # Build our source package
    rpmbuild --with testkeys -bs "$packaging_path/snapd.spec"

    # .. and we need all necessary build dependencies available
    deps=()
    n=0
    IFS=$'\n'
    for dep in $(rpm -qpR "$rpm_dir"/SRPMS/snapd-1337.*.src.rpm); do
      if [[ "$dep" = rpmlib* ]]; then
         continue
      fi
      deps[$n]=$dep
      n=$((n+1))
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

# Set REUSE_PROJECT to reuse the previous prepare when also reusing the server.
[ "$REUSE_PROJECT" != 1 ] || exit 0
echo "Running with SNAP_REEXEC: $SNAP_REEXEC"

# check that we are not updating
# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"
if [ "$(bootenv snap_mode)" = "try" ]; then
   echo "Ongoing reboot upgrade process, please try again when finished"
   exit 1
fi

# declare the "quiet" wrapper
# shellcheck source=tests/lib/quiet.sh
. "$TESTSLIB/quiet.sh"
# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"

if [ "$SPREAD_BACKEND" = external ]; then
   # stop and disable autorefresh
   if [ -e "$SNAP_MOUNT_DIR/core/current/meta/hooks/configure" ]; then
       systemctl disable --now snapd.refresh.timer
   fi
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

# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"

distro_update_package_db

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
if [ -z "$(which govendor)" ]; then
    rm -rf "$GOPATH/src/github.com/kardianos/govendor"
    go get -u github.com/kardianos/govendor
fi
quiet govendor sync

if [ -z "$SNAPD_PUBLISHED_VERSION" ]; then
    case "$SPREAD_SYSTEM" in
      ubuntu-*|debian-*)
         build_deb
         ;;
      fedora-*|opensuse-*)
         build_rpm
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
