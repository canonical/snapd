#!/bin/bash

set -eux

prepare_classic() {
    apt install -y ${SPREAD_PATH}/../snapd_*.deb
    # Snapshot the state including core.
    if [ ! -f ${SPREAD_PATH}/snapd-state.tar.gz ]; then
        ! snap list | grep core || exit 1
        snap install test-snapd-tools
        snap list | grep core
        snap remove test-snapd-tools
        
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

prepare_all_snap() {
    if [ $SPREAD_REBOOT = 0 ] || [ -e /var/lib/dpkg/status ]; then
        # install the stuff we need
        apt install -y kpartx busybox-static
        apt install -y ${SPREAD_PATH}/../snapd_*.deb
        
        snap install --edge ubuntu-core
        snap install --edge --devmode ubuntu-device-flash

        # needs to be under /home because ubuntu-device-flash
        # uses snap-confine and that will hide parts of the hostfs
        IMAGE_HOME=/home/image
        mkdir -p $IMAGE_HOME

        # modify the core snap so that the current root-pw works there
        # for spread to do the first login
        UNPACKD="/tmp/ubuntu-core-snap"
        unsquashfs -d $UNPACKD /var/lib/snapd/snaps/ubuntu-core_*.snap
        
        # set root pw by concating root line from host and rest from core
        want_pw="$(grep ^root /etc/shadow)"
        echo "$want_pw" > /tmp/new-shadow
        tail -n +2 /etc/shadow >> /tmp/new-shadow
        cp -v /tmp/new-shadow $UNPACKD/etc/shadow
        
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

        # FIXME: how to test store updated of ubuntu-core with that?
        
        # create new image with the modified ubuntu-core snap
        IMAGE=all-snap-amd64.img
        /snap/bin/ubuntu-device-flash core 16 --channel edge --gadget pc  --kernel pc-kernel --os $IMAGE_HOME/ubuntu-core_*.snap --install snapweb  --output $IMAGE_HOME/$IMAGE
        
        # mount fresh image and add all our SPREAD_PROJECT data
        kpartx -avs $IMAGE_HOME/$IMAGE
        # FIXME: hardcoded mapper location, parse from kpartx
        mount /dev/mapper/loop2p3 /mnt
        mkdir -p /mnt/user-data/
        cp -avr /home/gopath /mnt/user-data/

        # create test user home dir
        mkdir -p /mnt/user-data/test
        chown 1001:1001 /mnt/user-data/test
        
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
    
        umount /mnt
        kpartx -d  $IMAGE_HOME/$IMAGE
    
        # the reflash magic
        # FIXME: ideally in initrd, but this is good enough for now
        cat > $IMAGE_HOME/reflash.sh << EOF
#!/bin/sh -ex
mount -t tmpfs none /tmp
cp /bin/busybox /tmp
cp $IMAGE_HOME/$IMAGE /tmp
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

        # Reboot !
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

    # Snapshot the system
    if [ ! -f $SPREAD_PATH/snapd-state.tar.gz ]; then
        systemctl stop snapd.socket
        tar czf $SPREAD_PATH/snapd-state.tar.gz /var/lib/snapd
        systemctl start snapd.socket
    fi
}

