#!/bin/bash

# mount ubuntu cloud image through qemu-nbd and mount
# critical virtual filesystems (such as proc) under
# the root of mounted image.
# The path of the image needs to be absolute as a systemd service
# gets created for qemu-nbd.
mount_ubuntu_image() {
    local CLOUD_IMAGE=$1
    local IMAGE_MOUNTPOINT=$2

    if ! lsmod | grep nbd; then
        modprobe nbd
    fi

    # Run qemu-nbd as a service, so that it does not interact with ssh
    # stdin/stdout it would otherwise inherit from the spread session.
    systemd-run --system --service-type=forking --unit=qemu-nbd-preseed.service "$(command -v qemu-nbd)" -v --fork -c /dev/nbd0 "$CLOUD_IMAGE"
    # nbd0p1 may take a short while to become available
    if ! retry -n 30 --wait 1 test -e /dev/nbd0p1; then
        echo "ERROR: /dev/nbd0p1 did not show up"
        journalctl -u qemu-nbd-preseed.service
        find /dev/ -name "nbd0*" -ls        
        exit 1
    fi
    mount /dev/nbd0p1 "$IMAGE_MOUNTPOINT"
    mount -t proc /proc "$IMAGE_MOUNTPOINT/proc"
    mount -t sysfs sysfs "$IMAGE_MOUNTPOINT/sys"
    mount -t devtmpfs udev "$IMAGE_MOUNTPOINT/dev"
    mount -t securityfs securityfs "$IMAGE_MOUNTPOINT/sys/kernel/security"
}

umount_ubuntu_image() {
    local IMAGE_MOUNTPOINT=$1

    for fs in proc dev sys/kernel/security sys; do
        umount "$IMAGE_MOUNTPOINT/$fs"
    done
    umount "$IMAGE_MOUNTPOINT"
    rmdir "$IMAGE_MOUNTPOINT"

    # qemu-nbd -d may sporadically fail when removing the device,
    # reporting it's still in use.
    retry -n 5 --wait 1 qemu-nbd -d /dev/nbd0
}

# inject_snap_info_seed adds a snap to the seed.yaml, and works for snaps not
# already in the seed. It requires base snaps, default-providers, etc. to all be
# worked out and manually added with additional invocations
# the first argument is the mountpoint of the image, the second argument is the 
# name of the snap, the snap file must be the same as the name with .snap as the
# file extension in the current working directory
# example:
#   $ snap download --edge --basename=test-snapd-sh test-snapd-sh
#   $ inject_snap_into_seeds "$IMAGE_MOUNTPOINT" test-snapd-sh
inject_snap_into_seed() {
    local IMAGE_MOUNTPOINT=$1
    local SNAP_NAME=$2
    local SNAP_FILE="$SNAP_NAME.snap"
    local SEED_DIR="$IMAGE_MOUNTPOINT/var/lib/snapd/seed"
    local SEED_YAML="$SEED_DIR/seed.yaml"
    local SEED_SNAPS_DIR="$SEED_DIR/snaps"

    # Ubuntu 24.04: there is no longer any seeded snaps in base or minimal cloud images
    # https://bugs.launchpad.net/ubuntu/+source/ubuntu-meta/+bug/2051346
    # https://bugs.launchpad.net/ubuntu/+source/ubuntu-meta/+bug/2051572
    if [ ! -d "$SEED_DIR" ]; then
        snap known model > /tmp/generic.model
        snap prepare-image --classic /tmp/generic.model $IMAGE_MOUNTPOINT
    fi

    # need remarshal for going from json to yaml and back for seed manipulation
    if ! command -v json2yaml || ! command -v yaml2json; then
        snap install remarshal
    fi

    # XXX: this is very simplistic and will break easily, refactor to use the 
    #      iterative seed modification prepare-image args when those exist

    snapsWithName=$(yaml2json < "$SEED_YAML" | jq -r --arg NAME "$SNAP_NAME" '[.snaps[] | select(.name == $NAME)] | length')
    if [ "$snapsWithName" != "0" ]; then
        # get the snap file name so we can delete it from the seed
        old_name=$(yaml2json < "$SEED_YAML" | \
            jq -r --arg NAME "$SNAP_NAME" '.snaps[] | select(.name == $NAME) | .file')
        rm "$SEED_SNAPS_DIR/$old_name"

        # now drop the entry from the seed.yaml so we can add the new one easily
        yaml2json < "$SEED_YAML" | \
            jq --arg NAME "$SNAP_NAME" 'del(.snaps[] | select(.name == $NAME))' | \
                json2yaml > "$SEED_YAML.tmp"
        mv "$SEED_YAML.tmp" "$SEED_YAML"
    fi

    # now add the desired snap as an unasserted snap with some jq magic™
    yaml2json < "$SEED_YAML"| \
        jq --arg FILE "$SNAP_FILE" --arg NAME "$SNAP_NAME" \
            '.snaps[.snaps| length] |= .  + {"channel":"stable","unasserted":true,"name":$NAME,"file":$FILE}' | \
                json2yaml > "$SEED_YAML.tmp"
    mv "$SEED_YAML.tmp" "$SEED_YAML"

    # and remember to copy the new snap file to the seed
    cp "$SNAP_FILE" "$SEED_SNAPS_DIR"

    # check that we didn't break things too badly
    snap debug validate-seed "$SEED_YAML"
}
