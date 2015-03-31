Snappy garbage collection
=========================

A snappy package [app? part?] present in a system can be in several states:

- installed (but not active)
- active-to-be (e.g. once you reboot)
- active
- removed

note that a *removed* part is still present; the data has not been *purged*.

When a part is updated, its data is copied (actually right now it's a link tree,
a tree of directories containing hard links to the files, but I'm not sure
that's a good idea: if the new version corrupts the data, rollback is
compromised) to a new location which is used by the updated part.

*Garbage collection* is what we call the mechanism of removing, and purging,
 installed but not active parts of the system, with the objective of saving disk
 space without compromising the ability to revert your system to a previous
 known-good state.

When you update part of your system we'll keep the one old
installed-but-not-active part installed, remove the one before that, and purge
anything prior. This means that for parts that have an explicit active-to-be
state, at most four versions will be present; for parts without that, at most
three versions will be present.

Explicitly removing part of your system will also remove *and purge* all prior
versions.

Purging part of your system purges all versions of it.

You can disable garbage collection with the `--no-gc` commandline option, or
when removing or purging a part, by specifying the version on which to operate
explicitly (in which case `--gc` will enable gc again, and will apply from the
specified version back).
