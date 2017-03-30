#!/bin/bash

set -eux

. $TESTSLIB/apt.sh
. $TESTSLIB/snaps.sh

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
    core="$(readlink -f /snap/core/current || readlink -f /snap/ubuntu-core/current)"
    snap="$(mount | grep " $core" | awk '{print $1}')"
    umount --verbose "$core"

    # Now unpack the core, inject the new snap-exec/snapctl into it
    unsquashfs "$snap"
    cp /usr/lib/snapd/snap-exec squashfs-root/usr/lib/snapd/
    cp /usr/bin/snapctl squashfs-root/usr/bin/
    # also inject new version of snap-confine and snap-scard-ns
    cp /usr/lib/snapd/snap-discard-ns squashfs-root/usr/lib/snapd/
    cp /usr/lib/snapd/snap-confine squashfs-root/usr/lib/snapd/
    # also add snap/snapd because we re-exec by default and want to test
    # this version
    cp /usr/lib/snapd/snapd squashfs-root/usr/lib/snapd/
    cp /usr/lib/snapd/info squashfs-root/usr/lib/snapd/
    cp /usr/bin/snap squashfs-root/usr/bin/snap
    # repack, cheating to speed things up (4sec vs 1.5min)
    mv "$snap" "${snap}.orig"
    mksnap_fast "squashfs-root" "$snap"
    rm -rf squashfs-root

    # Now mount the new core snap
    mount "$snap" "$core"

    # Make sure we're running with the correct copied bits
    for p in /usr/lib/snapd/snap-exec /usr/lib/snapd/snap-confine /usr/lib/snapd/snap-discard-ns /usr/bin/snapctl /usr/lib/snapd/snapd /usr/bin/snap; do
        if ! cmp ${p} ${core}${p}; then
            echo "$p in tree and $p in core snap are unexpectedly not the same"
            exit 1
        fi
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
    apt_install_local ${GOPATH}/snapd_*.deb
    if snap --version |MATCH unknown; then
        echo "Package build incorrect, 'snap --version' mentions 'unknown'"
        snap --version
        apt-cache policy snapd
        exit 1
    fi
    if /usr/lib/snapd/snap-confine --version | MATCH unknown; then
        echo "Package build incorrect, 'snap-confine --version' mentions 'unknown'"
        /usr/lib/snapd/snap-confine --version
        apt-cache policy snap-confine
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
        . $TESTSLIB/store.sh
        setup_staging_store
    fi

    # Snapshot the state including core.
    if [ ! -f $SPREAD_PATH/snapd-state.tar.gz ]; then
        ! snap list | grep core || exit 1
        # use parameterized core channel (defaults to edge) instead
        # of a fixed one and close to stable in order to detect defects
        # earlier
        snap install --${CORE_CHANNEL} core
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
        mounts="$(systemctl list-unit-files --full | grep '^snap[-.].*\.mount' | cut -f1 -d ' ')"
        services="$(systemctl list-unit-files --full | grep '^snap[-.].*\.service' | cut -f1 -d ' ')"
        for unit in $services $mounts; do
            systemctl stop $unit
        done
        tar czf $SPREAD_PATH/snapd-state.tar.gz /var/lib/snapd /snap /etc/systemd/system/snap-*core*.mount
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
        for unit in $mounts $services; do
            systemctl start $unit
        done
        systemctl start snapd.socket
    fi
}

setup_reflash_magic() {
        # install the stuff we need
        apt-get install -y kpartx busybox-static
        apt_install_local ${GOPATH}/snapd_*.deb
        apt-get clean

        snap install --${CORE_CHANNEL} core

        # install ubuntu-image
        snap install --devmode --edge ubuntu-image

        # needs to be under /home because ubuntu-device-flash
        # uses snap-confine and that will hide parts of the hostfs
        IMAGE_HOME=/home/image
        mkdir -p $IMAGE_HOME

        # modify the core snap so that the current root-pw works there
        # for spread to do the first login
        UNPACKD="/tmp/core-snap"
        unsquashfs -d $UNPACKD /var/lib/snapd/snaps/core_*.snap

        # FIXME: netplan workaround
        mkdir -p $UNPACKD/etc/netplan

        # set root pw by concating root line from host and rest from core
        want_pw="$(grep ^root /etc/shadow)"
        echo "$want_pw" > /tmp/new-shadow
        tail -n +2 /etc/shadow >> /tmp/new-shadow
        cp -v /tmp/new-shadow $UNPACKD/etc/shadow
        cp -v /etc/passwd $UNPACKD/etc/passwd

        # ensure spread -reuse works in the core image as well
        if [ -e /.spread.yaml ]; then
            cp -av /.spread.yaml $UNPACKD
        fi

        # we need the test user in the image
        # see the comment in spread.yaml about 12345
        sed -i "s/^test.*$//" $UNPACKD/etc/{shadow,passwd}
        chroot $UNPACKD addgroup --quiet --gid 12345 test
        chroot $UNPACKD adduser --quiet --no-create-home --uid 12345 --gid 12345 --disabled-password --gecos '' test
        echo 'test ALL=(ALL) NOPASSWD:ALL' >> $UNPACKD/etc/sudoers.d/99-test-user

        echo 'ubuntu ALL=(ALL) NOPASSWD:ALL' >> $UNPACKD/etc/sudoers.d/99-ubuntu-user

        # modify sshd so that we can connect as root
        sed -i 's/\(PermitRootLogin\|PasswordAuthentication\)\>.*/\1 yes/' $UNPACKD/etc/ssh/sshd_config

        # FIXME: install would be better but we don't have dpkg on
        #        the image
        # unpack our freshly build snapd into the new core snap
        dpkg-deb -x ${SPREAD_PATH}/../snapd_*.deb $UNPACKD

        # add gpio and iio slots
        cat >> $UNPACKD/meta/snap.yaml <<-EOF
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
        snapbuild $UNPACKD $IMAGE_HOME

        # FIXME: fetch directly once its in the assertion service
        cp "$TESTSLIB/assertions/pc-${REMOTE_STORE}.model" $IMAGE_HOME/pc.model

        # FIXME: how to test store updated of ubuntu-core with sideloaded snap?
        IMAGE=all-snap-amd64.img

        # ensure that ubuntu-image is using our test-build of snapd with the
        # test keys and not the bundled version of usr/bin/snap from the snap.
        # Note that we can not put it into /usr/bin as '/usr' is different
        # when the snap uses confinement.
        cp /usr/bin/snap $IMAGE_HOME
        export UBUNTU_IMAGE_SNAP_CMD=$IMAGE_HOME/snap

        # download pc-kernel snap for the specified channel
        snap download --channel="$KERNEL_CHANNEL" pc-kernel

        /snap/bin/ubuntu-image -w $IMAGE_HOME $IMAGE_HOME/pc.model \
                               --channel edge \
                               --extra-snaps $IMAGE_HOME/core_*.snap \
                               --extra-snaps $PWD/pc-kernel_*.snap \
                               --output $IMAGE_HOME/$IMAGE
        rm ./pc-kernel*

        # mount fresh image and add all our SPREAD_PROJECT data
        kpartx -avs $IMAGE_HOME/$IMAGE
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
        (cd /tmp ; unsquashfs -v $IMAGE_HOME/core_*.snap etc/systemd/system)
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
        kpartx -d  $IMAGE_HOME/$IMAGE

        # the reflash magic
        # FIXME: ideally in initrd, but this is good enough for now
        cat > $IMAGE_HOME/reflash.sh << EOF
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
        chmod +x $IMAGE_HOME/reflash.sh

        # extract ROOT from /proc/cmdline
        ROOT=$(cat /proc/cmdline | sed -e 's/^.*root=//' -e 's/ .*$//')
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
    if [ $SPREAD_REBOOT = 1 ]; then
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
    . $TESTSLIB/names.sh
    for name in $gadget_name $kernel_name core; do
        if ! snap list | grep $name; then
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
    if [ ! -f $SPREAD_PATH/snapd-state.tar.gz ]; then
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
        tar czf $SPREAD_PATH/snapd-state.tar.gz /var/lib/snapd $BOOT
        systemctl start snapd.socket
    fi
}
