# libapparmor user-space query peeker

This library can be injected into a process to observe queries from user-space
to the kernel.

## DBus (system session)

It's the same as the user session override below, but you _have to_ reboot the
system, as dbus.service cannot be restarted.

## DBus (user session)

Create an override file at
`~/.config/systemd/user/dbus.service.d/override.conf` with the following
content:

```
[Service]
Environment=LD_PRELOAD=/path/to/libaa-query-peek.so
```

Reload systemd with:

```sh
systemctl --user daemon-reload
```

## Logs

The preload library writes messages to standard error. Bytes <= 32 are escaped
as `\xFF`.

A typical D-Bus query looks like this:
```
aa_query_label mask:0x2, query:label\x00snap.mattermost-desktop.mattermost-desktop\x00\x20session\x00org.freedesktop.DBus\x00unconfined\x00/org/freedesktop/DBus\x00org.freedesktop.DBus\x00NameHasOwner, size:145, -> 0, allowed:0x1, audited:0
```

The `\x20` escape code stands for `AA_CLASS_DBUS`.
