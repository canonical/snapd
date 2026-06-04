#!/bin/bash

# remove stripping
sed -i '/minidebuginfo/d' build-aux/snap/snapcraft.yaml

# patch builds to include coverage
sed -i '/go build/ { / -cover/! s/go build/go build -cover/ }' build-aux/snap/snapcraft.yaml
sed -i 's/^EXTRA_GO_BUILD_FLAGS = .*/& -cover/' packaging/arch/PKGBUILD
find packaging -type f -name snapd.spec -exec sed -i 's/^EXTRA_GO_BUILD_FLAGS = .*/& -cover/' {} \;
