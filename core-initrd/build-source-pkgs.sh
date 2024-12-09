#!/bin/bash -exu

git clean -ffdx

# The current commit must be in the repo to be able to get the dependencies
# of snap-bootstrap.
commit=$(git rev-parse HEAD)

for dir in */debian; do
    rel=${dir%/debian}

    if [ "$rel" != latest ]; then
        for p in latest/*; do
            file=${p#latest/}
            if [ "$file" = debian ] || [ "$file" = go.mod ] ||
                   [ "$file" = go.sum ] || [ "$file" = cmd ]; then
                continue
            fi
            cp -a "$p" "$rel/"
        done
    fi

    pushd "$rel"
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
    dpkg-buildpackage -S -sa -d
    popd

done
