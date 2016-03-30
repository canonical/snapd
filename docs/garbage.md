# Snappy garbage collection

As a snap package is updated, old versions are kept around to enable switching
back to old, known-good versions using `rollback`. *Garbage collection* is
performed automatically to preserve the ability of doing this rollback without
consuming an overly large amount of disk space.

A snap present in a system can be in several states:

- `installed` (but not active)
- `active`

When a snap is updated, its data is copied to a new location which is
used by the updated snap.

Garbage collection is, then, what we call the mechanism of removing and
purging installed but not active snaps, with the objective of saving disk
space without compromising the ability to revert your system to a previous
known-good state.

When you update a snap we'll keep one old snap installed but not active,
remove and purge the one before that, and anything prior. This means that at
most two versions of a snap will be present on the system.

Explicitly removing a snap from your system will also remove *and purge* all
prior versions.

You can disable garbage collection with the `--no-gc` commandline option, or
when removing or purging a part, by specifying the version on which to operate
explicitly.

## Example

Let's look at installing and updating `hello-world` through a few
versions. Let's say you have version 1.0.1 installed, and you do a `snappy
update` which downloads version 1.0.2,

    $ sudo snappy update
    Installing hello-world.canonical (1.0.2)
    Starting download of hello-world.canonical
    30.99 KB / 30.99 KB [======================]
    Done
    Name        Date    Version Developer
    hello-world 1-01-01 1.0.2   canonical
    $ snappy list | grep hello
    hello-world 2015-03-31 1.0.2   canonical
    $ snappy list -v | grep hello
    hello-world  2015-03-31 1.0.1   canonical
    hello-world* 2015-03-31 1.0.2   canonical

so, it downloaded `1.0.2`, made it active, and left `1.0.1` installed. Let's
do it again!

    $ sudo snappy update
    Installing hello-world.canonical (1.0.3)
    Starting download of hello-world.canonical
    30.99 KB / 30.99 KB [======================]
    Done
    Name        Date    Version Developer
    hello-world 1-01-01 1.0.3   canonical
    $ snappy list -v | grep hello
    hello-world  2015-03-31 1.0.2   canonical
    hello-world* 2015-03-31 1.0.3   canonical

and `1.0.1` is gone.

## Future work and/or discussion

* Do we need to provide configuration options for `.snap` authors to specify
  tweaks to this gc policy?
