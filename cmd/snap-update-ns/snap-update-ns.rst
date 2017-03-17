================
 snap-update-ns
================

-----------------------------------------------------------------------
internal tool for updating preserved namespaces of snappy applications
-----------------------------------------------------------------------

:Author: zygmunt.krynicki@canonical.com
:Date:   2017-01-17
:Copyright: Canonical Ltd.
:Version: 2.22
:Manual section: 5
:Manual group: snappy

SYNOPSIS
========

	snap-update-ns SNAP_NAME

DESCRIPTION
===========

The `snap-update-ns` is a program used internally by `snapd` to update a
preserved mount namespace of a particular snap.

OPTIONS
=======

The `snap-update-ns` program does not support any options.

ENVIRONMENT
===========

`snap-update-ns` responds to the following environment variables

`SNAP_CONFINE_DEBUG`:
	When defined the program will print additional diagnostic information about
	the actions being performed. All the output goes to stderr.

FILES
=====

`snap-update-ns` uses the following files:

`/run/snapd/ns/$SNAP_NAME.mnt`:

    The preserved mount namespace that is upadted by `snap-update-ns`.

`/proc/self/mountinfo`:

    Kernel representation of the mount table of the current process.

`/var/lib/snapd/mount/$SNAP_NAME.fstab`:
    
    Desired, persistent mount table for the given snap.
    
`/run/snapd/ns/$SNAP_NAME.fstab`:

    Current, ephemeral mount table for the gien snap.

BUGS
====

Please report all bugs with https://bugs.launchpad.net/snap-wrap/+filebug
