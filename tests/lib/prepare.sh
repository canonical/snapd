#!/bin/bash

set -eux

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"
# shellcheck source=tests/lib/apt.sh
. "$TESTSLIB/apt.sh"
# shellcheck source=tests/lib/snaps.sh
. "$TESTSLIB/snaps.sh"

disable_kernel_rate_limiting() {
    # kernel rate limiting hinders debugging security policy so turn it off
    echo "Turning off kernel rate-limiting"
    # TODO: we should be able to run the tests with rate limiting disabled so
    # debug output is robust, but we currently can't :(
    echo "SKIPPED: see https://forum.snapcraft.io/t/snapd-spread-tests-should-be-able-to-run-with-kernel-rate-limiting-disabled/424"
    #sysctl -w kernel.printk_ratelimit=0
}

update_core_snap_for_classic_reexec() {
    # it is possible to disable this to test that snapd (the deb) works
    # fine with whatever is in the core snap
    if [ "$MODIFY_CORE_SNAP_FOR_REEXEC" != "1" ]; then
        echo "Not modifying the core snap as requested via MODIFY_CORE_SNAP_FOR_REEXEC"
        return
    fi

    # We want to use the in-tree snap/snapd/snap-exec/snapctl, because
    # we re-exec by default.
    # To accomplish that, we'll just unpack the core we just grabbed,
    # shove the new snap-exec and snapctl in there, and repack it.

    # First of all, unmount the core
    core="$(readlink -f "$SNAPMOUNTDIR/core/current" || readlink -f "$SNAPMOUNTDIR/ubuntu-core/current")"
    snap="$(mount | grep " $core" | awk '{print $1}')"
    umount --verbose "$core"

    # Now unpack the core, inject the new snap-exec/snapctl into it
    unsquashfs "$snap"
    # clean the old snapd libexec binaries, just in case
    rm squashfs-root/usr/lib/snapd/*
    # and copy in the current ones
    cp -a "$LIBEXECDIR"/snapd/* squashfs-root/usr/lib/snapd/
    # also the binaries themselves
    cp -a /usr/bin/{snap,snapctl} squashfs-root/usr/bin/
    # and snap-confine's apparmor
    if [ -e /etc/apparmor.d/usr.lib.snapd.snap-confine.real ]; then
        cp -a /etc/apparmor.d/usr.lib.snapd.snap-confine.real squashfs-root/etc/apparmor.d/usr.lib.snapd.snap-confine.real
    else
        cp -a /etc/apparmor.d/usr.lib.snapd.snap-confine      squashfs-root/etc/apparmor.d/usr.lib.snapd.snap-confine.real
    fi

    # repack, cheating to speed things up (4sec vs 1.5min)
    mv "$snap" "${snap}.orig"
    mksnap_fast "squashfs-root" "$snap"
    rm -rf squashfs-root

    # Now mount the new core snap
    mount "$snap" "$core"

    check_file() {
        if ! cmp "$1" "$2" ; then
            echo "$1 in tree and $2 in core snap are unexpectedly not the same"
            exit 1
        fi
    }

    # Make sure we're running with the correct copied bits
    for p in "$LIBEXECDIR/snapd/snap-exec" "$LIBEXECDIR/snapd/snap-confine" "$LIBEXECDIR/snapd/snap-discard-ns" "$LIBEXECDIR/snapd/snapd"; do
        check_file "$p" "$core/usr/lib/snapd/$(basename "$p")"
    done
    for p in /usr/bin/snapctl /usr/bin/snap; do
        check_file "$p" "$core$p"
    done
}

prepare_each_classic() {
    mkdir -p /etc/systemd/system/snapd.service.d
    if [ -z "${SNAP_REEXEC:-}" ]; then
        rm -f /etc/systemd/system/snapd.service.d/reexec.conf
    else
        cat <<EOF > /etc/systemd/system/snapd.service.d/reexec.conf
[Service]
Environment=SNAP_REEXEC=$SNAP_REEXEC
EOF
    fi
    if [ ! -f /etc/systemd/system/snapd.service.d/local.conf ]; then
        echo "/etc/systemd/system/snapd.service.d/local.conf vanished!"
        exit 1
    fi
}

prepare_classic() {
    install_build_snapd
    if snap --version |MATCH unknown; then
        echo "Package build incorrect, 'snap --version' mentions 'unknown'"
        snap --version
        apt-cache policy snapd
        exit 1
    fi
    if "$LIBEXECDIR/snapd/snap-confine" --version | MATCH unknown; then
        echo "Package build incorrect, 'snap-confine --version' mentions 'unknown'"
        apt-cache policy snap-confine
        "$LIBEXECDIR/snapd/snap-confine" --version
        exit 1
    fi

    mkdir -p /etc/systemd/system/snapd.service.d
    cat <<EOF > /etc/systemd/system/snapd.service.d/local.conf
[Unit]
StartLimitInterval=0
[Service]
Environment=SNAPD_DEBUG_HTTP=7 SNAPD_DEBUG=1 SNAPPY_TESTING=1 SNAPD_CONFIGURE_HOOK_TIMEOUT=30s
EOF
    mkdir -p /etc/systemd/system/snapd.socket.d
    cat <<EOF > /etc/systemd/system/snapd.socket.d/local.conf
[Unit]
StartLimitInterval=0
EOF

    if [ "$REMOTE_STORE" = staging ]; then
        # shellcheck source=tests/lib/store.sh
        . "$TESTSLIB/store.sh"
        setup_staging_store
    fi

    # Snapshot the state including core.
    if [ ! -f "$SPREAD_PATH/snapd-state.tar.gz" ]; then
        ! snap list | grep core || exit 1
        # use parameterized core channel (defaults to edge) instead
        # of a fixed one and close to stable in order to detect defects
        # earlier
        snap install --"$CORE_CHANNEL" core
        snap list | grep core

        # ensure no auto-refresh happens during the tests
        if [ -e /snap/core/current/meta/hooks/configure ]; then
            snap set core refresh.schedule="$(date +%a --date=2days)@12:00-14:00"
            snap set core refresh.disabled=true
        fi

        echo "Ensure that the grub-editenv list output is empty on classic"
        output=$(grub-editenv list)
        if [ -n "$output" ]; then
            echo "Expected empty grub environment, got:"
            echo "$output"
            exit 1
        fi

        systemctl stop snapd.service snapd.socket

        update_core_snap_for_classic_reexec

        systemctl daemon-reload
        escaped_snap_mount_dir="$(systemd-escape --path "$SNAPMOUNTDIR")"
        mounts="$(systemctl list-unit-files --full | grep "^$escaped_snap_mount_dir[-.].*\.mount" | cut -f1 -d ' ')"
        services="$(systemctl list-unit-files --full | grep "^$escaped_snap_mount_dir[-.].*\.service" | cut -f1 -d ' ')"
        for unit in $services $mounts; do
            systemctl stop "$unit"
        done
        tar czf "$SPREAD_PATH"/snapd-state.tar.gz /var/lib/snapd "$SNAPMOUNTDIR" /etc/systemd/system/"$escaped_snap_mount_dir"-*core*.mount
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
        for unit in $mounts $services; do
            systemctl start "$unit"
        done
        systemctl start snapd.socket
    fi

    if [[ "$SPREAD_SYSTEM" == debian-* || "$SPREAD_SYSTEM" == ubuntu-* ]]; then
        # Improve entropy for the whole system quite a lot to get fast
        # key generation during our test cycles
        apt-get install rng-tools
        echo "HRNGDEVICE=/dev/urandom" > /etc/default/rng-tools
        /etc/init.d/rng-tools restart
    fi

    disable_kernel_rate_limiting
}

setup_reflash_magic() {
        # install the stuff we need
        distro_install_package kpartx busybox-static
        distro_install_local_package "$GOHOME"/snapd_*.deb
        distro_clean_package_cache

        snap install "--${CORE_CHANNEL}" core

        # install ubuntu-image
        snap install --classic --edge ubuntu-image

        # needs to be under /home because ubuntu-device-flash
        # uses snap-confine and that will hide parts of the hostfs
        IMAGE_HOME=/home/image
        mkdir -p "$IMAGE_HOME"

        # modify the core snap so that the current root-pw works there
        # for spread to do the first login
        UNPACKD="/tmp/core-snap"
        unsquashfs -d "$UNPACKD" /var/lib/snapd/snaps/core_*.snap

        # FIXME: netplan workaround
        mkdir -p "$UNPACKD/etc/netplan"

        # set root pw by concating root line from host and rest from core
        want_pw="$(grep ^root /etc/shadow)"
        echo "$want_pw" > /tmp/new-shadow
        tail -n +2 /etc/shadow >> /tmp/new-shadow
        cp -v /tmp/new-shadow "$UNPACKD/etc/shadow"
        cp -v /etc/passwd "$UNPACKD/etc/passwd"

        # ensure spread -reuse works in the core image as well
        if [ -e /.spread.yaml ]; then
            cp -av /.spread.yaml "$UNPACKD"
        fi

        # we need the test user in the image
        # see the comment in spread.yaml about 12345
        sed -i 's/^test.*$//' "$UNPACKD"/etc/{shadow,passwd}
        chroot "$UNPACKD" addgroup --quiet --gid 12345 test
        chroot "$UNPACKD" adduser --quiet --no-create-home --uid 12345 --gid 12345 --disabled-password --gecos '' test
        echo 'test ALL=(ALL) NOPASSWD:ALL' >> "$UNPACKD/etc/sudoers.d/99-test-user"

        echo 'ubuntu ALL=(ALL) NOPASSWD:ALL' >> "$UNPACKD/etc/sudoers.d/99-ubuntu-user"

        # modify sshd so that we can connect as root
        sed -i 's/\(PermitRootLogin\|PasswordAuthentication\)\>.*/\1 yes/' "$UNPACKD/etc/ssh/sshd_config"

        # FIXME: install would be better but we don't have dpkg on
        #        the image
        # unpack our freshly build snapd into the new core snap
        dpkg-deb -x "$SPREAD_PATH"/../snapd_*.deb "$UNPACKD"

        # add gpio and iio slots
        cat >> "$UNPACKD/meta/snap.yaml" <<-EOF
slots:
    gpio-pin:
        interface: gpio
        number: 100
        direction: out
    iio0:
        interface: iio
        path: /dev/iio:device0
EOF

        # build new core snap for the image
        snapbuild "$UNPACKD" "$IMAGE_HOME"

        # FIXME: fetch directly once its in the assertion service
        cp "$TESTSLIB/assertions/pc-${REMOTE_STORE}.model" "$IMAGE_HOME/pc.model"

        # FIXME: how to test store updated of ubuntu-core with sideloaded snap?
        IMAGE=all-snap-amd64.img

        # ensure that ubuntu-image is using our test-build of snapd with the
        # test keys and not the bundled version of usr/bin/snap from the snap.
        # Note that we can not put it into /usr/bin as '/usr' is different
        # when the snap uses confinement.
        cp /usr/bin/snap "$IMAGE_HOME"
        export UBUNTU_IMAGE_SNAP_CMD="$IMAGE_HOME/snap"

        EXTRA_FUNDAMENTAL=
        IMAGE_CHANNEL=edge
        if [ "$KERNEL_CHANNEL" = "$GADGET_CHANNEL" ]; then
            IMAGE_CHANNEL="$KERNEL_CHANNEL"
        else
            # download pc-kernel snap for the specified channel and set ubuntu-image channel
            # to gadget, so that we don't need to download it
            snap download --channel="$KERNEL_CHANNEL" pc-kernel

            EXTRA_FUNDAMENTAL="--extra-snaps $PWD/pc-kernel_*.snap"
            IMAGE_CHANNEL="$GADGET_CHANNEL"
        fi

        /snap/bin/ubuntu-image -w "$IMAGE_HOME" "$IMAGE_HOME/pc.model" \
                               --channel "$IMAGE_CHANNEL" \
                               "$EXTRA_FUNDAMENTAL" \
                               --extra-snaps "$IMAGE_HOME"/core_*.snap \
                               --output "$IMAGE_HOME/$IMAGE"
        rm -f ./pc-kernel_*.{snap,assert} ./pc_*.{snap,assert}

        # mount fresh image and add all our SPREAD_PROJECT data
        kpartx -avs "$IMAGE_HOME/$IMAGE"
        # FIXME: hardcoded mapper location, parse from kpartx
        mount /dev/mapper/loop2p3 /mnt
        mkdir -p /mnt/user-data/
        cp -ar /home/gopath /mnt/user-data/

        # create test user home dir
        mkdir -p /mnt/user-data/test
        # using symbolic names requires test:test have the same ids
        # inside and outside which is a pain (see 12345 above), but
        # using the ids directly is the wrong kind of fragile
        chown --verbose test:test /mnt/user-data/test

        # we do what sync-dirs is normally doing on boot, but because
        # we have subdirs/files in /etc/systemd/system (created below)
        # the writeable-path sync-boot won't work
        mkdir -p /mnt/system-data/etc/systemd
        (cd /tmp ; unsquashfs -v "$IMAGE_HOME"/core_*.snap etc/systemd/system)
        cp -avr /tmp/squashfs-root/etc/systemd/system /mnt/system-data/etc/systemd/

        # FIXUP silly systemd
        mkdir -p /mnt/system-data/etc/systemd/system/snapd.service.d
        cat <<EOF > /mnt/system-data/etc/systemd/system/snapd.service.d/local.conf
[Unit]
StartLimitInterval=0
[Service]
Environment=SNAPD_DEBUG_HTTP=7 SNAPD_DEBUG=1 SNAPPY_TESTING=1 SNAPPY_USE_STAGING_STORE=$SNAPPY_USE_STAGING_STORE
ExecStartPre=/bin/touch /dev/iio:device0
EOF
        mkdir -p /mnt/system-data/etc/systemd/system/snapd.socket.d
        cat <<EOF > /mnt/system-data/etc/systemd/system/snapd.socket.d/local.conf
[Unit]
StartLimitInterval=0
EOF

        umount /mnt
        kpartx -d  "$IMAGE_HOME/$IMAGE"

        # the reflash magic
        # FIXME: ideally in initrd, but this is good enough for now
        cat > "$IMAGE_HOME/reflash.sh" << EOF
#!/bin/sh -ex
mount -t tmpfs none /tmp
cp /bin/busybox /tmp
cp $IMAGE_HOME/$IMAGE /tmp
sync
# blow away everything
/tmp/busybox dd if=/tmp/$IMAGE of=/dev/sda bs=4M
# and reboot
/tmp/busybox sync
/tmp/busybox echo b > /proc/sysrq-trigger
EOF
        chmod +x "$IMAGE_HOME/reflash.sh"

        # extract ROOT from /proc/cmdline
        ROOT=$(sed -e 's/^.*root=//' -e 's/ .*$//' /proc/cmdline)
        cat >/boot/grub/grub.cfg <<EOF
set default=0
set timeout=2
menuentry 'flash-all-snaps' {
linux /vmlinuz root=$ROOT ro init=$IMAGE_HOME/reflash.sh console=ttyS0
initrd /initrd.img
}
EOF
}

prepare_all_snap() {
    # we are still a "classic" image, prepare the surgery
    if [ -e /var/lib/dpkg/status ]; then
        setup_reflash_magic
        REBOOT
    fi

    # verify after the first reboot that we are now in the all-snap world
    if [ "$SPREAD_REBOOT" = 1 ]; then
        echo "Ensure we are now in an all-snap world"
        if [ -e /var/lib/dpkg/status ]; then
            echo "Rebooting into all-snap system did not work"
            exit 1
        fi
    fi

    echo "Wait for firstboot change to be ready"
    while ! snap changes | grep "Done"; do
        snap changes || true
        snap change 1 || true
        sleep 1
    done

    echo "Ensure fundamental snaps are still present"
    # shellcheck source=tests/lib/names.sh
    . "$TESTSLIB/names.sh"
    for name in "$gadget_name" "$kernel_name" core; do
        if ! snap list "$name"; then
            echo "Not all fundamental snaps are available, all-snap image not valid"
            echo "Currently installed snaps"
            snap list
            exit 1
        fi
    done

    # ensure no auto-refresh happens during the tests
    if [ -e /snap/core/current/meta/hooks/configure ]; then
        snap set core refresh.schedule="$(date +%a --date=2days)@12:00-14:00"
        snap set core refresh.disabled=true
    fi

    # Snapshot the fresh state (including boot/bootenv)
    if [ ! -f "$SPREAD_PATH/snapd-state.tar.gz" ]; then
        # we need to ensure that we also restore the boot environment
        # fully for tests that break it
        BOOT=""
        if ls /boot/uboot/*; then
            BOOT=/boot/uboot/
        elif ls /boot/grub/*; then
            BOOT=/boot/grub/
        else
            echo "Cannot determine bootdir in /boot:"
            ls /boot
            exit 1
        fi

        systemctl stop snapd.service snapd.socket
        tar czf "$SPREAD_PATH/snapd-state.tar.gz" /var/lib/snapd $BOOT
        systemctl start snapd.socket
    fi

    disable_kernel_rate_limiting
}
