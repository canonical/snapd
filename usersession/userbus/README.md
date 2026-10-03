# User bus bridge

`userbus.Connect(ctx, uid)` returns an owned godbus connection to the regular
`/run/user/<uid>/bus`. Root can select any existing user; an unprivileged caller
can connect only as itself. The user bus and, for systemd operations, the user
manager must already exist.

The connection uses the `snapd-userbus` multicall entry point from the invoking
snapd's executable tree. The helper runs with the selected UID, its primary
GID, and (when launched by root) no supplementary groups. It originates the
upstream connection and authentication credentials. The parent authenticates
with `EXTERNAL` without claiming its own UID.

The helper relays authentication and then complete D-Bus wire messages over a
UNIX socketpair. It preserves message bodies, serials, and FD indices, forwarding
`SCM_RIGHTS` in both directions. Message framing matters: UNIX stream sockets can
coalesce plain writes with a subsequent descriptor-bearing write. Forwarding
arbitrary chunks would associate descriptors with the wrong D-Bus message.

## Lifetime

The context passed to `Connect` controls the entire connection lifetime. Setup
I/O has an additional 30-second limit. Give individual method calls their own
contexts. Always call the returned connection's `Close`, which also reaps the
helper. Cancellation or a disconnected bus terminates the helper. Closing a
connection does not stop a service already accepted by systemd.

The caller owns application files passed as `dbus.UnixFD` and must keep them
open until the send completes. The caller also owns any received descriptors.
The helper closes its temporary descriptor copies after each forwarded message.

The current godbus v5.2.2 dependency leaks received descriptors when a canceled
call's late reply is discarded, even if the connection is subsequently closed.
The upstream fix is tracked in [godbus/dbus#443](https://github.com/godbus/dbus/pull/443).
Its regression test is opt-in with `SNAPD_TEST_GODBUS_FD_FIX=1` until the fixed
dependency is adopted.

## systemd signals

Use the embedded godbus connection for ordinary method calls and signal match
rules. To observe jobs reliably:

1. Register and drain a signal channel, filtering by signal name.
2. Install a match for `org.freedesktop.systemd1.Manager.JobRemoved` from
   `org.freedesktop.systemd1`.
3. Call `org.freedesktop.systemd1.Manager.Subscribe` and wait for its reply.
4. Submit jobs and correlate results by job object path.

Keep early job events: a job can finish before the submitting goroutine processes
the method reply. A successful start-job result is distinct from completion of
the service's program. A manager restart or connection loss can lose signals;
callers must reconcile outstanding operations rather than retry blindly.

This package supplies the connection. It does not provision sessions, cache
connections, subscribe to systemd automatically, or track jobs on callers' behalf.
