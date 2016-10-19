#!/bin/bash

set -eux

. $TESTSLIB/apt.sh

update_core_snap_with_snap_exec_snapctl() {
    # We want to use the in-tree snap-exec and snapctl, not the ones in the core
    # snap. To accomplish that, we'll just unpack the core we just grabbed,
    # shove the new snap-exec and snapctl in there, and repack it.

    # First of all, unmount the core
    core="$(readlink -f /snap/core/current || readlink -f /snap/ubuntu-core/current)"
    snap="$(mount | grep " $core" | awk '{print $1}')"
    umount --verbose "$core"

    # Now unpack the core, inject the new snap-exec and snapctl into it, and
    # repack it.
    unsquashfs "$snap"
    cp /usr/lib/snapd/snap-exec squashfs-root/usr/lib/snapd/
    cp /usr/bin/snapctl squashfs-root/usr/bin/
    mv "$snap" "${snap}.orig"
    mksquashfs squashfs-root "$snap" -comp xz
    rm -rf squashfs-root

    # Now mount the new core snap
    mount "$snap" "$core"

    # Make sure we're running with the correct snap-exec
    if ! cmp /usr/lib/snapd/snap-exec ${core}/usr/lib/snapd/snap-exec; then
        echo "snap-exec in tree and snap-exec in core snap are unexpectedly not the same"
        exit 1
    fi

    # Make sure we're running with the correct snapctl
    if ! cmp /usr/bin/snapctl ${core}/usr/bin/snapctl; then
        echo "snapctl in tree and snapctl in core snap are unexpectedly not the same"
        exit 1
    fi
}

prepare_classic() {
    apt_install_local ${SPREAD_PATH}/../snapd_*.deb

    # Snapshot the state including core.
    if [ ! -f $SPREAD_PATH/snapd-state.tar.gz ]; then
        ! snap list | grep core || exit 1
        # FIXME: go back to stable once we have a stable release with
        #        the snap-exec fix
        snap install --candidate core
        snap list | grep core

        echo "Ensure that the grub-editenv list output is empty on classic"
        output=$(grub-editenv list)
        if [ -n "$output" ]; then
            echo "Expected empty grub environment, got:"
            echo "$output"
            exit 1
        fi

        systemctl stop snapd.service snapd.socket

        update_core_snap_with_snap_exec_snapctl

        systemctl daemon-reload
        mounts="$(systemctl list-unit-files | grep '^snap[-.].*\.mount' | cut -f1 -d ' ')"
        services="$(systemctl list-unit-files | grep '^snap[-.].*\.service' | cut -f1 -d ' ')"
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
        apt_install_local ${SPREAD_PATH}/../snapd_*.deb
        apt-get clean

        snap install --edge core

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

        # build new core snap for the image
        snapbuild $UNPACKD $IMAGE_HOME

        # FIXME: fetch directly once its in the assertion service
        cat > $IMAGE_HOME/pc.model <<EOF
type: model
authority-id: canonical
series: 16
brand-id: canonical
model: pc
architecture: amd64
gadget: pc
kernel: pc-kernel
timestamp: 2016-08-31T00:00:00.0Z
sign-key-sha3-384: 9tydnLa6MTJ-jaQTFUXEwHl1yRx7ZS4K5cyFDhYDcPzhS7uyEkDxdUjg9g08BtNn

AcLBXAQAAQoABgUCV8lRDQAKCRDgT5vottzAEg9GEACsSb+qXB34mwESsd7ns6VpM9BfAOOSstwB
KJlWOlcJ39M7is/fO+dxRH4XsI7Td6BI1WEf5188sJuld8APUsTPn8tPYN3JB5CJ8Edkr6p78YUW
f3Wo26USAE32ewjq9kHo6uBqIr4VixjTXfGUeDXc7tvKcduIMokSKjDLRHJRur1NC8LjkBn2ZPi8
9d0BpJzr5y8wK0yFEyAhaS8H8LvL7VMjKG7/BkZcQ0a3jv69qh9jdmxnKDN2zcd1btRR1Giew3gw
VJ8lNtfxQSWi+nYNEuzDqwKdffo9sVyCzBC+vEH3xYYk8NpRx2QgCSzDCPMoxaJgLwhAeWz6mHQp
8EaGOsMZm7c85BXUcdJGEhZ5MpNGSzCb/ifgOKBB6zYzekiQh4TVLgi9Uk/acsLH75vNrI8Kwyl+
r4Pahf///LbeWNwcEonaSV48S5fg3QqxEQeb42xcp6wPfRr7LN1LvQ9kRQTt42GDAlIva5HKlo0T
cUb5A4zz3IlBn/KQ4BS/2sBcixrH97tHInef4oA8IrBiBDGnIv/s4qyZ+gB5fX8Ohnn/a5bUgU5u
GmwRQ12Ix54YGJrzZocu1AiQINij4s6ZSoJAEJobI9VBK8WnV8PRmra6UJonV+qrJOiSKTJVCkAF
+RFartQL+pjF/H29FsyBkIEcPwhTslxWKUWajHsExw==
EOF

        # FIXME: how to test store updated of ubuntu-core with sideloaded snap?
        IMAGE=all-snap-amd64.img

        # ensure that ubuntu-image is using our test-build of snapd with the
        # test keys and not the bundled version of usr/bin/snap from the snap.
        # Note that we can not put it into /usr/bin as '/usr' is different
        # when the snap uses confinement.
        cp /usr/bin/snap $IMAGE_HOME
        export UBUNTU_IMAGE_SNAP_CMD=$IMAGE_HOME/snap
        /snap/bin/ubuntu-image -w $IMAGE_HOME $IMAGE_HOME/pc.model --channel edge --extra-snaps $IMAGE_HOME/core_*.snap  --output $IMAGE_HOME/$IMAGE

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
Environment=SNAPD_DEBUG_HTTP=7 SNAP_REEXEC=0
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
    for name in $gadget_name $kernel_name $core_name; do
        if ! snap list | grep $name; then
            echo "Not all fundamental snaps are available, all-snap image not valid"
            echo "Currently installed snaps"
            snap list
            exit 1
        fi
    done

    echo "Kernel has a store revision"
    snap list|grep ^${kernel_name}|grep -E " [0-9]+\s+canonical"

    # Snapshot the fresh state
    if [ ! -f $SPREAD_PATH/snapd-state.tar.gz ]; then
        systemctl stop snapd.service snapd.socket
        tar czf $SPREAD_PATH/snapd-state.tar.gz /var/lib/snapd
        systemctl start snapd.socket
    fi
}
