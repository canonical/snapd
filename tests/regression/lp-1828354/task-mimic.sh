#!/bin/bash -xe
# shellcheck source=tests/lib/defer.sh
. ../../lib/defer.sh

prime_defer

# shellcheck source=tests/lib/dirs.sh
. ../../lib/dirs.sh

# Most of the test is about this snap name. Use a variable for brevity.
snap=test-snapd-app-mimic

# This improves visibility in output-rich programs.
test_failed() {
    e=$?

    printf "\n*** THIS HAS FAILED ***\n\n";

    set  -x +e
    case "${DEBUG_ACTION:-}" in
        shell) /bin/bash ;;
        shell-into-snap-ns) SNAP_INSTANCE_NAME="$snap" /usr/lib/snapd/snap-confine "snap.$snap.sh" /bin/bash ;;
        shell-into-user-ns) sudo su test -c "SNAP_INSTANCE_NAME=$snap /usr/lib/snapd/snap-confine snap.$snap.sh /bin/bash" ;;
    esac
    set -e +x
    exit "$e"
}

set -E
trap test_failed ERR

# Enable persistence of per-user-mount-namespaces.
#
# This allows us to see persistent state of the per-user mount namespace.
# This test relies on this because otherwise per-user mount namespace is always
# re-created from per-snap mount namespace, on each command invocation.
snap set system experimental.per-user-mount-namespace=true
defer snap set system experimental.per-user-mount-namespace=false

# Install our primary snap and connect it to mount-observe.
snap pack "$snap"
defer rm "${snap}_1_all.snap"
sudo snap install --devmode --dangerous "./${snap}_1_all.snap"
defer sudo snap remove "$snap"
snap connect "$snap:mount-observe"

# Later on remove the private temporary directories we are about to create.
defer sudo rm -rf "/tmp/snap.$snap"

# Install the supporting content snap.
snap pack test-snapd-content
defer rm test-snapd-content_1_all.snap
snap install --dangerous ./test-snapd-content_1_all.snap
defer sudo snap remove test-snapd-content

# Define helpers that run a command inside the snap as either root or test
# user. Those are different from "snap-run" that it doesn't rely on snap-exec
# which can misbehave when $SNAP/meta/ is clobbered.
as_snap_root() {
    snap run "$snap.sh" -c "$*"
    # sudo su root -c "SNAP=/snap/$snap/x1 SNAP_INSTANCE_NAME=$snap \"$LIBEXECDIR/snapd/snap-confine\" snap.$snap.sh $*"
}

as_snap_user() {
    su test -c "snap run \"$snap.sh\" -c \"$*\""
    # sudo su test -c "SNAP=/snap/$snap/x1 SNAP_INSTANCE_NAME=$snap \"$LIBEXECDIR/snapd/snap-confine\" snap.$snap.sh $*"
}

# Define properties that should hold in at all times.
assert_invariant() {
    # Existing canary files are not clobbered.
    as_snap_root /usr/bin/test -f "/snap/$snap/x1/canary"
    as_snap_root /usr/bin/test -f "/snap/$snap/x1/dir/canary"
    as_snap_root /usr/bin/test -f "/snap/$snap/x1/meta/canary"
    test "$(as_snap_root /bin/cat "/snap/$snap/x1/canary")" = "app:canary"
    test "$(as_snap_root /bin/cat "/snap/$snap/x1/dir/canary")" = "app:dir/canary"
    test "$(as_snap_root /bin/cat "/snap/$snap/x1/meta/canary")" = "app:meta/canary"

    # Same as above but for snap user in per-user mount namespace.
    as_snap_user /usr/bin/test -f "/snap/$snap/x1/canary"
    as_snap_user /usr/bin/test -f "/snap/$snap/x1/dir/canary"
    as_snap_user /usr/bin/test -f "/snap/$snap/x1/meta/canary"
    test "$(as_snap_user /bin/cat "/snap/$snap/x1/canary")" = "app:canary"
    test "$(as_snap_user /bin/cat "/snap/$snap/x1/dir/canary")" = "app:dir/canary"
    test "$(as_snap_user /bin/cat "/snap/$snap/x1/meta/canary")" = "app:meta/canary"
}

# Define properties that should hold when the content snap is not connected.
assert_disconnected() {
    # The dynamically-created tmpfs directory does not exist.
    # There's no writable mimic so nothing shadows $SNAP.
    # Mount event propagation from $SNAP is shared (to ns:user) and slave (from ns:sys).
    as_snap_root /usr/bin/test ! -e "/snap/$snap/x1/tmpfs"
    test "$(as_snap_root /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type)" = squashfs
    test "$(as_snap_root /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- --one --renumber .opt_fields "/snap/$snap/x1")" = "shared:1 master:2"

    # Same as above but for snap user in per-user mount namespace.
    as_snap_user /usr/bin/test ! -e "/snap/$snap/x1/tmpfs"
    test "$(as_snap_user /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type)" = squashfs
    test "$(as_snap_user /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- --one --renumber .opt_fields "/snap/$snap/x1")" = "shared:1 master:2"
}

# Define properties that should hold when the content snap connected.
assert_connected() {
    # The dynamically-created tmpfs directory exists.
    as_snap_root /usr/bin/test -d "/snap/$snap/x1/tmpfs/"
    # There's a writable mimic shadowing the original squashfs.
    test "$(as_snap_root /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- "/snap/$snap/x1" .fs_type)" = "$(printf "squashfs\ntmpfs\n")"

    # The content is mounted underneath and is not clobbered.
    as_snap_root /usr/bin/test -d "/snap/$snap/x1/tmpfs/content"
    as_snap_root /usr/bin/test -f "/snap/$snap/x1/tmpfs/content/canary"
    test "$(as_snap_root /bin/cat "/snap/$snap/x1/tmpfs/content/canary")" = "content:content/canary"

    # Same as above but for snap user in per-user mount namespace.
    as_snap_user /usr/bin/test -d "/snap/$snap/x1/tmpfs/"
    test "$(as_snap_user /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- "/snap/$snap/x1" .fs_type)" = "$(printf "squashfs\ntmpfs\n")"
    as_snap_user /usr/bin/test -d "/snap/$snap/x1/tmpfs/content"
    as_snap_user /usr/bin/test -f "/snap/$snap/x1/tmpfs/content/canary"
    test "$(as_snap_user /bin/cat "/snap/$snap/x1/tmpfs/content/canary")" = "content:content/canary"

    # The block device fueling the mimic is the same in both namespaces.
    # NOTE: we don't do remapping and we expect to see the exact same numbers.
    test "$(as_snap_root /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- "/snap/$snap/x1" .fs_type=tmpfs .dev)" = "$(as_snap_user /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- "/snap/$snap/x1" .fs_type=tmpfs .dev)"

    # Propagation of the tmpfs is identical in both namespaces (shared:xxx)
    test "$(as_snap_root /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type=tmpfs .opt_fields)" = "$(as_snap_user /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type=tmpfs .opt_fields)"
    test "$(as_snap_root /bin/cat /proc/self/mountinfo | ./mountinfo-tool -f- --one --renumber "/snap/$snap/x1" .fs_type=tmpfs .opt_fields)" = shared:1
}

# Verify that initial state when the content is not connected is as expected.
sudo /usr/lib/snapd/snap-discard-ns "$snap"
assert_invariant
assert_disconnected

# Verify that initial state when the content connected is as expected.
sudo /usr/lib/snapd/snap-discard-ns "$snap"
snap connect "$snap:content" test-snapd-content:content
assert_invariant
assert_connected

# Now, without discarding the mount namespace verify we can perform transitions:
# connected -> disconnected -> connected
snap disconnect "$snap:content" test-snapd-content:content
assert_invariant
assert_disconnected

snap connect "$snap:content" test-snapd-content:content
assert_invariant
assert_connected

echo "MIMIC TEST COMPLETE"
