==============
 snap-wrap
==============

-----------------------------------------------
internal tool for confining snappy applications
-----------------------------------------------

:Author: zygmunt.krynicki@canonical.com
:Date:   2016-10-05
:Copyright: Canonical Ltd.
:Version: 1.0.43
:Manual section: 5
:Manual group: snappy

SYNOPSIS
========

	snap-wrap SECURITY_TAG COMMAND [...ARGUMENTS]

DESCRIPTION
===========

The `snap-wrap` is a program used internally by `snapd` to construct a
confined execution environment for snap applications.

OPTIONS
=======

The `snap-wrap` program does not support any options.

FEATURES
========

Apparmor profiles
-----------------

`snap-wrap` switches to the apparmor profile `$SECURITY_TAG`. The profile is
**mandatory** and `snap-wrap` will refuse to run without it.

has to be loaded into the kernel prior to using `snap-wrap`. Typically this
is arranged for by `snapd`. The profile contains rich description of what the
application process is allowed to do, this includes system calls, file paths,
access patterns, linux capabilities, etc. The apparmor profile can also do
extensive dbus mediation. Refer to apparmor documentation for more details.

Seccomp profiles
----------------

`snap-wrap` looks for the `/var/lib/snapd/seccomp/profiles/$SECURITY_TAG`
file. This file is **mandatory** and `snap-wrap` will refuse to run without
it.

The file is read and parsed using a custom syntax that describes the set of
allowed system calls and optionally their arguments. The profile is then used
to confine the started application.

As a security precaution disallowed system calls cause the started application
executable to be killed by the kernel. In the future this restriction may be
lifted to return `EPERM` instead.

Mount profiles
--------------

`snap-wrap` looks for the `/var/lib/snapd/mount/$SECURITY_TAG.fstab` file.
If present it is read, parsed and treated like a typical `fstab(5)` file.
The mount directives listed there are executed in order. All directives must
succeed as any failure will abort execution.

By default all mount entries start with the following flags: `bind`, `ro`,
`nodev`, `nosuid`.  Some of those flags can be reversed by an appropriate
option (e.g. `rw` can cause the mount point to be writable).

As a security precaution only `bind` mounts are supported at this time.

Quirks
------

`snap-wrap` contains a quirk system that emulates some or the behavior of
the older versions of snap-wrap that certain snaps (still in devmode but
useful and important) have grown to rely on. This section documents the list of
quirks:

- The /var/lib/lxd directory, if it exists on the host, is made available in
  the execution environment. This allows various snaps, while running in
  devmode, to access the LXD socket. LP: #1613845

Sharing of the mount namespace
------------------------------

As of version 1.0.41 all the applications from the same snap will share the
same mount namespace. Applications from different snaps continue to use
separate mount namespaces.

ENVIRONMENT
===========

`snap-wrap` responds to the following environment variables

`SNAP_CONFINE_DEBUG`:
	When defined the program will print additional diagnostic information about
	the actions being performed. All the output goes to stderr.

The following variables are only used when `snap-wrap` is not setuid root.
This is only applicable when testing the program itself.

`SNAPPY_LAUNCHER_INSIDE_TESTS`:
	Internal variable that should not be relied upon.

`SNAP_CONFINE_NO_ROOT`:
	Internal variable that should not be relied upon.

`SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR`:
	Internal variable that should not be relied upon.

`SNAP_USER_DATA`:
    Full path to the directory like /home/$LOGNAME/snap/$SNAP_NAME/$SNAP_REVISION.

    This directory is created by snap-wrap on startup. This is a temporary
    feature that will be merged into snapd's snap-run command. The set of directories
    that can be created is confined with apparmor.

FILES
=====

`snap-wrap` uses the following files:

`/var/lib/snapd/mount/*.fstab`:

	Description of the mount profile.

`/var/lib/snapd/seccomp/profiles/*`:

	Description of the seccomp profile.

`/run/snapd/ns/`:

    Directory used to keep shared mount namespaces.

    `snap-wrap` internally converts this directory to a private bind mount.
    Semantically the behavior is identical to the following mount commands:

    mount --bind /run/snapd/ns /run/snapd/ns
    mount --make-private /run/snapd/ns

`/run/snapd/ns/.lock`:

    A `flock(2)`-based lock file acquired to create and convert
    `/run/snapd/ns/` to a private bind mount.

`/run/snapd/ns/$SNAP_NAME.lock`:

    A `flock(2)`-based lock file acquired to create or join the mount namespace
    represented as `/run/snaps/ns/$SNAP_NAME.mnt`.

`/run/snapd/ns/$SNAP_NAME.mnt`:

    This file can be either:

    - An empty file that may be seen before the mount namespace is preserved or
      when the mount namespace is unmounted.
    - A file belonging to the `nsfs` file system, representing a fully
      populated mount namespace of a given snap. The file is bind mounted from
      `/proc/self/ns/mnt` from the first process in any snap.

`/proc/self/mountinfo`:

    This file is read to decide if `/run/snapd/ns/` needs to be created and
    converted to a private bind mount, as described above.

Note that the apparmor profile is external to `snap-wrap` and is loaded
directly into the kernel. The actual apparmor profile is managed by `snapd`.

BUGS
====

Please report all bugs with https://bugs.launchpad.net/snap-wrap/+filebug
