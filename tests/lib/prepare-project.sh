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

    quiet su -l -c "cd $PWD && DEB_BUILD_OPTIONS='nocheck testkeys' dpkg-buildpackage -tc -b -Zgzip" test
    # put our debs to a safe place
    cp ../*.deb $GOPATH
}

download_from_ppa(){
    local ppa_version="$1"

    for pkg in snapd; do
        file="${pkg}_${ppa_version}_$(dpkg --print-architecture).deb"
        curl -L -o "$GOPATH/$file" "https://launchpad.net/~snappy-dev/+archive/ubuntu/snapd-${ppa_version}/+files/$file"
    done
}

# Set REUSE_PROJECT to reuse the previous prepare when also reusing the server.
[ "$REUSE_PROJECT" != 1 ] || exit 0
echo "Running with SNAP_REEXEC: $SNAP_REEXEC"

# check that we are not updating
. "$TESTSLIB/boot.sh"
if [ "$(bootenv snap_mode)" = "try" ]; then
   echo "Ongoing reboot upgrade process, please try again when finished"
   exit 1
fi

# declare the "quiet" wrapper
. "$TESTSLIB/quiet.sh"

if [ "$SPREAD_BACKEND" = external ]; then
   # build test binaries
   if [ ! -f $GOPATH/bin/snapbuild ]; then
       mkdir -p $GOPATH/bin
       snap install --edge test-snapd-snapbuild
       cp /snap/test-snapd-snapbuild/current/bin/snapbuild $GOPATH/bin/snapbuild
       snap remove test-snapd-snapbuild
   fi
   # stop and disable autorefresh
   if [ -e /snap/core/current/meta/hooks/configure ]; then
       systemctl disable --now snapd.refresh.timer
       snap set core refresh.disabled=true
   fi
   exit 0
fi

if [ "$SPREAD_BACKEND" = qemu ]; then
   # treat APT_PROXY as a location of apt-cacher-ng to use
   if [ -d /etc/apt/apt.conf.d ] && [ -n "${APT_PROXY:-}" ]; then
       printf 'Acquire::http::Proxy "%s";\n' "$APT_PROXY" > /etc/apt/apt.conf.d/99proxy
   fi
fi

create_test_user

quiet apt-get update

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

quiet apt-get purge -y snapd
# utilities
# XXX: build-essential seems to be required. Otherwise package build
# fails with unmet dependency on "build-essential:native"
quiet apt-get install -y build-essential curl devscripts expect gdebi-core jq rng-tools git

# in 16.04: apt build-dep -y ./
quiet apt-get install -y $(gdebi --quiet --apt-line ./debian/control)

# update vendoring
if [ "$(which govendor)" = "" ]; then
    rm -rf $GOPATH/src/github.com/kardianos/govendor
    go get -u github.com/kardianos/govendor
fi
quiet govendor sync

if [ -z "$SNAPD_PPA_VERSION" ]; then
    build_deb
else
    download_from_ppa "$SNAPD_PPA_VERSION"
fi

# Build snapbuild.
go get ./tests/lib/snapbuild
# Build fakestore.

fakestore_tags=
if [ "$REMOTE_STORE" = staging ]; then
    fakestore_tags="-tags withstagingkeys"
fi
go get $fakestore_tags ./tests/lib/fakestore/cmd/fakestore
# Build fakedevicesvc.
go get ./tests/lib/fakedevicesvc
