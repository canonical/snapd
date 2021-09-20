#! /bin/sh

set -e

OUT_DIR="$1"

PACKAGES="cmd/snap"

if [ ! -x "tests/component/build.sh" ]; then
    echo "Error: the build script must be run from the repository's root directory."
    exit 1
fi

mkdir -p "$OUT_DIR"

for PACKAGE in $PACKAGES
do
    go test -o "${OUT_DIR}/$(basename $PACKAGE)" \
        -covermode atomic -cover \
        -coverpkg ./... \
        "./$PACKAGE"
done
