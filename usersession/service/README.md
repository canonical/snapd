# Direct user-service management

This package manages persistent snap user units through the public
`org.freedesktop.systemd1` API on `/run/user/UID/bus`. The protocol implementation
is in `systemd/user`; `usersession/userbus` supplies the credential-switched
connection. Unit files remain in `/etc/systemd/user`.

## Targets and ownership

`Select` enumerates active `user@UID.service` instances using PID 1's public
`ListUnits` method. Lingering managers are eligible without a login session.
UIDs are validated, sorted and deduplicated. A nil selection means all running
managers; an explicitly empty selection means none.

An explicitly requested UID without a running manager is an error. An all-user
operation with no running managers succeeds without per-user work. A running
manager without a regular user bus is unavailable, including legacy sessions
where the agent could use systemd's private socket. There is no private-socket
fallback, session provisioning, or automatic lingering configuration.

`Targets` is an immutable operation snapshot. `Do` reports the selected,
successful and failed UIDs. Callers carry these results into preference
bookkeeping rather than discovering sessions again. Refresh disabled-service
snapshots also carry their selected UIDs into stop and rollback operations.

Each `WithUID` call owns a connection and holds a process-wide per-UID gate
throughout a complete operation and its cleanup. Status-dependent restart
selection is performed within this gate. Neither discovery nor gate acquisition
may happen while holding the overlord state lock. API status requests batch
their units and close the bridge before decorating the response.

## Operations and compatibility

Starts run in supplied order and stop on the first error. Partial starts are
cleaned up for that user; another user's successful operation is not undone.
Stop attempts each unit and disables only after every stop succeeds. Restart
keeps the existing stop-then-start sequence. Socket/timer selection, snap scope,
and global enablement are handled by wrappers.

The initial migration retains the agent's batch enable/disable rollback
semantics and the service task's existing all-success preference-commit rule.
In particular, it does not introduce per-user masks to override global
enablement. Global user-unit settings continue to apply to future logins.

Method calls have operation contexts. The connection outlives cancellation long
enough to run bounded cleanup, and is then closed and its helper reaped. A lost
reply can leave job acceptance unknown; restart requests are never retried
automatically after a disconnect.

`systemd/user` installs signal subscriptions before submitting a job, retains
early completion signals, and correlates by job path within one manager-owner
generation. Manager loss and signal-buffer overflow fail outstanding work.
The bounded signal handler preserves wire ordering without unbounded deferred
signal-delivery goroutines.

The session agent provides notifications and session information. Notification
target discovery continues to use agent sockets. Its former `/v1/service-control`
and `/v1/service-status` endpoints are retired and return HTTP 404. Older daemons
that require these endpoints cannot manage services through this version of the
agent, including mixed-version refresh or rollback configurations. External
systemctl clients do not participate in snapd's per-UID gates.
