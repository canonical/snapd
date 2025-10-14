# Initramfs for Ubuntu Core and hybrid systems

This folder contains files that are used to build the initramfs for
Ubuntu Core 24 / hybrid 24.04 and later versions, and that were
originally in https://github.com/canonical/core-initrd. This contains
subfolders, each of them for a currently supported Ubuntu release.

Each subfolder contains the sources for a debian package. The `latest`
subdir contains the sources for the most recent Ubuntu release.

When doing changes, add information about them by running either `dch -i` if
this is the first change after a PPA upload, or `dch -a` for subsequent
changes. Leave the distro release as UNRELEASED. The value of the debian
version it not important at this point.

When releasing a new version, first check out the desired snapd
release/commit - the repo needs to be initially in a clean state. Then create
source packages that can later be built by Launchpad, by running:

```
./build-source-pkgs.sh
```

This will pull the sources to build `snap-bootstrap` from the snapd tree and
copy duplicated files from the `latest` folder to older releases. It will also
finalize the packages changelog, setting the final version and the
distribution. A pull request to the snapd repository should be proposed at this
point, to get the changes reviewed and merged.

Then it is recommended to compare the source packages with
the previous versions in the snappy-dev PPA:

```
dget https://launchpad.net/~snappy-dev/+archive/ubuntu/image/+sourcefiles/ubuntu-core-initramfs/<old_version>/ubuntu-core-initramfs_<old_version>.dsc
debdiff ubuntu-core-initramfs_<old_version>.dsc ubuntu-core-initramfs_<new_version>.dsc > diff.txt
```

And finally upload the new packages with:

```
dput ppa:snappy-dev/image ubuntu-core-initramfs_<new_version>_source.changes
```
