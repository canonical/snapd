#!/bin/bash -exu

git clean -ffdx

for dir in */debian; do
    rel=${dir%/debian}

    if [ "$rel" != latest ]; then
        for f in latest/*; do
            ln -s "$f" "$rel"/"${f#latest/}"
        done
    fi
    pushd "$rel"
    dpkg-buildpackage -S -sa -d
    popd
done
