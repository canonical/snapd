#!/bin/sh
#
# (not really executable, just to make editors happy)

version_info() {
    versions=$(snappy list)
    current=$(echo "$versions" | awk '/^ubuntu-core/ {print $3}')
    echo "current version: $current"
    all_version="$(snappy list -v)"

    # something went wrong here with the extraction
    [ "$current" != "${current#[0-9]}" ] || fail "\"$current\" is not numeric"

    versions=$(snappy list -u)
    avail=$(echo "$versions" | awk '/^ubuntu-core/ {print $3}')
    echo "available version: $avail"
    [ "$avail" != "${avail#[0-9]}" ] || fail  "\"$avail\" is not numeric"
}

# debug info really
boot_info() {
    echo "Boot order"

    if [ -e /boot/grub/grub.cfg ]; then
        # FIXME: why do we have two places that define what root to boot?
        
        # check grub.cfg config
        grep root=LABEL /boot/grub/grub.cfg|head -n1
        # show /boot/grub/grubenv
        grub-editenv - list
    elif [ -e /boot/uboot/snappy-system.txt ]; then
        # show uboot config
        egrep '(snappy_mode|snappy_ab)' /boot/uboot/snappy-system.txt
    else
        fail "unknown bootloader"
    fi
    echo "---"
}

switch_channel() {
    SED_EXPR="$1"
    sudo mount -o remount,rw /
    sudo sed -i "${SED_EXPR}" /etc/system-image/channel.ini
    sudo mount -o remount,ro /

    if [ -e /writable/cache/system/etc/system-image/channel.ini ]; then
        sudo mount -o remount,rw /writable/cache/system/
        sudo sed -i "${SED_EXPR}" /writable/cache/system/etc/system-image/channel.ini
        sudo mount -o remount,ro /writable/cache/system/
    fi
}

save_version_info() {
    avail=$1
    current=$2
    
    echo "$avail" > "${ADT_ARTIFACTS}/avail"
    echo "$current" > "${ADT_ARTIFACTS}/current"
}
