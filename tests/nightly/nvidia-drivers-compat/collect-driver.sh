#!/bin/bash

set -eux

if [ ! -e dpkg-src-done ]; then
    if [ -e /etc/apt/sources.list.d/ubuntu.sources ]; then
        # new format fro 24.04+
        sed -i -e 's/Types: deb$/Types: deb deb-src/' /etc/apt/sources.list.d/ubuntu.sources >&2
        grep 'deb-src' /etc/apt/sources.list.d/ubuntu.sources >&2
    else
        sed -i -e 's/^# deb-src /deb-src /' /etc/apt/sources.list >&2
        grep 'deb-src' /etc/apt/sources.list >&2
    fi
    touch dpkg-src-done >&2
fi

if [ ! -e apt-file-done ]; then
    eatmydata -- apt update >&2
    DEBIAN_FRONTEND=noninteractive eatmydata -- apt install dpkg-dev apt-file -y >&2
    eatmydata -- apt-file update >&2
    touch apt-file-done >&2
fi

driver="${1-}"

if [ -z "$driver" ]; then
    echo "driver not provided" >&2
    exit 1
fi

eatmydata -- apt source "$driver" >&2
driver_version=${DRIVER_VERSION-$(echo "$driver" | sed -e 's/-open//' -e 's/-server//' | rev | cut  -d- -f1 | rev)}
# shellcheck disable=SC2086
binpkglist=$(
    cat nvidia-graphics-drivers-${driver_version}*.dsc | \
        awk '/Package-List:/ { dump=1; next } /^[a-zA-Z].*:/ { if (dump==1) { dump=0; next} } // { if (dump==1) { print $1 } }' | \
        grep -v -- -open\
          )

echo "-- binary package list: $binpkglist" >&2

if [ "${USE_INSTALL-}" = "y" ]; then
    # shellcheck disable=SC2086
    DEBIAN_FRONTEND=noninteractive eatmydata -- apt install $binpkglist -y >&2
    for p in $binpkglist; do
        dpkg -L "$p" |grep -E '\.so' | sort -u || true
    done
else
    (
        for p in $binpkglist; do
        # generate pattern for grep
        echo " .*/$p\(,.*\)\?\$"
        done
    ) > "patterns.$driver"
    /usr/lib/apt/apt-helper cat-file /var/lib/apt/lists/*Contents-* | \
        grep -f "patterns.$driver" | \
        awk '/.*\.so(\..*)?/ { print  "/" $1 }' | \
        LC_ALL=C sort -ud --stable || true
    rm -f "patterns.$driver" >&2
fi

# shellcheck disable=SC2086
rm -rfv nvidia-graphics-drivers-${driver_version}* >&2
