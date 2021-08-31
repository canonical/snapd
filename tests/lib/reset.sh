#!/bin/bash -x

# shellcheck source=tests/lib/state.sh
. "$TESTSLIB/state.sh"
# shellcheck source=tests/lib/systemd.sh
. "$TESTSLIB/systemd.sh"


reset_classic() {
    # Reload all service units as in some situations the unit might
    # have changed on the disk.
    systemctl daemon-reload
    systemd_stop_units snapd.service snapd.socket

    # none of the purge steps stop the user services, we need to do it
    # explicitly, at least for the root user
    systemctl --user stop snapd.session-agent.socket || true

    SNAP_MOUNT_DIR="$(os.paths snap-mount-dir)"
    case "$SPREAD_SYSTEM" in
        ubuntu-*|debian-*)
            sh -x "${SPREAD_PATH}/debian/snapd.prerm" remove
            sh -x "${SPREAD_PATH}/debian/snapd.postrm" purge
            ;;
        fedora-*|opensuse-*|arch-*|amazon-*|centos-*)
            # We don't know if snap-mgmt was built, so call the *.in file
            # directly and pass arguments that will override the placeholders
            sh -x "${SPREAD_PATH}/cmd/snap-mgmt/snap-mgmt.sh.in" \
                --snap-mount-dir="$SNAP_MOUNT_DIR" \
                --purge
            # The script above doesn't remove the snapd directory as this
            # is normally done by the rpm packaging system.
            rm -rf /var/lib/snapd
            ;;
        *)
            echo "don't know how to reset $SPREAD_SYSTEM"
            exit 1
            ;;
    esac

    # purge may have removed udev rules, retrigger device events
    udevadm trigger
    udevadm settle

    # purge has removed units, reload the state now
    systemctl daemon-reload

    # extra purge
    rm -rvf /var/snap "${SNAP_MOUNT_DIR:?}/bin"
    mkdir -p "$SNAP_MOUNT_DIR" /var/snap /var/lib/snapd
    if [ "$(find "$SNAP_MOUNT_DIR" /var/snap -mindepth 1 -print -quit)" ]; then
        echo "postinst purge failed"
        ls -lR "$SNAP_MOUNT_DIR"/ /var/snap/
        exit 1
    fi
    rm -rf /tmp/snap.*

    case "$SPREAD_SYSTEM" in
        fedora-*|centos-*)
            # On systems running SELinux we need to restore the context of the
            # directories we just recreated. Otherwise, the entries created
            # inside will be incorrectly labeled.
            restorecon -F -v -R "$SNAP_MOUNT_DIR" /var/snap /var/lib/snapd
            ;;
    esac

    # systemd retains the failed state of service units, even after they are
    # removed, we need to reset their 'failed state'
    systemctl --plain --failed --no-legend --full | awk '/^ *snap\..*\.service +(error|not-found) +failed/ {print $1}' | while read -r unit; do
        systemctl reset-failed "$unit" || true
    done

    if os.query is-trusty; then
        systemctl start snap.mount.service
    fi

    # Clean root home
    rm -rf /root/snap /root/.snap/gnupg /root/.{bash_history,local,cache,config}
    # Clean test home
    rm -rf /home/test/snap /home/test/.{bash_history,local,cache,config}
    # Clean /tmp
    rm -f /tmp/core* /tmp/ubuntu-core*

    if [ "$1" = "--reuse-core" ]; then
        # Restore snapd state and start systemd service units
        restore_snapd_state
        escaped_snap_mount_dir="$(systemd-escape --path "$SNAP_MOUNT_DIR")"
        mounts="$(systemctl list-unit-files --full | grep "^${escaped_snap_mount_dir}[-.].*\\.mount" | cut -f1 -d ' ')"
        services="$(systemctl list-unit-files --full | grep "^${escaped_snap_mount_dir}[-.].*\\.service" | cut -f1 -d ' ')"
        systemctl daemon-reload # Workaround for http://paste.ubuntu.com/17735820/
        for unit in $mounts $services; do
            systemctl start "$unit"
        done

        # force all profiles to be re-generated
        rm -f /var/lib/snapd/system-key

        # force snapd-session-agent.socket fto be re-generated
        rm -f /run/user/0/snapd-session-agent.socket
    fi

    # Make sure the systemd user wants directories exist
    mkdir -p /etc/systemd/user/sockets.target.wants /etc/systemd/user/timers.target.wants /etc/systemd/user/default.target.wants

    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.socket

        EXTRA_NC_ARGS="-q 1"
        case "$SPREAD_SYSTEM" in
            fedora-34-*|debian-10-*)
                # Param -q is not available on fedora 34
                EXTRA_NC_ARGS="-w 1"
                ;;
            fedora-*|amazon-*|centos-*)
                EXTRA_NC_ARGS=""
                ;;
        esac

        # wait for snapd listening
        retry -n 120 --wait 0.5 sh -c "printf 'GET / HTTP/1.0\r\n\r\n' | nc -U $EXTRA_NC_ARGS /run/snapd.socket"
    fi
}

reset_all_snap() {
    # remove all leftover snaps

    # make sure snapd is running before we attempt to remove snaps, in case a test stopped it
    if ! systemctl status snapd.service snapd.socket >/dev/null; then
        systemctl start snapd.service snapd.socket
    fi

    # shellcheck source=tests/lib/names.sh
    . "$TESTSLIB/names.sh"
    SNAP_MOUNT_DIR="$(os.paths snap-mount-dir)"
    remove_bases=""
    # remove all app snaps first
    for snap in "$SNAP_MOUNT_DIR"/*; do
        snap="${snap:6}"
        case "$snap" in
            "bin" | "$gadget_name" | "$kernel_name" | "$core_name" | "snapd" |README)
                ;;
            *)
                # Check if a snap should be kept, there's a list of those in spread.yaml.
                keep=0
                for precious_snap in $SKIP_REMOVE_SNAPS; do
                    if [ "$snap" = "$precious_snap" ]; then
                        keep=1
                        break
                    fi
                done
                if [ "$keep" -eq 0 ]; then
                    if snap info --verbose "$snap" | grep -E '^type: +(base|core)'; then
                        if [ -z "$remove_bases" ]; then
                            remove_bases="$snap"
                        else
                            remove_bases="$remove_bases $snap"
                        fi
                    else
                        snap remove --purge "$snap"
                    fi
                fi
                ;;
        esac
    done
    # remove all base/os snaps at the end
    if [ -n "$remove_bases" ]; then
        for base in $remove_bases; do
            snap remove --purge "$base"
            if [ -d "$SNAP_MOUNT_DIR/$base" ]; then
                echo "Error: removing base $base has unexpected leftover dir $SNAP_MOUNT_DIR/$base"
                ls -al "$SNAP_MOUNT_DIR"
                ls -al "$SNAP_MOUNT_DIR/$base"
                exit 1
            fi
        done
    fi

    # purge may have removed udev rules, retrigger device events
    udevadm trigger
    udevadm settle

    # ensure we have the same state as initially
    systemctl stop snapd.service snapd.socket
    restore_snapd_state
    rm -rf /root/.snap
    rm -rf /tmp/snap.*
    if [ "$1" != "--keep-stopped" ]; then
        systemctl start snapd.service snapd.socket
    fi

    # Exit in case there is a snap in broken state after restoring the snapd state
    if snap list | grep -E "broken$"; then
        echo "snap in broken state"
        exit 1
    fi

}

# Before resetting all snapd state, specifically remove all disabled snaps that
# are not from the store, since otherwise their revision number will remain
# mounted at /snap/<name>/x<rev>/ and if we execute multiple tests that use this
# same snap, the previous mount unit for x2 for example will stay around if we
# simply revert to x1 and then delete state.json, since x2 is still mounted if
# we then again install that snap again twice (i.e. to get to x2), the mount 
# unit will still be active and thus the previous iteration of this snap at 
# revision x2 will be used as this new revision's files for x2. This is 
# particularly damaging for the snapd snap when we are installing different 
# versions such as in the snapd-refresh-vs-services (and the -reboots variant) 
# test, since the bug manifests as us trying to refresh to a particular revision
# of snapd, but that revision is still mounted from the previous iteration of
# the test and thus gets the wrong version, as displayed in this output:
#
# + snap install --dangerous snapd_2.49.1.snap
# 2021-04-23T20:11:20Z INFO Waiting for automatic snapd restart...
# snapd 2.49.2 installed
#

snap list --all | grep disabled | while read -r name _ revision _ ; do
    snap remove "$name" --revision="$revision"
done


# When the variable REUSE_SNAPD is set to 1, we don't remove and purge snapd.
# In that case we just cleanup the environment by removing installed snaps as
# it is done for core systems.
if os.query is-core || [ "$REUSE_SNAPD" = 1 ]; then
    reset_all_snap "$@"
else
    reset_classic "$@"
fi

# Discard all mount namespaces and active mount profiles.
# This is duplicating logic in snap-discard-ns but it doesn't
# support --all switch yet so we cannot use it.
if [ -d /run/snapd/ns ]; then
    for mnt in /run/snapd/ns/*.mnt; do
        umount -l "$mnt" || true
        rm -f "$mnt"
    done
    find /run/snapd/ns/ \( -name '*.fstab' -o -name '*.user-fstab' -o -name '*.info' \) -delete
fi
