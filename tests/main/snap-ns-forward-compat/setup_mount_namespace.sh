#! /bin/bash

set -ex

setup_mount_namespace() {
    MOUNT_TMPDIR="$1"
    mount --make-rshared /
    mount -o bind "$MOUNT_TMPDIR" "$MOUNT_TMPDIR"
    mount --make-unbindable "$MOUNT_TMPDIR"

    echo "mount the tmpfs as /"
    mount -t tmpfs -o mode=770,x-snapd.origin=rootfs none "$MOUNT_TMPDIR"

    echo "Mount a few dirs from the core snap"
    #shellcheck disable=SC2043
    for d in usr
    do
        mkdir "$MOUNT_TMPDIR/$d"
        mount -o rbind "/$SNAP_MOUNT_DIR/core20/current/$d" "$MOUNT_TMPDIR/$d"
        mount --make-rslave "$MOUNT_TMPDIR/$d"
    done
    for d in bin lib lib32 lib64 libx32 sbin
    do
        ln -s usr/$d "$MOUNT_TMPDIR/$d"
    done

    echo "Mount a few dirs from the root fs"
    for d in dev etc home proc root run sys tmp var
    do
        mkdir "$MOUNT_TMPDIR/$d"
        mount -o rbind "/$d" "$MOUNT_TMPDIR/$d"
        mount --make-rslave "$MOUNT_TMPDIR/$d"
    done

    LIBEXECDIR=$(os.paths libexec-dir)
    mkdir -p "$MOUNT_TMPDIR/usr/lib/snapd"
    mount -o rbind "$LIBEXECDIR/snapd" "$MOUNT_TMPDIR/usr/lib/snapd"
    mount --make-rslave "$MOUNT_TMPDIR/usr/lib/snapd"

    mkdir "$MOUNT_TMPDIR/snap"
    mount -o rbind "$SNAP_MOUNT_DIR" "$MOUNT_TMPDIR/snap"
    mount --make-rslave "$MOUNT_TMPDIR/snap"

    echo "Create a sentinel file in the tmpfs"
    touch "$MOUNT_TMPDIR/this-is-our-rootfs"

    echo "Mount the hostfs somewhere"
    mkdir -p "$MOUNT_TMPDIR/var/lib/snapd/hostfs"
    mount -o bind "$MOUNT_TMPDIR/var/lib/snapd/hostfs" "$MOUNT_TMPDIR/var/lib/snapd/hostfs"
    mount --make-private "$MOUNT_TMPDIR/var/lib/snapd/hostfs"

    echo "Do pivot_root"
    cd "$MOUNT_TMPDIR"
    pivot_root . "var/lib/snapd/hostfs"
    # Clear bash cache of executable paths, because in some distros (like arch)
    # the mount command is normally found in /usr/sbin/mount, whereas after the
    # pivot_root we'll be using the one from the core snap, which is in
    # /usr/bin/
    hash -r

    echo "Cleanup filesystem"
    umount "/var/lib/snapd/hostfs/$MOUNT_TMPDIR"
    mount --make-rslave /var/lib/snapd/hostfs
    for d in dev sys proc
    do
        umount -l "/var/lib/snapd/hostfs/$d"
    done

    echo "Mount namespace has been set up"
}

setup_mount_namespace "$@"
