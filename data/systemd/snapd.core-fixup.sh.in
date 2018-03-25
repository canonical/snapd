#!/bin/sh

set -e

if ! grep -q "ID=ubuntu-core" /etc/os-release; then
    # this code is only relevant on ubuntu-core devices
    #
    # this script will only run via systemd if /writable/system-data
    # exists however we still add this check here in case people run
    # it manually
    exit 0
fi

# store important data in case we need it later
if [ ! -f /var/lib/snapd/device/ownership-change.before ]; then
    mkdir -p /var/lib/snapd/device
    find /etc/cloud /var/lib/cloud /var/lib/snapd -printf '%M %U %G %p\n' > /var/lib/snapd/device/ownership-change.before.tmp || true
    find  /writable/system-data /writable/system-data/var /writable/system-data/var/lib /writable/system-data/boot /writable/system-data/etc -maxdepth 0 -printf '%M %U %G %p\n' >> /var/lib/snapd/device/ownership-change.before.tmp || true
    mv /var/lib/snapd/device/ownership-change.before.tmp /var/lib/snapd/device/ownership-change.before
fi
    
# cleanup read/write files and directories (CVE-2017-10600)
for i in /etc/cloud /var/lib/cloud /var/lib/snapd ; do
  # restore ownership to root:root
  find "$i" \( -type f -o -type d -o -type l \) -a \( \! -uid 0 -o \! -gid 0 \) -print0 | \
    xargs -0 --no-run-if-empty chown -c --no-dereference root:root -- || true
done

# cleanup a few /writable directories without descending
for i in /writable/system-data /writable/system-data/var /writable/system-data/var/lib /writable/system-data/boot /writable/system-data/etc ; do
  # restore ownership to root:root
  find "$i" -maxdepth 0 \( \! -uid 0 -o \! -gid 0 -o -type l \) -print0 | \
    xargs -0 --no-run-if-empty chown -c --no-dereference root:root -- || true
done

# store permissions after manipulation, this is also used as the stamp file
# for the systemd service to ensure it is only run once
find /etc/cloud /var/lib/cloud /var/lib/snapd -printf '%M %U %G %p\n' > /var/lib/snapd/device/ownership-change.after.tmp
find  /writable/system-data /writable/system-data/var /writable/system-data/var/lib /writable/system-data/boot /writable/system-data/etc -maxdepth 0 -printf '%M %U %G %p\n' >> /var/lib/snapd/device/ownership-change.after.tmp
mv /var/lib/snapd/device/ownership-change.after.tmp /var/lib/snapd/device/ownership-change.after

# ensure things are really on disk
sync
