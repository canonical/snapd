==============================
 snap-confine-suid-trampoline
==============================

-----------------------------------------------------------------
internal tool for running snap-confine from the core snap as root
-----------------------------------------------------------------

:Author: zygmunt.krynicki@canonical.com
:Date:   2017-03-14
:Copyright: Canonical Ltd.
:Version: 2.24
:Manual section: 5
:Manual group: snappy

SYNOPSIS
========

	snap-confine-suid-trampoline ...

DESCRIPTION
===========

`snap-confine-suid-trampoline` program assists snapd in running `snap-confine`
from the *core* snap. It is not meant to be used directly.

OPTIONS
=======

The `snap-confine-suid-trampoline` program does not support any options
directly, all the command line arguments are forwarded as-is to the copy of
`snap-confine` in the core snap.

ENVIRONMENT
===========

See the manual page of `snap-confine` for details.

FILES
=====

`/snap/core/current/lib/$ARCH_TRIPLET/ld-2.30.so`:
    Path of the dynamic linker in the core snap.

`/snap/core/current/usr/lib/snapd/snap-confine`:
    Path of the `snap-confine` in the *core* snap.

Note that your distribution may have moved the `/snap` directory to
`/var/lib/snapd/snap`. This manual page text is static.

BUGS
====

Please report all bugs with https://bugs.launchpad.net/snap-confine/+filebug
