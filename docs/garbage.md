# Snappy garbage collection

As a snap package is updated, old versions are kept around to enable switching
back to old, known-good versions using `rollback`. *Garbage collection* is
performed automatically to preserve the ability of doing this rollback without
consuming an overly large amount of disk space.

A snap present in a system can be in several states:

- `installed` (but not active)
- `active`
- `removed`

note that a *removed* snap is still present; the data has not been *purged*.

> Also note that garbage collection only applies to snaps packaged as `.snap`
> files, and does not (currently) apply to `ubuntu-core` nor `enablement`.

When a snap is updated, its data is copied (actually right now it's a link tree,
a tree of directories containing hard links to the files, but I'm not sure
that's a good idea: if the new version corrupts the data, rollback is
compromised) to a new location which is used by the updated snap.

Garbage collection is, then, what we call the mechanism of removing and
purging installed but not active snaps, with the objective of saving disk
space without compromising the ability to revert your system to a previous
known-good state.

When you update a snap we'll keep one old snap installed but not active,
remove the one before that, and purge anything prior. This means that at most
three versions of a snap will be present on the system, with the third one
being `removed` but not `purged`.

> For example, if you let's look at installing and updating `hello-world`
> through a few versions. Let's say you have version 1.0.1 installed, and you
> do a `snappy update` which downloads version 1.0.2,
>
>     $ sudo snappy update
>     Installing hello-world.canonical (1.0.2)
>     Starting download of hello-world.canonical
>     30.99 KB / 30.99 KB [======================]
>     Done
>     Name        Date    Version Developer
>     hello-world 1-01-01 1.0.2   canonical
>     $ snappy list | grep hello
>     hello-world 2015-03-31 1.0.2   canonical
>     $ snappy list -v | grep hello
>     hello-world  2015-03-31 1.0.1   canonical
>     hello-world* 2015-03-31 1.0.2   canonical
>
> so, it downloaded `1.0.2`, made it active, and left `1.0.1` installed. Let's
> do it again!
>
>     $ sudo snappy update
>     Installing hello-world.canonical (1.0.3)
>     Starting download of hello-world.canonical
>     30.99 KB / 30.99 KB [======================]
>     Done
>     Name        Date    Version Developer
>     hello-world 1-01-01 1.0.3   canonical
>     $ snappy list -v | grep hello
>     hello-world# 2015-03-31 1.0.1   canonical
>     hello-world  2015-03-31 1.0.2   canonical
>     hello-world* 2015-03-31 1.0.3   canonical
>
> and `1.0.1` is mostly gone. If we were to iterate it once again, `1.0.1`
> would drop off completely.

Explicitly removing a snap from your system will also remove *and purge* all
prior versions.

Purging a snap from your system purges all versions of it.

You can disable garbage collection with the `--no-gc` commandline option, or
when removing or purging a part, by specifying the version on which to operate
explicitly.

## Future work and/or discussion

* The above mentions `purge`, which is still TBD.
* If/when old data dirs are backed up instead of copied, the gc mechanism will
  need to adapt. Not a big issue, but we need to be aware of it (tests will
  fail anyway, so we should be good).
* Once `ubuntu-core` and enablement become .snaps, should they be gc'ed?
  (probably not `ubuntu-core`; probably yes enablement. The logic will likely
  need to change.)
* Do we need to provide configuration options for `.snap` authors to specify
  tweaks to this gc policy?

