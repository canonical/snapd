==============
 snap-confine
==============

-----------------------------------------------
internal tool for confining snappy applications
-----------------------------------------------

:Author: zygmunt.krynicki@canonical.com
:Date:   2016-06-27
:Copyright: Canonical Ltd.
:Version: 1.0.33
:Manual section: 5
:Manual group: snappy

SYNOPSIS
========

	snap-confine SECURITY_TAG COMMAND [...ARGUMENTS]

DESCRIPTION
===========

The `snap-confine` is a program used internally by `snapd` to construct a
confined execution environment for snap applications.

OPTIONS
=======

The `snap-confine` program does not support any options.

FEATURES
========

Apparmor profiles
-----------------

`snap-confine` switches to the apparmor profile `$SECURITY_TAG`. The profile is
**mandatory** and `snap-confine` will refuse to run without it.

has to be loaded into the kernel prior to using `snap-confine`. Typically this
is arranged for by `snapd`. The profile contains rich description of what the
application process is allowed to do, this includes system calls, file paths,
access patterns, linux capabilities, etc. The apparmor profile can also do
extensive dbus mediation. Refer to apparmor documentation for more details.

Seccomp profiles
----------------

`snap-confine` looks for the `/var/lib/snapd/seccomp/profiles/$SECURITY_TAG`
file. This file is **mandatory** and `snap-confine` will refuse to run without
it.

The file is read and parsed using a custom syntax that describes the set of
allowed system calls and optionally their arguments. The profile is then used
to confine the started application.

As a security precaution disallowed system calls cause the started application
executable to be killed by the kernel. In the future this restriction may be
lifted to return `EPERM` instead.

Mount profiles
--------------

`snap-confine` looks for the `/var/lib/snapd/mount/$SECURITY_TAG.fstab` file.
If present it is read, parsed and treated like a typical `fstab(5)` file.
The mount directives listed there are executed in order. All directives must
succeed as any failure will abort execution.

By default all mount entries start with the following flags: `bind`, `ro`,
`nodev`, `nosuid`.  Some of those flags can be reversed by an appropriate
option (e.g. `rw` can cause the mount point to be writable).

As a security precaution only `bind` mounts are supported at this time.

ENVIRONMENT
===========

`snap-confine` responds to the following environment variables

`SNAP_CONFINE_DEBUG`:
	When defined the program will print additional diagnostic information about
	the actions being performed. All the output goes to stderr.

The following variables are only used when `snap-confine` is not setuid root.
This is only applicable when testing the program itself.

`SNAPPY_LAUNCHER_INSIDE_TESTS`:
	Internal variable that should not be relied upon.

`UBUNTU_CORE_LAUNCHER_NO_ROOT`:
	Internal variable that should not be relied upon.

`SNAPPY_LAUNCHER_SECCOMP_PROFILE_DIR`:
	Internal variable that should not be relied upon.

FILES
=====

`snap-confine` uses the following files:

`/var/lib/snapd/mount/*.fstab`:

	Description of the mount profile.

`/var/lib/snapd/seccomp/profiles/*`:

	Description of the seccomp profile.

Note that the apparmor profile is external to `snap-confine` and is loaded
directly into the kernel. The actual apparmor profile is managed by `snapd`.

BUGS
====

Please report all bugs with https://bugs.launchpad.net/snap-confine/+filebug
