#!/bin/bash -exu

# This scripts cleans-up the core-initrd subfolder and pulls all necessary bits
# from snapd to create the ubuntu-core-initramfs source package for supported
# Ubuntu releases. It is meant to be called inside the core-initrd folder.
#
# Usage:
#
# $ ./build-source-pkgs.sh <ubuntu_release1> ... <ubuntu_releaseN>
# or
# $ ./build-source-pkgs.sh
#
# to build all releases in the directory.

# The current commit must be in the repo to be able to get the dependencies
# of snap-bootstrap.
if [ -n "${TEST_BUILD-}" ]; then
    # code at this commit won't be actually used, but we need it to exist so go
    # mod tidy runs properly
    commit=master
else
    git clean -ffdx
    commit=$(git rev-parse HEAD)
fi

# build info file, source the created file to get the variables
pushd ..
./mkversion.sh
# shellcheck disable=SC1091
. data/info
SNAPD_VERSION=$VERSION
popd

if [[ $SNAPD_VERSION == *"-dirty"* ]]; then
    printf "repo is dirty, please clean-up before building the initramfs source packages\n"
    exit 1
fi

contains_element() {
    local e match="$1"
    shift
    for e; do [[ "$e" == "$match" ]] && return 0; done
    return 1
}

# Folder for snapd bits, that will be copied to all releases
mkdir -p snapd-initramfs
pushd snapd-initramfs
## snap-bootstrap
mkdir -p cmd
# go commands do not follow symlinks, copy instead
cp -a ../../cmd/snap-bootstrap/ cmd/
cat << EOF > go.mod
module github.com/snapcore/snap-bootstrap

go 1.18

require	github.com/snapcore/snapd $commit
EOF
if [ -n "${TEST_BUILD-}" ]; then
    # Use local code for test builds
    printf "\nreplace github.com/snapcore/snapd => ../../\n" >> go.mod
fi
# solve dependencies
go mod tidy
# build vendor folder
go mod vendor

## info and recovery trigger service
mkdir -p snapd
cp ../../data/info snapd/
sed 's#@libexecdir@#/usr/lib#' ../../data/systemd/snapd.recovery-chooser-trigger.service.in > \
    snapd/snapd.recovery-chooser-trigger.service
popd

# Go through the different supported Ubuntu releases, creating source
# packages for them.
no_link=(debian go.mod go.sum cmd snapd vendor)
if [ "$#" -eq 0 ]; then
    # If no explicit releases are given, build all releases in the directory
    deb_dir=(*/debian)
    set -- "${deb_dir[@]%/debian}"
fi
for rel; do
    series=$(dpkg-parsechangelog --file "$rel"/debian/changelog --show-field Distribution)
    if [ "$rel" = latest ]; then
        ubuntu_ver=$(ubuntu-distro-info --series="$series" -r)
        # We might have "xx.xx LTS"
        ubuntu_ver=${ubuntu_ver%% *}
    else
        ubuntu_ver=$rel
        for p in latest/*; do
            file=${p#latest/}
            if contains_element "$file" "${no_link[@]}"; then
                continue
            fi
            cp -a "$p" "$rel/"
        done
    fi

    pushd "$rel"
    cp -a ../snapd-initramfs/* .

    curr_ver=$(dpkg-parsechangelog --show-field Version)
    initrd_ver=${curr_ver%%+*}
    next_ver="$initrd_ver"+"$SNAPD_VERSION"+"$ubuntu_ver"
    dch -v "$next_ver" "Update to snapd version $SNAPD_VERSION"
    dch --distribution "$series" -r ""
    dpkg-buildpackage -S -sa -d
    popd
done
