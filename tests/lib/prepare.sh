#!/bin/bash

set -eux

# shellcheck source=tests/lib/dirs.sh
. "$TESTSLIB/dirs.sh"
# shellcheck source=tests/lib/snaps.sh
. "$TESTSLIB/snaps.sh"
# shellcheck source=tests/lib/pkgdb.sh
. "$TESTSLIB/pkgdb.sh"
# shellcheck source=tests/lib/boot.sh
. "$TESTSLIB/boot.sh"
# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"
# shellcheck source=tests/lib/systems.sh
. "$TESTSLIB/systems.sh"


disable_kernel_rate_limiting() {
    # kernel rate limiting hinders debugging security policy so turn it off
    echo "Turning off kernel rate-limiting"
    # TODO: we should be able to run the tests with rate limiting disabled so
    # debug output is robust, but we currently can't :(
    echo "SKIPPED: see https://forum.snapcraft.io/t/snapd-spread-tests-should-be-able-to-run-with-kernel-rate-limiting-disabled/424"
    #sysctl -w kernel.printk_ratelimit=0
}

disable_journald_rate_limiting() {
    # Disable journald rate limiting
    mkdir -p /etc/systemd/journald.conf.d
    # The RateLimitIntervalSec key is not supported on some systemd versions causing
    # the journal rate limit could be considered as not valid and discarded in concecuence.
    # RateLimitInterval key is supported in old systemd versions and in new ones as well,
    # maintaining backward compatibility.
    cat <<-EOF > /etc/systemd/journald.conf.d/no-rate-limit.conf
    [Journal]
    RateLimitInterval=0
    RateLimitBurst=0
EOF
    systemctl restart systemd-journald.service
}

disable_journald_start_limiting() {
    # Disable journald start limiting
    mkdir -p /etc/systemd/system/systemd-journald.service.d
    cat <<-EOF > /etc/systemd/system/systemd-journald.service.d/no-start-limit.conf
    [Unit]
    StartLimitBurst=0
EOF
    systemctl daemon-reload
}

ensure_jq() {
    if command -v jq; then
        return
    fi

    if is_core18_system; then
        snap install --devmode jq-core18
        snap alias jq-core18.jq jq
    elif is_core20_system; then
        snap install --devmode --edge jq-core20
        snap alias jq-core20.jq jq
    else
        snap install --devmode jq
    fi
}

disable_refreshes() {
    echo "Ensure jq is available"
    ensure_jq

    echo "Modify state to make it look like the last refresh just happened"
    systemctl stop snapd.socket snapd.service
    jq ".data[\"last-refresh\"] = \"$(date +%Y-%m-%dT%H:%M:%S%:z)\"" /var/lib/snapd/state.json > /var/lib/snapd/state.json.new
    mv /var/lib/snapd/state.json.new /var/lib/snapd/state.json
    systemctl start snapd.socket snapd.service

    echo "Minimize risk of hitting refresh schedule"
    snap set core refresh.schedule=00:00-23:59
    snap refresh --time --abs-time | MATCH "last: 2[0-9]{3}"

    echo "Ensure jq is gone"
    snap remove --purge jq jq-core18 jq-core20
}

setup_systemd_snapd_overrides() {
    START_LIMIT_INTERVAL="StartLimitInterval=0"
    mkdir -p /etc/systemd/system/snapd.service.d
    cat <<EOF > /etc/systemd/system/snapd.service.d/local.conf
[Unit]
$START_LIMIT_INTERVAL
[Service]
Environment=SNAPD_DEBUG_HTTP=7 SNAPD_DEBUG=1 SNAPPY_TESTING=1 SNAPD_REBOOT_DELAY=10m SNAPD_CONFIGURE_HOOK_TIMEOUT=30s SNAPPY_USE_STAGING_STORE=$SNAPPY_USE_STAGING_STORE
ExecStartPre=/bin/touch /dev/iio:device0
EOF
    mkdir -p /etc/systemd/system/snapd.socket.d
    cat <<EOF > /etc/systemd/system/snapd.socket.d/local.conf
[Unit]
$START_LIMIT_INTERVAL
EOF

    # We change the service configuration so reload and restart
    # the units to get them applied
    systemctl daemon-reload
    # stop the socket (it pulls down the service)
    systemctl stop snapd.socket
    # start the service (it pulls up the socket)
    systemctl start snapd.service
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
    core="$(readlink -f "$SNAP_MOUNT_DIR/core/current" || readlink -f "$SNAP_MOUNT_DIR/ubuntu-core/current")"
    snap="$(mount | grep " $core" | awk '{print $1}')"
    umount --verbose "$core"

    # Now unpack the core, inject the new snap-exec/snapctl into it
    unsquashfs -no-progress "$snap"
    # clean the old snapd binaries, just in case
    rm squashfs-root/usr/lib/snapd/* squashfs-root/usr/bin/snap
    # and copy in the current libexec
    cp -a "$LIBEXECDIR"/snapd/* squashfs-root/usr/lib/snapd/
    # also the binaries themselves
    cp -a /usr/bin/snap squashfs-root/usr/bin/
    # make sure bin/snapctl is a symlink to lib/
    if [ ! -L squashfs-root/usr/bin/snapctl ]; then
        rm -f squashfs-root/usr/bin/snapctl
        ln -s ../lib/snapd/snapctl squashfs-root/usr/bin/snapctl
    fi

    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            # and snap-confine's apparmor
            if [ -e /etc/apparmor.d/usr.lib.snapd.snap-confine.real ]; then
                cp -a /etc/apparmor.d/usr.lib.snapd.snap-confine.real squashfs-root/etc/apparmor.d/usr.lib.snapd.snap-confine.real
            else
                cp -a /etc/apparmor.d/usr.lib.snapd.snap-confine      squashfs-root/etc/apparmor.d/usr.lib.snapd.snap-confine.real
            fi
            ;;
    esac

    case "$SPREAD_SYSTEM" in
        ubuntu-*)
            # also load snap-confine's apparmor profile
            apparmor_parser -r squashfs-root/etc/apparmor.d/usr.lib.snapd.snap-confine.real
            ;;
    esac

    case "$SPREAD_SYSTEM" in
        fedora-*|centos-*|amazon-*)
            if selinuxenabled ; then
                # On these systems just unpacking core snap to $HOME will
                # automatically apply user_home_t label on all the contents of the
                # snap; since we cannot drop xattrs when calling mksquashfs, make
                # sure that we relabel the contents in way that a squashfs image
                # without any labels would look like: system_u:object_r:unlabeled_t
                chcon -R -u system_u -r object_r -t unlabeled_t squashfs-root
            fi
            ;;
    esac

    # Debian packages don't carry permissions correctly and we use post-inst
    # hooks to fix that on classic systems. Here, as a special case, fix the
    # void directory.
    chmod 111 squashfs-root/var/lib/snapd/void

    # repack, cheating to speed things up (4sec vs 1.5min)
    mv "$snap" "${snap}.orig"
    mksnap_fast "squashfs-root" "$snap"
    chmod --reference="${snap}.orig" "$snap"
    rm -rf squashfs-root

    # Now mount the new core snap, first discarding the old mount namespace
    "$LIBEXECDIR/snapd/snap-discard-ns" core
    mount "$snap" "$core"

    check_file() {
        if ! cmp "$1" "$2" ; then
            echo "$1 in tree and $2 in core snap are unexpectedly not the same"
            exit 1
        fi
    }

    # Make sure we're running with the correct copied bits
    for p in "$LIBEXECDIR/snapd/snap-exec" "$LIBEXECDIR/snapd/snap-confine" "$LIBEXECDIR/snapd/snap-discard-ns" "$LIBEXECDIR/snapd/snapd" "$LIBEXECDIR/snapd/snap-update-ns"; do
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
    # the re-exec setting may have changed in the service so we need
    # to ensure snapd is reloaded
    systemctl daemon-reload
    systemctl restart snapd

    if [ ! -f /etc/systemd/system/snapd.service.d/local.conf ]; then
        echo "/etc/systemd/system/snapd.service.d/local.conf vanished!"
        exit 1
    fi
}

prepare_classic() {
    # Skip building snapd when REUSE_SNAPD is set to 1
    if [ "$REUSE_SNAPD" != 1 ]; then
        distro_install_build_snapd
    fi

    if snap --version |MATCH unknown; then
        echo "Package build incorrect, 'snap --version' mentions 'unknown'"
        snap --version
        distro_query_package_info snapd
        exit 1
    fi
    if "$LIBEXECDIR/snapd/snap-confine" --version | MATCH unknown; then
        echo "Package build incorrect, 'snap-confine --version' mentions 'unknown'"
        "$LIBEXECDIR/snapd/snap-confine" --version
        case "$SPREAD_SYSTEM" in
            ubuntu-*|debian-*)
                apt-cache policy snapd
                ;;
            fedora-*)
                dnf info snapd
                ;;
        esac
        exit 1
    fi

    # Some systems (google:ubuntu-16.04-64) ship with a broken sshguard
    # unit. Stop the broken unit to not confuse the "degraded-boot" test.
    #
    # Some other (debian-sid) fail in fwupd-refresh.service
    #
    # FIXME: fix the ubuntu-16.04-64 image
    # FIXME2: fix the debian-sid-64 image
    for svc in fwupd-refresh.service sshguard.service; do
        if systemctl list-unit-files | grep "$svc"; then
            if systemctl is-failed "$svc"; then
                systemctl stop "$svc"
	        systemctl reset-failed "$svc"
            fi
        fi
    done

    setup_systemd_snapd_overrides

    if [ "$REMOTE_STORE" = staging ]; then
        # shellcheck source=tests/lib/store.sh
        . "$TESTSLIB/store.sh"
        # reset seeding data that is likely tainted with production keys
        systemctl stop snapd.service snapd.socket
        rm -rf /var/lib/snapd/assertions/*
        rm -f /var/lib/snapd/state.json
        setup_staging_store
    fi

    # Snapshot the state including core.
    if ! is_snapd_state_saved; then
        # need to be seeded to proceed with snap install
        # also make sure the captured state is seeded
        snap wait system seed.loaded

        # Cache snaps
        # shellcheck disable=SC2086
        cache_snaps core ${PRE_CACHE_SNAPS}

        echo "Cache the snaps profiler snap"
        if [ "$PROFILE_SNAPS" = 1 ]; then
            cache_snaps test-snapd-profiler
        fi

        snap list | not grep core || exit 1
        # use parameterized core channel (defaults to edge) instead
        # of a fixed one and close to stable in order to detect defects
        # earlier
        snap install --"$CORE_CHANNEL" core
        snap list | grep core

        systemctl stop snapd.{service,socket}
        update_core_snap_for_classic_reexec
        systemctl start snapd.{service,socket}

        disable_refreshes

        echo "Ensure that the bootloader environment output does not contain any of the snap_* variables on classic"
        # shellcheck disable=SC2119
        output=$(bootenv)
        if echo "$output" | MATCH snap_ ; then
            echo "Expected bootloader environment without snap_*, got:"
            echo "$output"
            exit 1
        fi

        systemctl stop snapd.{service,socket}
        save_snapd_state
        systemctl start snapd.socket
    fi

    disable_kernel_rate_limiting

    if [[ "$SPREAD_SYSTEM" == arch-* ]]; then
        # Arch packages do not ship empty directories by default, hence there is
        # no /etc/dbus-1/system.d what prevents dbus from properly establishing
        # inotify watch on that path
        mkdir -p /etc/dbus-1/system.d
        systemctl reload dbus.service
    fi
}

repack_snapd_snap_with_deb_content() {
    local TARGET="$1"

    local UNPACK_DIR="/tmp/snapd-unpack"
    unsquashfs -no-progress -d "$UNPACK_DIR" snapd_*.snap
    # clean snap apparmor.d to ensure we put the right snap-confine apparmor
    # file in place. Its called usr.lib.snapd.snap-confine on 14.04 but
    # usr.lib.snapd.snap-confine.real everywhere else
    rm -f "$UNPACK_DIR"/etc/apparmor.d/*

    dpkg-deb -x "$SPREAD_PATH"/../snapd_*.deb "$UNPACK_DIR"
    cp /usr/lib/snapd/info "$UNPACK_DIR"/usr/lib/
    snap pack "$UNPACK_DIR" "$TARGET"
    rm -rf "$UNPACK_DIR"
}

repack_snapd_snap_with_deb_content_and_run_mode_firstboot_tweaks() {
    local TARGET="$1"

    local UNPACK_DIR="/tmp/snapd-unpack"
    unsquashfs -no-progress -d "$UNPACK_DIR" snapd_*.snap

    # clean snap apparmor.d to ensure we put the right snap-confine apparmor
    # file in place. Its called usr.lib.snapd.snap-confine on 14.04 but
    # usr.lib.snapd.snap-confine.real everywhere else
    rm -f "$UNPACK_DIR"/etc/apparmor.d/*

    dpkg-deb -x "$SPREAD_PATH"/../snapd_*.deb "$UNPACK_DIR"
    cp /usr/lib/snapd/info "$UNPACK_DIR"/usr/lib/

    # now install a unit that setups enough so that we can connect
    cat > "$UNPACK_DIR"/lib/systemd/system/snapd.spread-tests-run-mode-tweaks.service <<'EOF'
[Unit]
Description=Tweaks to run mode for spread tests
Before=snapd.service
Documentation=man:snap(1)

[Service]
Type=oneshot
ExecStart=/usr/lib/snapd/snapd.spread-tests-run-mode-tweaks.sh
RemainAfterExit=true

[Install]
WantedBy=multi-user.target
EOF
    # XXX: this duplicates a lot of setup_test_user_by_modify_writable()
    cat > "$UNPACK_DIR"/usr/lib/snapd/snapd.spread-tests-run-mode-tweaks.sh <<'EOF'
#!/bin/sh
set -e
# ensure we don't enable ssh in install mode or spread will get confused
if ! grep 'snapd_recovery_mode=run' /proc/cmdline; then
    echo "not in run mode - script not running"
    exit 0
fi
if [ -e /root/spread-setup-done ]; then
    exit 0
fi

# extract data from previous stage
(cd / && tar xvf /run/mnt/ubuntu-seed/run-mode-overlay-data.tar.gz)

# user db - it's complicated
for f in group gshadow passwd shadow; do
    # now bind mount read-only those passwd files on boot
    cat >/etc/systemd/system/etc-"$f".mount <<EOF2
[Unit]
Description=Mount root/test-etc/$f over system etc/$f
Before=ssh.service

[Mount]
What=/root/test-etc/$f
Where=/etc/$f
Type=none
Options=bind,ro

[Install]
WantedBy=multi-user.target
EOF2
    systemctl enable etc-"$f".mount
    systemctl start etc-"$f".mount
done

mkdir -p /home/test
chown 12345:12345 /home/test
mkdir -p /home/ubuntu
chown 1000:1000 /home/ubuntu
mkdir -p /etc/sudoers.d/
echo 'test ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers.d/99-test-user
echo 'ubuntu ALL=(ALL) NOPASSWD:ALL' >> /etc/sudoers.d/99-ubuntu-user
sed -i 's/\#\?\(PermitRootLogin\|PasswordAuthentication\)\>.*/\1 yes/' /etc/ssh/sshd_config
echo "MaxAuthTries 120" >> /etc/ssh/sshd_config
grep '^PermitRootLogin yes' /etc/ssh/sshd_config
systemctl reload ssh

touch /root/spread-setup-done
EOF
    chmod 0755 "$UNPACK_DIR"/usr/lib/snapd/snapd.spread-tests-run-mode-tweaks.sh
    snap pack "$UNPACK_DIR" "$TARGET"
    rm -rf "$UNPACK_DIR"
}

setup_core_for_testing_by_modify_writable() {
    UNPACK_DIR="$1"

    # create test user and ubuntu user inside the writable partition
    # so that we can use a stock core in tests
    mkdir -p /mnt/user-data/test

    # create test user, see the comment in spread.yaml about 12345
    mkdir -p /mnt/system-data/etc/sudoers.d/
    echo 'test ALL=(ALL) NOPASSWD:ALL' >> /mnt/system-data/etc/sudoers.d/99-test-user
    echo 'ubuntu ALL=(ALL) NOPASSWD:ALL' >> /mnt/system-data/etc/sudoers.d/99-ubuntu-user
    # modify sshd so that we can connect as root
    mkdir -p /mnt/system-data/etc/ssh
    cp -a "$UNPACK_DIR"/etc/ssh/* /mnt/system-data/etc/ssh/
    # core18 is different here than core16
    sed -i 's/\#\?\(PermitRootLogin\|PasswordAuthentication\)\>.*/\1 yes/' /mnt/system-data/etc/ssh/sshd_config
    # ensure the setting is correct
    grep '^PermitRootLogin yes' /mnt/system-data/etc/ssh/sshd_config

    # build the user database - this is complicated because:
    # - spread on linode wants to login as "root"
    # - "root" login on the stock core snap is disabled
    # - uids between classic/core differ
    # - passwd,shadow on core are read-only
    # - we cannot add root to extrausers as system passwd is searched first
    # - we need to add our ubuntu and test users too
    # So we create the user db we need in /root/test-etc/*:
    # - take core passwd without "root"
    # - append root
    # - make sure the group matches
    # - bind mount /root/test-etc/* to /etc/* via custom systemd job
    # We also create /var/lib/extrausers/* and append ubuntu,test there
    test ! -e /mnt/system-data/root
    mkdir -m 700 /mnt/system-data/root
    test -d /mnt/system-data/root
    mkdir -p /mnt/system-data/root/test-etc
    mkdir -p /mnt/system-data/var/lib/extrausers/
    touch /mnt/system-data/var/lib/extrausers/sub{uid,gid}
    mkdir -p /mnt/system-data/etc/systemd/system/multi-user.target.wants
    for f in group gshadow passwd shadow; do
        # the passwd from core without root
        grep -v "^root:" "$UNPACK_DIR/etc/$f" > /mnt/system-data/root/test-etc/"$f"
        # append this systems root user so that linode can connect
        grep "^root:" /etc/"$f" >> /mnt/system-data/root/test-etc/"$f"

        # make sure the group is as expected
        chgrp --reference "$UNPACK_DIR/etc/$f" /mnt/system-data/root/test-etc/"$f"
        # now bind mount read-only those passwd files on boot
        cat >/mnt/system-data/etc/systemd/system/etc-"$f".mount <<EOF
[Unit]
Description=Mount root/test-etc/$f over system etc/$f
Before=ssh.service

[Mount]
What=/root/test-etc/$f
Where=/etc/$f
Type=none
Options=bind,ro

[Install]
WantedBy=multi-user.target
EOF
        ln -s /etc/systemd/system/etc-"$f".mount /mnt/system-data/etc/systemd/system/multi-user.target.wants/etc-"$f".mount

        # create /var/lib/extrausers/$f
        # append ubuntu, test user for the testing
        grep "^test:" /etc/$f >> /mnt/system-data/var/lib/extrausers/"$f"
        grep "^ubuntu:" /etc/$f >> /mnt/system-data/var/lib/extrausers/"$f"
        # check test was copied
        MATCH "^test:" </mnt/system-data/var/lib/extrausers/"$f"
        MATCH "^ubuntu:" </mnt/system-data/var/lib/extrausers/"$f"
    done

    # ensure spread -reuse works in the core image as well
    if [ -e /.spread.yaml ]; then
        cp -av /.spread.yaml /mnt/system-data
    fi

    # using symbolic names requires test:test have the same ids
    # inside and outside which is a pain (see 12345 above), but
    # using the ids directly is the wrong kind of fragile
    chown --verbose test:test /mnt/user-data/test

    # we do what sync-dirs is normally doing on boot, but because
    # we have subdirs/files in /etc/systemd/system (created below)
    # the writeable-path sync-boot won't work
    mkdir -p /mnt/system-data/etc/systemd

    # we do not need console-conf, so prevent it from running
    mkdir -p /mnt/system-data/var/lib/console-conf
    touch /mnt/system-data/var/lib/console-conf/complete

    (cd /tmp ; unsquashfs -no-progress -v  /var/lib/snapd/snaps/"$core_name"_*.snap etc/systemd/system)
    cp -avr /tmp/squashfs-root/etc/systemd/system /mnt/system-data/etc/systemd/
    umount /mnt
    kpartx -d  "$IMAGE_HOME/$IMAGE"
}

setup_reflash_magic() {
    # install the stuff we need
    distro_install_package kpartx busybox-static
    distro_install_local_package "$GOHOME"/snapd_*.deb
    distro_clean_package_cache

    # need to be seeded to proceed with snap install
    snap wait system seed.loaded

    # we cannot use "names.sh" here because no snaps are installed yet
    core_name="core"
    if is_core18_system; then
        snap download "--channel=${SNAPD_CHANNEL}" snapd
        core_name="core18"
    elif is_core20_system; then
        snap download "--channel=${SNAPD_CHANNEL}" snapd
        core_name="core20"
    fi
    snap install "--channel=${CORE_CHANNEL}" "$core_name"

    # install ubuntu-image
    snap install --classic --edge ubuntu-image

    # needs to be under /home because ubuntu-device-flash
    # uses snap-confine and that will hide parts of the hostfs
    IMAGE_HOME=/home/image
    mkdir -p "$IMAGE_HOME"

    # ensure that ubuntu-image is using our test-build of snapd with the
    # test keys and not the bundled version of usr/bin/snap from the snap.
    # Note that we can not put it into /usr/bin as '/usr' is different
    # when the snap uses confinement.
    cp /usr/bin/snap "$IMAGE_HOME"
    export UBUNTU_IMAGE_SNAP_CMD="$IMAGE_HOME/snap"

    if is_core18_system; then
        repack_snapd_snap_with_deb_content "$IMAGE_HOME"
        # FIXME: fetch directly once its in the assertion service
        cp "$TESTSLIB/assertions/ubuntu-core-18-amd64.model" "$IMAGE_HOME/pc.model"
        IMAGE=core18-amd64.img
    elif is_core20_system; then
        repack_snapd_snap_with_deb_content_and_run_mode_firstboot_tweaks "$IMAGE_HOME"
        # TODO:UC20: use canonical model instead of "mvo" one
        cp "$TESTSLIB/assertions/ubuntu-core-20-amd64.model" "$IMAGE_HOME/pc.model"
        IMAGE=core20-amd64.img
    else
        # modify the core snap so that the current root-pw works there
        # for spread to do the first login
        UNPACK_DIR="/tmp/core-snap"
        unsquashfs -no-progress -d "$UNPACK_DIR" /var/lib/snapd/snaps/core_*.snap

        # FIXME: install would be better but we don't have dpkg on
        #        the image
        # unpack our freshly build snapd into the new snapd snap
        dpkg-deb -x "$SPREAD_PATH"/../snapd_*.deb "$UNPACK_DIR"
        # Debian packages don't carry permissions correctly and we use
        # post-inst hooks to fix that on classic systems. Here, as a special
        # case, fix the void directory we just unpacked.
        chmod 111 "$UNPACK_DIR/var/lib/snapd/void"
        # ensure any new timer units are available
        cp -a /etc/systemd/system/timers.target.wants/*.timer "$UNPACK_DIR/etc/systemd/system/timers.target.wants"

        # add gpio and iio slots
        cat >> "$UNPACK_DIR/meta/snap.yaml" <<-EOF
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
        snap pack "$UNPACK_DIR" "$IMAGE_HOME"

        # FIXME: fetch directly once its in the assertion service
        cp "$TESTSLIB/assertions/pc-${REMOTE_STORE}.model" "$IMAGE_HOME/pc.model"

        # FIXME: how to test store updated of ubuntu-core with sideloaded snap?
        IMAGE=all-snap-amd64.img
    fi

    EXTRA_FUNDAMENTAL=
    IMAGE_CHANNEL=edge
    if [ "$KERNEL_CHANNEL" = "$GADGET_CHANNEL" ]; then
        IMAGE_CHANNEL="$KERNEL_CHANNEL"
    else
        # download pc-kernel snap for the specified channel and set
        # ubuntu-image channel to that of the gadget, so that we don't
        # need to download it
        snap download --channel="$KERNEL_CHANNEL" pc-kernel

        EXTRA_FUNDAMENTAL="--extra-snaps $PWD/pc-kernel_*.snap"
        IMAGE_CHANNEL="$GADGET_CHANNEL"
    fi

    # 'snap pack' creates snaps 0644, and ubuntu-image just copies those in
    # maybe we should fix one or both of those, but for now this'll do
    chmod 0600 "$IMAGE_HOME"/*.snap

    # on core18 we need to use the modified snapd snap and on core16
    # it is the modified core that contains our freshly build snapd
    if is_core18_system || is_core20_system; then
        extra_snap=("$IMAGE_HOME"/snapd_*.snap)
    else
        extra_snap=("$IMAGE_HOME"/core_*.snap)
    fi

    # extra_snap should contain only ONE snap
    if [ "${#extra_snap[@]}" -ne 1 ]; then
        echo "unexpected number of globbed snaps: ${extra_snap[*]}"
        exit 1
    fi

    /snap/bin/ubuntu-image -w "$IMAGE_HOME" "$IMAGE_HOME/pc.model" \
                           --channel "$IMAGE_CHANNEL" \
                           "$EXTRA_FUNDAMENTAL" \
                           --extra-snaps "${extra_snap[0]}" \
                           --output "$IMAGE_HOME/$IMAGE"
    rm -f ./pc-kernel_*.{snap,assert} ./pc_*.{snap,assert} ./snapd_*.{snap,assert}

    # mount fresh image and add all our SPREAD_PROJECT data
    kpartx -avs "$IMAGE_HOME/$IMAGE"
    # FIXME: hardcoded mapper location, parse from kpartx
    if is_core18_system; then
        mount /dev/mapper/loop3p3 /mnt
    elif is_core20_system; then
        # (ab)use ubuntu-seed
        mount /dev/mapper/loop3p2 /mnt
    else
        mount /dev/mapper/loop2p3 /mnt
    fi
    mkdir -p /mnt/user-data/
    # copy over everything from gopath to user-data, exclude:
    # - VCS files
    # - built debs
    # - golang archive files and built packages dir
    # - govendor .cache directory and the binary,
    if is_core16_system || is_core18_system; then
        rsync -a -C \
          --exclude '*.a' \
          --exclude '*.deb' \
          --exclude /gopath/.cache/ \
          --exclude /gopath/bin/govendor \
          --exclude /gopath/pkg/ \
          /home/gopath /mnt/user-data/
    elif is_core20_system; then
        # prepare passwd for run-mode-overlay-data
        mkdir -p /root/test-etc
        mkdir -p /var/lib/extrausers
        touch /var/lib/extrausers/sub{uid,gid}
        for f in group gshadow passwd shadow; do
            grep -v "^root:" /etc/"$f" > /root/test-etc/"$f"
            grep "^root:" /etc/"$f" >> /root/test-etc/"$f"
            chgrp --reference /etc/"$f" /root/test-etc/"$f"
            # create /var/lib/extrausers/$f
            # append ubuntu, test user for the testing
            grep "^test:" /etc/"$f" >> /var/lib/extrausers/"$f"
            grep "^ubuntu:" /etc/"$f" >> /var/lib/extrausers/"$f"
            # check test was copied
            MATCH "^test:" </var/lib/extrausers/"$f"
            MATCH "^ubuntu:" </var/lib/extrausers/"$f"
        done
        tar -c -z \
          --exclude '*.a' \
          --exclude '*.deb' \
          --exclude /gopath/.cache/ \
          --exclude /gopath/bin/govendor \
          --exclude /gopath/pkg/ \
          -f /mnt/run-mode-overlay-data.tar.gz \
          /home/gopath /root/test-etc /var/lib/extrausers
    fi

    # now modify the image writable partition
    if is_core18_system; then
        UNPACK_DIR="/tmp/core18-snap"
        unsquashfs -no-progress -d "$UNPACK_DIR" /var/lib/snapd/snaps/core18_*.snap
    fi
    # modifying "writable" is only possible on uc16/uc18
    if is_core16_system || is_core18_system; then
        # modify the writable partition of "core" so that we have the
        # test user
        setup_core_for_testing_by_modify_writable "$UNPACK_DIR"
    fi

    # the reflash magic
    # FIXME: ideally in initrd, but this is good enough for now
    cat > "$IMAGE_HOME/reflash.sh" << EOF
#!/bin/sh -ex
mount -t tmpfs none /tmp
cp /bin/busybox /tmp
cp $IMAGE_HOME/$IMAGE /tmp
sync
# blow away everything
OF=/dev/sda
if [ -e /dev/vda ]; then
    OF=/dev/vda
fi
/tmp/busybox dd if=/tmp/$IMAGE of=\$OF bs=4M
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

# prepare_ubuntu_core will prepare ubuntu-core 16 and 18
prepare_ubuntu_core() {
    # we are still a "classic" image, prepare the surgery
    if [ -e /var/lib/dpkg/status ]; then
        setup_reflash_magic
        REBOOT
    fi

    disable_journald_rate_limiting
    disable_journald_start_limiting

    # verify after the first reboot that we are now in core18 world
    if [ "$SPREAD_REBOOT" = 1 ]; then
        echo "Ensure we are now in an all-snap world"
        if [ -e /var/lib/dpkg/status ]; then
            echo "Rebooting into all-snap system did not work"
            exit 1
        fi
    fi

    # Wait for the snap command to become available.
    if [ "$SPREAD_BACKEND" != "external" ]; then
        for i in $(seq 120); do
            if [ "$(command -v snap)" = "/usr/bin/snap" ] && snap version | grep -q 'snapd +1337.*'; then
                break
            fi
            sleep 1
        done
    fi

    # Wait for seeding to finish.
    snap wait system seed.loaded

    echo "Ensure fundamental snaps are still present"
    # shellcheck source=tests/lib/names.sh
    . "$TESTSLIB/names.sh"
    for name in "$gadget_name" "$kernel_name" "$core_name"; do
        if ! snap list "$name"; then
            echo "Not all fundamental snaps are available, all-snap image not valid"
            echo "Currently installed snaps"
            snap list
            exit 1
        fi
    done

    echo "Ensure the snapd snap is available"
    if is_core18_system; then
        if ! snap list snapd; then
            echo "snapd snap on core18 is missing"
            snap list
            exit 1
        fi
    fi

    echo "Ensure rsync is available"
    if ! command -v rsync; then
        rsync_snap="test-snapd-rsync"
        if is_core18_system; then
            rsync_snap="test-snapd-rsync-core18"
        elif is_core20_system; then
            rsync_snap="test-snapd-rsync-core20"
        fi
        snap install --devmode "$rsync_snap"
        snap alias "$rsync_snap".rsync rsync
    fi

    # Cache snaps
    # shellcheck disable=SC2086
    cache_snaps ${PRE_CACHE_SNAPS}

    echo "Ensure the core snap is cached"
    # Cache snaps
    if is_core18_system; then
        if snap list core >& /dev/null; then
            echo "core snap on core18 should not be installed yet"
            snap list
            exit 1
        fi
        cache_snaps core test-snapd-sh-core18
    fi

    echo "Cache the snaps profiler snap"
    if [ "$PROFILE_SNAPS" = 1 ]; then
        if is_core18_system; then
            cache_snaps test-snapd-profiler-core18
        else
            cache_snaps test-snapd-profiler
        fi
    fi

    disable_refreshes
    setup_systemd_snapd_overrides

    # Snapshot the fresh state (including boot/bootenv)
    if ! is_snapd_state_saved; then
        systemctl stop snapd.service snapd.socket
        save_snapd_state
        systemctl start snapd.socket
    fi

    disable_kernel_rate_limiting
}

cache_snaps(){
    # Pre-cache snaps so that they can be installed by tests quickly.
    # This relies on a behavior of snapd which snaps installed are
    # cached and then used when need to the installed again

    # Download each of the snaps we want to pre-cache. Note that `snap download`
    # a quick no-op if the file is complete.
    for snap_name in "$@"; do
        snap download "$snap_name"

        # Copy all of the snaps back to the spool directory. From there we
        # will reuse them during subsequent `snap install` operations.
        snap_file=$(ls "${snap_name}"_*.snap)
        mv "${snap_file}" /var/lib/snapd/snaps/"${snap_file}".partial
        rm -f "${snap_name}"_*.assert
    done
}
