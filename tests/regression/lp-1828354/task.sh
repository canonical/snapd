#!/bin/bash -ex

# Define helpers that run a command inside the snap as root or test user.
# The call to "su root -c" is redundant but makes for nice parity with the
# second function below.
as_snap_root() {
    su root -c "snap run \"$snap.sh\" -c \"$*\""
}

as_snap_user() {
    su test -c "snap run \"$snap.sh\" -c \"$*\""
}

# Depending on the test variant define different assert functions.
case "$VARIANT" in
    mimic)
        # Define properties that should hold in at all times.
        assert_invariant() {
            echo "## Asserting invariant properties"
            echo "### Existing canary files are not clobbered."
            as_snap_root /usr/bin/test -f "/snap/$snap/x1/canary"
            as_snap_root /usr/bin/test -f "/snap/$snap/x1/dir/canary"
            as_snap_root /usr/bin/test -f "/snap/$snap/x1/meta/canary"
            test "$(as_snap_root /bin/cat "/snap/$snap/x1/canary")" = "app:canary"
            test "$(as_snap_root /bin/cat "/snap/$snap/x1/dir/canary")" = "app:dir/canary"
            test "$(as_snap_root /bin/cat "/snap/$snap/x1/meta/canary")" = "app:meta/canary"

            echo "### Same as above but for snap user in per-user mount namespace."
            as_snap_user /usr/bin/test -f "/snap/$snap/x1/canary"
            as_snap_user /usr/bin/test -f "/snap/$snap/x1/dir/canary"
            as_snap_user /usr/bin/test -f "/snap/$snap/x1/meta/canary"
            test "$(as_snap_user /bin/cat "/snap/$snap/x1/canary")" = "app:canary"
            test "$(as_snap_user /bin/cat "/snap/$snap/x1/dir/canary")" = "app:dir/canary"
            test "$(as_snap_user /bin/cat "/snap/$snap/x1/meta/canary")" = "app:meta/canary"
        }

        # Define properties that should hold when the content snap is not connected.
        assert_disconnected() {
            echo "## Asserting properties when content snap is disconnected"
            echo "### The dynamically-created tmpfs directory does not exist."
            echo "### There's no writable mimic so nothing shadows \$SNAP."
            echo "### Mount event propagation from \$SNAP is shared (to ns:user) and slave (from ns:sys)."
            as_snap_root /usr/bin/test ! -e "/snap/$snap/x1/tmpfs"
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type)" = squashfs
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one .opt_fields "/snap/$snap/x1" | sed -e 's/[0-9]\+/nnn/g')" = "shared:nnn master:nnn"

            echo "### Same as above but for snap user in per-user mount namespace."
            as_snap_user /usr/bin/test ! -e "/snap/$snap/x1/tmpfs"
            test "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type)" = squashfs
            test "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one .opt_fields "/snap/$snap/x1" | sed -e 's/[0-9]\+/nnn/g')" = "shared:nnn master:nnn"
        }

        # Define properties that should hold when the content snap connected.
        assert_connected() {
            echo "## Asserting properties when content snap is connected"
            echo "### The dynamically-created tmpfs directory exists."
            as_snap_root /usr/bin/test -d "/snap/$snap/x1/tmpfs/"
            echo "### There's a writable mimic shadowing the original squashfs."
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- "/snap/$snap/x1" .fs_type)" = "$(printf "squashfs\ntmpfs\n")"

            echo "### The content is mounted underneath and is not clobbered."
            as_snap_root /usr/bin/test -d "/snap/$snap/x1/tmpfs/content"
            as_snap_root /usr/bin/test -f "/snap/$snap/x1/tmpfs/content/canary"
            test "$(as_snap_root /bin/cat "/snap/$snap/x1/tmpfs/content/canary")" = "content:content/canary"

            echo "### Same as above but for snap user in per-user mount namespace."
            as_snap_user /usr/bin/test -d "/snap/$snap/x1/tmpfs/"
            test "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- "/snap/$snap/x1" .fs_type)" = "$(printf "squashfs\ntmpfs\n")"
            as_snap_user /usr/bin/test -d "/snap/$snap/x1/tmpfs/content"
            as_snap_user /usr/bin/test -f "/snap/$snap/x1/tmpfs/content/canary"
            test "$(as_snap_user /bin/cat "/snap/$snap/x1/tmpfs/content/canary")" = "content:content/canary"

            echo "### The block device fueling the mimic is the same in both namespaces."
            echo "### NOTE: we don't do remapping and we expect to see the exact same numbers."
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- "/snap/$snap/x1" .fs_type=tmpfs .dev)" = \
                 "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- "/snap/$snap/x1" .fs_type=tmpfs .dev)"

            echo "### Propagation of the tmpfs is identical in both namespaces (shared:xxx)"
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type=tmpfs .opt_fields)" = \
                 "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1" .fs_type=tmpfs .opt_fields)"
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one --renumber "/snap/$snap/x1" .fs_type=tmpfs .opt_fields | sed -e 's/[0-9]\+/nnn/')" = shared:nnn

        }
    ;;
    layout)
        # Define properties that should hold in at all times.
        assert_invariant() {
            echo "## Asserting invariant properties"
            echo "### Existing canary files are not clobbered except for"
            echo "### the file that is shadowed by layout-defined tmpfs."
            as_snap_root /usr/bin/test -f "/snap/$snap/x1/canary"
            as_snap_root /usr/bin/test -f "/snap/$snap/x1/meta/canary"
            test "$(as_snap_root /bin/cat "/snap/$snap/x1/canary")" = "app:canary"
            test "$(as_snap_root /bin/cat "/snap/$snap/x1/meta/canary")" = "app:meta/canary"
            as_snap_root /usr/bin/test -d "/snap/$snap/x1/tmpfs"
            as_snap_root /usr/bin/test ! -f "/snap/$snap/x1/tmpfs/canary"

            echo "### Same as above but for snap user in per-user mount namespace."
            as_snap_user /usr/bin/test -f "/snap/$snap/x1/canary"
            as_snap_user /usr/bin/test -f "/snap/$snap/x1/meta/canary"
            test "$(as_snap_user /bin/cat "/snap/$snap/x1/canary")" = "app:canary"
            test "$(as_snap_user /bin/cat "/snap/$snap/x1/meta/canary")" = "app:meta/canary"
            as_snap_user /usr/bin/test -d "/snap/$snap/x1/tmpfs"
            as_snap_root /usr/bin/test ! -f "/snap/$snap/x1/tmpfs/canary"
        }

        # Define properties that should hold when the content snap is not connected.
        assert_disconnected() {
            echo "## Asserting properties when content snap is disconnected"
            echo "### Content mount is simply gone."
            as_snap_root /usr/bin/test ! -e "/snap/$snap/x1/tmpfs/content"
            as_snap_user /usr/bin/test ! -e "/snap/$snap/x1/tmpfs/content"

            # FIXME: the comments are not consistent with measurements. Are we doing it right?
            echo "### Mount event propagation in ns:sys for \$SNAP/tmpfs is shared (to ns:user) and slave (from ns:sys)."
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1/tmpfs" .fs_type)" = tmpfs
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1/tmpfs" .opt_fields | sed -e 's/[0-9]\+/nnn/')" = shared:nnn
            echo "### Mount event propagation in ns:user for \$SNAP/tmpfs is shared (to ns:user) and slave (from ns:sys)."
            test "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1/tmpfs" .fs_type)" = tmpfs
            test "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1/tmpfs" .opt_fields | sed -e 's/[0-9]\+/nnn/')" = shared:nnn
        }

        # Define properties that should hold when the content snap connected.
        assert_connected() {
            echo "## Asserting properties when content snap is connected"
            echo "### The content is mounted underneath and is not clobbered."
            as_snap_root /usr/bin/test -d "/snap/$snap/x1/tmpfs/content"
            as_snap_root /usr/bin/test -f "/snap/$snap/x1/tmpfs/content/canary"
            test "$(as_snap_root /bin/cat "/snap/$snap/x1/tmpfs/content/canary")" = "content:content/canary"

            echo "### Same as above but for snap user in per-user mount namespace."
            as_snap_user /usr/bin/test -d "/snap/$snap/x1/tmpfs/content"
            as_snap_user /usr/bin/test -f "/snap/$snap/x1/tmpfs/content/canary"
            test "$(as_snap_user /bin/cat "/snap/$snap/x1/tmpfs/content/canary")" = "content:content/canary"

            echo "### Propagation of the tmpfs is identical in both namespaces (shared:xxx)"
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1/tmpfs" .fs_type=tmpfs .opt_fields)" = \
                 "$(as_snap_user /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1/tmpfs" .fs_type=tmpfs .opt_fields)"
            test "$(as_snap_root /bin/cat /proc/self/mountinfo | mountinfo-tool -f- --one "/snap/$snap/x1/tmpfs" .fs_type=tmpfs .opt_fields | sed -e 's/[0-9]\+/nnn/')" = shared:nnn
        }
    ;;
esac

echo "# Verify that initial state when the content is not connected is as expected."
snap-tool snap-discard-ns "$snap"
assert_invariant
assert_disconnected

echo "# Verify that initial state when the content connected is as expected."
snap-tool snap-discard-ns "$snap"
snap connect "$snap:content" test-snapd-content:content
assert_invariant
assert_connected

echo "# Now, without discarding the mount namespace verify we can perform transitions:"
echo "# connected -> disconnected -> connected"
snap disconnect "$snap:content" test-snapd-content:content
assert_invariant
assert_disconnected

snap connect "$snap:content" test-snapd-content:content
assert_invariant
assert_connected
