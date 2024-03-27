#!/bin/bash -ex

transition_to_recover_mode(){
    local label=""
    local HAVE_LABEL=""
    if [ -n "${1:-}" ]; then
        label=$1
        HAVE_LABEL=1
    else
        HAVE_LABEL=0
    fi

    # TODO: the following mocking of systemctl should be combined with the code
    # in tests/core/snapd-refresh-vs-services-reboots into a generic shutdown
    # helper to get better observability and less race conditions around
    # snapd rebooting things from a live system under spread

    # save the original systemctl command since we essentially need to mock it
    cp /bin/systemctl /tmp/orig-systemctl

    # redirect shutdown command to our mock to observe calls and avoid racing
    # with spread
    mount -o bind "$TESTSLIB/mock-shutdown" /bin/systemctl

    # reboot to recovery mode
    echo "Request rebooting into recovery mode"
    if [ "$HAVE_LABEL" = 1 ]; then
        snap reboot --recover "$label" | MATCH 'Reboot into ".*" "recover" mode'
    else
        snap reboot --recover | MATCH 'Reboot into "recover" mode'
    fi

    # snapd schedules a slow timeout and an immediate one, however it is
    # scheduled asynchronously, try to keep the check simple
    # shellcheck disable=SC2016
    retry -n 30 --wait 1 sh -c 'test "$(wc -l < /tmp/mock-shutdown.calls)" = "2"'
    # a reboot in 10 minutes should have been scheduled
    MATCH -- '-r \+10' < /tmp/mock-shutdown.calls
    # and an immediate reboot should have been scheduled
    MATCH -- '-r \+0' < /tmp/mock-shutdown.calls

    # restore shutdown so that spread can reboot the host
    umount /bin/systemctl

    # with the external backend, we do not have the special snapd snap with
    # the first-boot run mode tweaks as created from $TESTLIB/prepare.sh's
    # repack_snapd_snap_with_deb_content_and_run_mode_firstboot_tweaks func
    # so instead to get the spread gopath and other data needed to continue
    # the test, we need to add a .ssh/rc script which copies all of the data
    # from /host/ubuntu-data in recover mode to the tmpfs /home, see
    # sshd(8) for details on the ~/.ssh/rc script
    if [ "$SPREAD_BACKEND" = "external" ] || [ "$SPREAD_BACKEND" = "testflinger" ]; then
        mkdir -p /home/external/.ssh
        chown external:external /home/external/.ssh
        touch /home/external/.ssh/rc
        chown external:external /home/external/.ssh/rc
        # See the "sshrc" section in sshd(8)
        cat <<-EOF > /home/external/.ssh/rc
#!/bin/sh
# added by spread tests
if grep -q 'snapd_recovery_mode=recover' /proc/cmdline; then
    # use as sudo for proper permissions, assumes that the external user is
    # a sudo user, which it must be by definition of it being the login user
    # that spread uses
    # silence the output so that nothing is output to a ssh command,
    # potentially confusing spread
    sudo mkdir /home/gopath > /dev/null
    sudo mount --bind /host/ubuntu-data/user-data/gopath /home/gopath > /dev/null
fi
EOF
    fi

    # XXX: is this a race between spread seeing REBOOT and machine rebooting?
    REBOOT
}

transition_to_run_mode() {
    local label=""
    local HAVE_LABEL=""
    if [ -n "${1:-}" ]; then
        label=$1
        HAVE_LABEL=1
    else
        HAVE_LABEL=0
    fi

    # see earlier note
    mount -o bind "$TESTSLIB/mock-shutdown" /usr/sbin/shutdown

    # request going back to run mode
    if [ "$HAVE_LABEL" = "1" ]; then
        snap reboot --run "$label" | MATCH 'Reboot into ".*" "run" mode.'
    else
        snap reboot --run | MATCH 'Reboot into "run" mode.'
    fi
    # XXX: is this a race between spread seeing REBOOT and machine rebooting?

    # see earlier note about shutdown
    # shellcheck disable=SC2016
    retry -n 30 --wait 1 sh -c 'test "$(wc -l < /tmp/mock-shutdown.calls)" = "2"'
    # a reboot in 10 minutes should have been scheduled
    MATCH -- '-r \+10' < /tmp/mock-shutdown.calls
    # and an immediate reboot should have been scheduled
    MATCH -- '-r \+0' < /tmp/mock-shutdown.calls

    umount /usr/sbin/shutdown

    # if we are using the external backend, remove the .ssh/rc hack we used
    # to copy data around, it's not necessary going back to run mode, and it
    # somehow interferes with spread re-connecting over ssh, causing
    # spread to timeout trying to reconnect after the reboot with an error
    # message like this:
    # ssh: unexpected packet in response to channel open: <nil>
    if [ "$SPREAD_BACKEND" = "external" ]; then
        # TODO:UC20: if/when /host is mounted ro, then we will need to
        # either remount it rw or do some other hack to fix this
        rm -f /host/ubuntu-data/user-data/external/.ssh/rc
    fi

    REBOOT
}

prepare_recover_mode() {
    # the system is seeding and snap command may not be available yet
    # shellcheck disable=SC2016
    retry -n 60 --wait 1 sh -c 'test "$(command -v snap)" = /usr/bin/snap'

    # wait till the system seeding is finished
    snap wait system seed.loaded

    # we're running in an ephemeral system and thus have to re-install snaps
    snap install --edge jq
    snap install test-snapd-curl --devmode --edge

    MATCH 'snapd_recovery_mode=recover' < /proc/cmdline
    # verify we are in recovery mode via the API
    test-snapd-curl.curl -s --unix-socket /run/snapd.socket http://localhost/v2/system-info > system-info
    jq -r '.result["system-mode"]' < system-info | MATCH 'recover'
}
