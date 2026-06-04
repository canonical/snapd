#!/bin/bash

# remove stripping
sed -i '/minidebuginfo/d' build-aux/snap/snapcraft.yaml

# rather than dropping in the testingcoveragegeneration build tag, change
# them to withtestkeys, which is already including in all testing builds
while IFS= read -r file; do
    sed -i -E '/^[[:space:]]*\/\/go:build[[:space:]]+!?/ s/testingcoveragegeneration/withtestkeys/g' "$file"
done < <(find . -name '*.go' -exec sh -c 'if grep -q testingcoveragegeneration "$1"; then echo "$1"; fi' _ {} \;)

# patch builds to include coverage
sed -i '/go build/ { / -cover/! s/go build/go build -cover/ }' build-aux/snap/snapcraft.yaml
sed -i 's/^EXTRA_GO_BUILD_FLAGS = .*/& -cover/' packaging/arch/PKGBUILD
find packaging -type f -name snapd.spec -exec sed -i 's/^EXTRA_GO_BUILD_FLAGS = .*/& -cover/' {} \;
