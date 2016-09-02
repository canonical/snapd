#!/bin/bash

set -eux

prepare_classic() {
    apt install -y ${SPREAD_PATH}/../snapd_*.deb
    # Snapshot the state including core.
    if [ ! -f $SPREAD_PATH/snapd-state.tar.gz ]; then
        ! snap list | grep core || exit 1
        snap install ubuntu-core
        snap list | grep core

        systemctl stop snapd.service snapd.socket
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
        apt install -y kpartx busybox-static
        apt install -y ${SPREAD_PATH}/../snapd_*.deb
        
        snap install --edge ubuntu-core

        # install ubuntu-image
        snap install --devmode --edge ubuntu-image

        # needs to be under /home because ubuntu-device-flash
        # uses snap-confine and that will hide parts of the hostfs
        IMAGE_HOME=/home/image
        mkdir -p $IMAGE_HOME

        # modify the core snap so that the current root-pw works there
        # for spread to do the first login
        UNPACKD="/tmp/ubuntu-core-snap"
        unsquashfs -d $UNPACKD /var/lib/snapd/snaps/ubuntu-core_*.snap

        # FIXME: netplan workaround
        mkdir -p $UNPACKD/etc/netplan

        # set root pw by concating root line from host and rest from core
        want_pw="$(grep ^root /etc/shadow)"
        echo "$want_pw" > /tmp/new-shadow
        tail -n +2 /etc/shadow >> /tmp/new-shadow
        cp -v /tmp/new-shadow $UNPACKD/etc/shadow

        # ensure spread -reuse works in the core image as well
        if [ -e /.spread.yaml ]; then
            cp -av /.spread.yaml $UNPACKD
        fi
        
        # we need the test user in the image
        chroot $UNPACKD adduser --quiet --no-create-home --disabled-password --gecos '' test

        # modify sshd so that we can connect as root
        sed -i 's/\(PermitRootLogin\|PasswordAuthentication\)\>.*/\1 yes/' $UNPACKD/etc/ssh/sshd_config

        # FIXME: install would be better but we don't have dpkg on
        #        the image
        # unpack our freshly build snapd into the new core snap
        dpkg-deb -x ${SPREAD_PATH}/../snapd_*.deb $UNPACKD
        
        # build new core snap for the image
        snapbuild $UNPACKD $IMAGE_HOME

        # FIXME: remove once we have a proper model.assertion
        . $TESTSLIB/store.sh
        STORE_DIR=/tmp/fake-store-blobdir
        mkdir -p $STORE_DIR
        setup_store fake-w-assert-fallback $STORE_DIR
        cp $TESTSLIB/assertions/testrootorg-store.account-key $STORE_DIR/asserts
        cp $TESTSLIB/assertions/developer1.account $STORE_DIR/asserts
        cp $TESTSLIB/assertions/developer1.account-key $STORE_DIR/asserts

        # FIXME: how to test store updated of ubuntu-core with sideloaded snap?
        export SNAPPY_FORCE_SAS_URL=http://localhost:11028
        IMAGE=all-snap-amd64.img
        # ensure that ubuntu-image is using our test-build of snapd with the
        # test keys and not the bundled version of usr/bin/snap from the snap.
        # Note that we can not put it into /usr/bin as '/usr' is different
        # when the snap uses confinement.
        cp /usr/bin/snap $IMAGE_HOME
        export UBUNTU_IMAGE_SNAP_CMD=$IMAGE_HOME/snap
        /snap/bin/ubuntu-image -w $IMAGE_HOME $TESTSLIB/assertions/developer1-pc.model --channel edge --extra-snaps $IMAGE_HOME/ubuntu-core_*.snap  --output $IMAGE_HOME/$IMAGE

        # teardown store
        teardown_store fake $STORE_DIR

        # mount fresh image and add all our SPREAD_PROJECT data
        kpartx -avs $IMAGE_HOME/$IMAGE
        # FIXME: hardcoded mapper location, parse from kpartx
        mount /dev/mapper/loop2p3 /mnt
        mkdir -p /mnt/user-data/
        cp -avr /home/gopath /mnt/user-data/

        # create test user home dir
        mkdir -p /mnt/user-data/test
        chown 1001:1001 /mnt/user-data/test

        # we do what sync-dirs is normally doing on boot, but because
        # we have subdirs/files in /etc/systemd/system (created below)
        # the writeable-path sync-boot won't work
        mkdir -p /mnt/system-data/etc/systemd
        (cd /tmp ; unsquashfs -v $IMAGE_HOME/ubuntu-core_*.snap etc/systemd/system)
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
[Service]
Environment=SNAPD_DEBUG_HTTP=7 SNAP_REEXEC=0
EOF

        # manually create cloud-init configuration
        # FIXME: move this to ubuntu-image once it supports it
        mkdir -p /mnt/system-data/var/lib/cloud/seed/nocloud-net
        cat <<EOF > /mnt/system-data/var/lib/cloud/seed/nocloud-net/meta-data
instance-id: nocloud-static
EOF
        cat <<EOF > /mnt/system-data/var/lib/cloud/seed/nocloud-net/user-data
#cloud-config
password: ubuntu
chpasswd: { expire: False }
ssh_pwauth: True
ssh_genkeytypes: ['rsa', 'dsa', 'ecdsa', 'ed25519']
EOF
        # FIXME: remove ubuntu user from the ubuntu-core/livecd-rootfs
        rm -f /mnt/system-data/var/lib/extrausers/passwd
        rm -f /mnt/system-data/var/lib/extrausers/shadow
        
        # done customizing stuff
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

    echo "Ensure fundamental snaps are still present"
    for name in pc pc-kernel ubuntu-core; do
        if ! snap list | grep $name; then
            echo "Not all fundamental snaps are available, all-snap image not valid"
            echo "Currently installed snaps"
            snap list
            exit 1
        fi
    done

    echo "Kernel has a store revision"
    snap list|grep ^pc-kernel|grep -E " [0-9]+\s+canonical"
}

