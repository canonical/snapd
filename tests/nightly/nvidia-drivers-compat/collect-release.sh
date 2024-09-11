#!/bin/bash

set -exu

release="${1-}"

if [ -z "$release" ]; then
    echo "release not provided"
    exit 1
fi

cname="nvidiatest-$(echo "$release" | tr . -)"

if [ -e "$release-done" ]; then
    echo "---- skipping $release, already done"
    exit 0
fi

# shellcheck disable=SC2086
lxc launch "ubuntu:$release" "$cname" --ephemeral -c limits.cpu=8 -c limits.memory=8GiB ${EXTRA_LXC_ARGS-}
lxc exec "$cname" -- cloud-init status --wait
echo "--- container ready"
driver_versions=$(lxc exec "$cname" -- sh -c "apt-cache search nvidia-driver | grep nvidia-driver | grep -v -i transition | cut -f1 -d' '")

echo "-- release $release"
echo "-- driver versions:"
echo "$driver_versions" > "$release-drivers"
echo "$driver_versions"

lxc file push collect-driver.sh "$cname"/root/collect-driver.sh

for v in $driver_versions; do
    if [ -e "$release-$v-done" ]; then
        echo "--- skipping driver $v, already done"
        continue
    fi

    lxc exec "$cname" -- /bin/sh -c "cd /root; ./collect-driver.sh $v" > "$release-$v.libs"
    test -s "$release-$v.libs"
    touch "$release-$v-done"
done

lxc delete --force "$cname"

touch "$release-done"
