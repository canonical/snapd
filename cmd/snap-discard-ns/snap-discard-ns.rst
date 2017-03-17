================
 snap-discard-ns
================

------------------------------------------------------------------------
internal tool for discarding preserved namespaces of snappy applications
------------------------------------------------------------------------

:Author: zygmunt.krynicki@canonical.com
:Date:   2016-10-05
:Copyright: Canonical Ltd.
:Version: 1.0.43
:Manual section: 5
:Manual group: snappy

SYNOPSIS
========

	snap-discard-ns SNAP_NAME

DESCRIPTION
===========

The `snap-discard-ns` is a program used internally by `snapd` to discard a preserved
mount namespace of a particular snap.

OPTIONS
=======

The `snap-discard-ns` program does not support any options.

ENVIRONMENT
===========

`snap-discard-ns` responds to the following environment variables

`SNAP_CONFINE_DEBUG`:
	When defined the program will print additional diagnostic information about
	the actions being performed. All the output goes to stderr.

FILES
=====

`snap-discard-ns` uses the following files:

`/run/snapd/ns/$SNAP_NAME.mnt`:

    The preserved mount namespace that is unmounted by `snap-discard-ns`.

BUGS
====

Please report all bugs with https://bugs.launchpad.net/snap-wrap/+filebug
