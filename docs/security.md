# Snap security policy and sandboxing

Snap packages run confined under a restrictive security sandbox by default.
The security policies and store policies work together to allow developers to
quickly update their applications and to provide safety to end users.

This document describes the sandbox and how to configure and work with the security policies for snap
packages.

# How policy is applied
Application authors should not have to know about or understand the lowlevel
implementation details on how security policy is enforced. Instead, all snaps
run under default security policy which can be extended through the use of
interfaces, slots and plugs and the available interfaces available on the
device can be seen with:

    $ snap interfaces

The description of these interfaces is found in `interfaces.md`.

Each command declared in `apps` by the snap is tracked by the system by
assigning a security label to the command. This security label takes the form
of `snap.<name>.<app>` where `<name>` is the name of the snap from `meta.md`
and `<app>` is the command name. For example, if this is in `snap.yaml`:

    name: foo
    ...
    apps:
      bar:
        command: ...
        ...

then the security label for the `bar` command is `snap.foo.bar`. This security
label is used throughout the system including in the enforcement of security
policy by the app launcher. All snap commands declared via `apps` in `meta.md`
are launched by the launcher and snaps run in the global (ie, default)
namespace (except where noted otherwise) to facilitate communications and
sharing between snaps and because this is more familiar for developers and
administrators. The security policy and launcher enforce application isolation
as per the snappy FHS. Under the hood, the launcher:

* Sets up various environment variables:
    * `HOME`: set to `SNAP_DATA` for daemons and `SNAP_USER_DATA` for user
      commands
    * `SNAP`: read-only install directory
    * `SNAP_ARCH`: the architecture of device (eg, amd64, arm64, armhf, i386, etc)
    * `SNAP_DATA`: writable area for the snap
    * `SNAP_LIBRARY_PATH`: additional directories added to `LD_LIBRARY_PATH`
    * `SNAP_NAME`: snap name (from `meta.md`)
    * `SNAP_REVISION`: store revision of the snap
    * `SNAP_USER_DATA`: per-user writable area for the snap
    * `SNAP_VERSION`: snap version (from `meta.md`)
    * `TMPDIR`: set to `/tmp`
* When hardware is assigned to the snap, sets up a device cgroup with default
  devices (eg, /dev/null, /dev/urandom, etc) and any devices that are assigned
  to this snap
* Sets up a private /tmp using a per-command private mount namespace and
  mounting a per-command directory on /tmp
* Sets up a per-command devpts new instance
* Sets up the seccomp filter for the command
* Executes the command under the command-specific AppArmor profile under a
  default nice value

This combination of restrictive AppArmor profiles (which mediate file access,
application execution, Linux capabilities(7), mount, ptrace, IPC, signals,
coarse-grained networking), clearly defined application-specific filesystem
areas, whitelist syscall filtering via seccomp, private /tmp, new instance
devpts and device cgroups provides for strong application confinement and
isolation.

## AppArmor
Upon snap package install, `snap.yaml` is examined and AppArmor profiles are
generated for each command to have the appropriate security label and
command-specific AppArmor rules. As mentioned, each command runs under an
app-specific default policy that may be extended through declared interfaces
which are expressed in the yaml as `plugs` and `slots`.

## Seccomp
Like with AppArmor, upon snap package install, `snap.yaml` is examined and
seccomp filters are generated for each command to run under a default seccomp
filter that may be extended through declared interfaces which are expressed in
the yaml as `plugs` and `slots`.

# Working with snap security policy

The `snap.yaml` need not specify anything for default confinement and may
optionally specify `plugs` and `slots` to declare additional interfaces to use.
When an interface is connected, the snap's security policy will be updated to
allow access to use the interface. See `meta.md` and `interface.md` for
details.

The default AppArmor policy is deny by default and snaps are restricted to
their app-specific directories, libraries, etc (enforcing ro, rw, etc). The
seccomp filter is also deny by default and the default filter allows enough
safe syscalls so that snaps using the default security policy should work.

Eg, consider the following:

    name: foo
    version: 1.0
    apps:
      bar:
        command: bar
      baz:
        command: baz
        daemon: simple
        plugs: [network]

then:

* the security label for `bar` is `snap.foo.bar`. It uses only the default
  policy
* the security label for `baz` is `snap.foo.baz`. It uses the `default` policy plus the `network` interface security policy as provided by the OS snap

Security policies and store policies work together to provide flexibility,
speed and safety. Because of this, use of some interfaces may trigger a manual
review in the official Ubuntu store and/or may need to be connected by the user
or gadget snap developer.

The interfaces available on the system and those used by snaps can be seen with
the `snap interfaces` command. Eg:

    $ snap interfaces
    Slot                 Plug
    :firewall-control    -
    :home                -
    :locale-control      -
    :log-observe         snappy-debug
    :mount-observe       -
    :network             xkcd-webserver
    :network-bind        xkcd-webserver
    :network-control     -
    :network-observe     -
    :snapd-control       -
    :system-observe      -
    :timeserver-control  -
    :timezone-control    -

In the above it can be seen that the `snappy-debug` snap has the `log-observe`
interface connected (and therefore the security policy from `log-observe` is
added to it) and the `xkcd-webserver` has the `network` and `network-bind`
interfaces connected. An interesting quality of interfaces is that they may
either be either declared per-command or per-snap. If declared per-snap, all
the commands within the snap have the interface security policy added to the
command's security policy when the interface is connected. If declared
per-command, only the commands within the snap that declare use of the
interface have the interface security policy added to them.

Snappy may autoconnect the requested interfaces upon install or may require the
user to manually connect them. Interface connections and disconnections are
performed via the `snap connect` and `snap disconnect` commands. See
`interfaces.md` for details.

# Developer mode
Sometimes it is helpful when developing a snap to not have to worry about the
security sandbox in order to focus on developing the snap. To support this,
snappy allows installing the snap in developer mode which puts the security
policy in complain mode (where violations against security policy are logged,
but permitted). Eg:

    $ sudo snap install --devmode <snap>

# Debugging
To check to see if you have any policy violations:

    $ sudo grep audit /var/log/syslog

An AppArmor violation will look something like:

    audit: type=1400 audit(1431384420.408:319): apparmor="DENIED" operation="mkdir" profile="snap.foo.bar" name="/var/lib/foo" pid=637 comm="bar" requested_mask="c" denied_mask="c" fsuid=0 ouid=0

If there are no AppArmor denials, AppArmor shouldn't be blocking the snap.

A seccomp violation will look something like:

    audit: type=1326 audit(1430766107.122:16): auid=1000 uid=1000 gid=1000 ses=15 pid=1491 comm="env" exe="/bin/bash" sig=31 arch=40000028 syscall=983045 compat=0 ip=0xb6fb0bd6 code=0x0

The `syscall=983045` can be resolved with the `scmp\_sys\_resolver` command:

    $ scmp_sys_resolver 983045
    set_tls

If there are no seccomp violations, seccomp isn't blocking the snap.

The `snappy-debug` snap can be used to help with policy violations. To use it:

    $ sudo snap install snappy-debug
    $ sudo /snap/bin/snappy-debug.security scanlog foo

This will:

* adjust kernel log rate limiting
* follow /var/log/syslog looking for policy violations for `foo`
* resolve syscall names
* make reccomendations on how to fix violations

See `snappy-debug.security help` for details.

If you believe there is a bug in the security policy or want to request a new
interface, please [file a bug](https://bugs.launchpad.net/snappy/+filebug),
adding the `snapd-interface` tag.
