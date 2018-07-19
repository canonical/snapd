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

# Workaround https://forum.snapcraft.io/t/5253
#
# We see sometimes corrupted uboot.env files created by fsck.vfat.
# On the fat filesystem they are indistinguishable because one
# has a fat16 name UBOOT.ENV (and not lfn (long-file-name)) but
# the other has a "uboot.env" lfn name and a FSCK0000.000 FAT16
# name. The only known workaround is to remove all dupes and put
# one file back in place.
if [ "$(find /boot/uboot -name uboot.env | wc -l)" -gt 1 ]; then
    echo "Corrupted uboot.env file detected"
    # Ensure we have one uboot.env to go back to. Note that it does
    # not matter which one we pick (we can't choose anyway, we get
    # whatever the kernel gives us). The key part is that there is
    # only a single one after this script finishes. The bootloader
    # logic will recover in any case.
    cp -a /boot/uboot/uboot.env /boot/uboot/uboot.env.save
    # now delete all dupes
    while ls /boot/uboot/uboot.env 2>/dev/null; do
        rm -f /boot/uboot/uboot.env
    done
    # and move the saved one into place
    mv /boot/uboot/uboot.env.save /boot/uboot/uboot.env

    # ensure we sync the fs
    sync
fi


# The code below deals with incorrect permissions that happened on
# some buggy ubuntu-image versions.
#
# This needs to run only once so we can exit here.
if [ -f /var/lib/snapd/device/ownership-change.after ]; then
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
