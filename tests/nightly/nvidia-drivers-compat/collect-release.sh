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
"$TESTSTOOLS"/lxd-state launch --remote ubuntu --image "$release" --name "$cname" --params "--ephemeral -c limits.cpu=8 -c limits.memory=8GiB ${EXTRA_LXC_ARGS-}"
# Search for nvidia-drivers in apt-cache and remove the ones which have
#   - "transition" in their first line of Description, or,
#   - a single dependency to other packages which is typical for transitional packages
# The second filter is needed as for many packages "transition" is not mentioned in the
# first line of Description but rather in the detailed Description field which is not
# available in `apt-cache search`.
# The first filter is still kept as the second filter is slower
driver_versions=$(lxc exec "$cname" -- sh -c "apt-cache search nvidia-driver | grep nvidia-driver | grep -v -i transition | cut -f1 -d' ' | \
                                              while read -r v; do \
                                                if [ \$(apt-cache depends \$v | grep -w Depends | wc -l) -gt 1 ]; then \
                                                  echo \$v; \
                                                fi; \
                                              done")

echo "-- release $release"
echo "-- driver versions:"
echo "$driver_versions" > "$release-drivers"
echo "$driver_versions"

lxc file push collect-driver.sh "$cname"/tmp/collect-driver.sh

for v in $driver_versions; do
    if [ -e "$release-$v-done" ]; then
        echo "--- skipping driver $v, already done"
        continue
    fi

    lxc exec "$cname" -- /bin/sh -c "cd /tmp; ./collect-driver.sh $v" > "$release-$v.libs"
    test -s "$release-$v.libs"
    touch "$release-$v-done"
done

lxc delete --force "$cname"

touch "$release-done"
