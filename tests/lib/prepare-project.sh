#!/bin/bash

create_test_user(){
   if ! id test >& /dev/null; then
        # manually setting the UID and GID to 12345 because we need to
        # know the numbers match for when we set up the user inside
        # the all-snap, which has its own user & group database.
        # Nothing special about 12345 beyond it being high enough it's
        # unlikely to ever clash with anything, and easy to remember.
        addgroup --quiet --gid 12345 test
        adduser --quiet --uid 12345 --gid 12345 --disabled-password --gecos '' test
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
   # build test binaries
   if [ ! -f "$GOHOME/bin/snapbuild" ]; then
       mkdir -p "$GOHOME/bin"
       snap install --edge test-snapd-snapbuild
       cp "$SNAPMOUNTDIR/test-snapd-snapbuild/current/bin/snapbuild" "$GOHOME/bin/snapbuild"
       snap remove test-snapd-snapbuild
   fi
   # stop and disable autorefresh
   if [ -e "$SNAPMOUNTDIR/core/current/meta/hooks/configure" ]; then
       systemctl disable --now snapd.refresh.timer
       snap set core refresh.disabled=true
   fi
   chown test.test -R "$PROJECT_PATH"
   exit 0
fi

if [ "$SPREAD_BACKEND" = qemu ]; then
   # qemu images may be built with pre-baked proxy settings that can be wrong
   rm -f /etc/apt/apt.conf.d/90cloud-init-aptproxy
   # treat APT_PROXY as a location of apt-cacher-ng to use
   if [ -d /etc/apt/apt.conf.d ] && [ -n "${APT_PROXY:-}" ]; then
       printf 'Acquire::http::Proxy "%s";\n' "$APT_PROXY" > /etc/apt/apt.conf.d/99proxy
   fi
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

distro_purge_package snapd
# utilities
# XXX: build-essential seems to be required. Otherwise package build
# fails with unmet dependency on "build-essential:native"
distro_install_package build-essential curl devscripts expect gdebi-core jq rng-tools git netcat-openbsd

# in 16.04: apt build-dep -y ./
gdebi --quiet --apt-line ./debian/control | quiet xargs -r apt-get install -y

# update vendoring
if [ "$(which govendor)" = "" ]; then
    rm -rf "$GOHOME/src/github.com/kardianos/govendor"
    go get -u github.com/kardianos/govendor
fi
quiet govendor sync

if [ -z "$SNAPD_PUBLISHED_VERSION" ]; then
    build_deb
else
    download_from_published "$SNAPD_PUBLISHED_VERSION"
    install_dependencies_from_published "$SNAPD_PUBLISHED_VERSION"
fi

# Build snapbuild.
go get ./tests/lib/snapbuild
# Build fakestore.

fakestore_tags=
if [ "$REMOTE_STORE" = staging ]; then
    fakestore_tags="-tags withstagingkeys"
fi
# shellcheck disable=SC2086
go get $fakestore_tags ./tests/lib/fakestore/cmd/fakestore

# Build additional utilities we need for testing
go get ./tests/lib/fakedevicesvc
go get ./tests/lib/systemd-escape
