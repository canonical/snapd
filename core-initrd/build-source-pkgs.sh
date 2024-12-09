#!/bin/bash -exu

# This scripts cleans-up the core-initrd subfolder and pulls all necessary bits
# from snapd to create the ubuntu-core-initramfs source package for each
# supported Ubuntu release. It is meant to be called inside the core-initrd
# folder.

git clean -ffdx

# The current commit must be in the repo to be able to get the dependencies
# of snap-bootstrap.
commit=$(git rev-parse HEAD)

# build info file
pushd ..
./mkversion.sh
popd

contains_element() {
    local e match="$1"
    shift
    for e; do [[ "$e" == "$match" ]] && return 0; done
    return 1
}

# Folder for snapd bits, that will be copied to all releases
mkdir snapd-initramfs
pushd snapd-initramfs
## snap-bootstrap
mkdir cmd
# go commands do not follow symlinks, copy instead
cp -a ../../cmd/snap-bootstrap/ cmd/
cat << EOF > go.mod
module github.com/snapcore/snap-bootstrap

go 1.18

require	github.com/snapcore/snapd $commit
EOF
# solve dependencies
go mod tidy
# build vendor folder
go mod vendor

## info and recovery trigger service
mkdir snapd
cp ../../data/info snapd/
sed 's#@libexecdir@#/usr/lib#' ../../data/systemd/snapd.recovery-chooser-trigger.service.in > \
    snapd/snapd.recovery-chooser-trigger.service
popd

# Go through the different supported Ubuntu releases, creating source
# packages for them.
no_link=(debian go.mod go.sum cmd snapd vendor)
for dir in */debian; do
    rel=${dir%/debian}

    if [ "$rel" != latest ]; then
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
    dpkg-buildpackage -S -sa -d
    popd
done
