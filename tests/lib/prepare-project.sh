#!/bin/bash

. "$TESTSLIB/quiet.sh"

create_test_user(){
   if ! id test >& /dev/null; then
        # manually setting the UID and GID to 12345 because we need to
        # know the numbers match for when we set up the user inside
        # the all-snap, which has its own user & group database.
        # Nothing special about 12345 beyond it being high enough it's
        # unlikely to ever clash with anything, and easy to remember.
        EXTRA_ADDUSER_ARGS=""
        if [[ "$SPREAD_SYSTEM" = ubuntu-* ]]; then
            EXTRA_ADDUSER_ARGS="--disable-password --gecos=''"
        fi
        quiet groupadd --gid 12345 test
        quiet adduser --uid 12345 --gid 12345 $EXTRA_ADDUSER_ARGS test
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
    cp ../*.deb $GOPATH
}

fedora_build_rpm() {
    base_version="$(head -1 debian/changelog | awk -F'[()]' '{print $2}')"
    version="1337.$base_version"
    sed -i -e "s/^Version:.*$/Version: $version/g" packaging/fedora-25/snapd.spec

    mkdir -p /tmp/pkg/snapd-$version
    cp -rav * /tmp/pkg/snapd-$version/

    mkdir -p $HOME/rpmbuild/SOURCES
    (cd /tmp/pkg; tar czf $HOME/rpmbuild/SOURCES/snapd-$version.tar.gz snapd-$version --exclude=vendor/)

    cp packaging/fedora-25/* $HOME/rpmbuild/SOURCES/

    rpmbuild -bs packaging/fedora-25/snapd.spec
    # FIXME 1.fc25 + arch needs to be dynamic as well
    mock /root/rpmbuild/SRPMS/snapd-$version-1.fc25.src.rpm
    cp /var/lib/mock/fedora-25-x86_64/result/snapd-$version-1.fc25.x86_64.rpm $GOPATH
    cp /var/lib/mock/fedora-25-x86_64/result/snapd-selinux-$version-1.fc25.noarch.rpm $GOPATH
    cp /var/lib/mock/fedora-25-x86_64/result/snap-confine-$version-1.fc25.x86_64.rpm $GOPATH
}

download_from_published(){
    local published_version="$1"

    curl -s -o pkg_page "https://launchpad.net/ubuntu/+source/snapd/$published_version"

    arch=$(dpkg --print-architecture)
    build_id=$(sed -n 's|<a href="/ubuntu/+source/snapd/'"$published_version"'/+build/\(.*\)">'"$arch"'</a>|\1|p' pkg_page | sed -e 's/^[[:space:]]*//')

    # we need to download snap-confine and ubuntu-core-launcher for versions < 2.23
    for pkg in snapd snap-confine ubuntu-core-launcher; do
        file="${pkg}_${published_version}_${arch}.deb"
        curl -L -o "$GOPATH/$file" "https://launchpad.net/ubuntu/+source/snapd/${published_version}/+build/${build_id}/+files/${file}"
    done
}

install_dependencies_from_published(){
    local published_version="$1"

    for dep in snap-confine ubuntu-core-launcher; do
        dpkg -i "${GOPATH}/${dep}_${published_version}_$(dpkg --print-architecture).deb"
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

. "$TESTSLIB/dirs.sh"

if [ "$SPREAD_BACKEND" = external ]; then
   # build test binaries
   if [ ! -f $GOPATH/bin/snapbuild ]; then
       mkdir -p $GOPATH/bin
       snap install --edge test-snapd-snapbuild
       cp $SNAPMOUNTDIR/test-snapd-snapbuild/current/bin/snapbuild $GOPATH/bin/snapbuild
       snap remove test-snapd-snapbuild
   fi
   # stop and disable autorefresh
   if [ -e $SNAPMOUNTDIR/core/current/meta/hooks/configure ]; then
       systemctl disable --now snapd.refresh.timer
       snap set core refresh.disabled=true
   fi
   chown test.test -R $PROJECT_PATH
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
distro_install_package $DISTRO_BUILD_DEPS

# We take a special case for Debian/Ubuntu where we install additional build deps
# base on the packaging. In Fedora/Suse this is handled via mock/osc
case "$SPREAD_SYSTEM" in
    debian-*|ubuntu-*)
        # in 16.04: apt build-dep -y ./
        quiet apt-get install -y $(gdebi --quiet --apt-line ./debian/control)
        ;;
    *)
        ;;
esac

# update vendoring
if [ -z "$(which govendor)" ]; then
    rm -rf $GOPATH/src/github.com/kardianos/govendor
    go get -u github.com/kardianos/govendor
fi
quiet govendor sync

if [ -z "$SNAPD_PUBLISHED_VERSION" ]; then
    case "$SPREAD_SYSTEM" in
      ubuntu-*|debian-*)
         build_deb
         ;;
      fedora-*)
         fedora_build_rpm
         ;;
      *)
         ;;
   esac
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
go get $fakestore_tags ./tests/lib/fakestore/cmd/fakestore

# Build additional utilities we need for testing
go get ./tests/lib/fakedevicesvc
go get ./tests/lib/systemd-escape
