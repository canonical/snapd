# Initramfs for Ubuntu Core and hybrid systems

This folder contains files that are used to build the initramfs for
Ubuntu Core 24 / hybrid 24.04 and later versions, and that were
originally in https://github.com/canonical/core-initrd. This contains
subfolders, each of them for a currently supported Ubuntu release.

Each subfolder contains the sources for a debian package. The subfolder for
the latest release contains most of the sources for all the releases.

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
copy duplicated files from the latest release folder to older releases. It will
also finalize the packages changelog, setting the final version and the
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

When a new Ubuntu release starts development, the contents of the previously
latest release should be moved to a new folder for the new release, except the
debian folder that should be copied. A new changelog entry for the new release
must be added, increasing the version number and setting the new code name, so
builds will be performed for this new release. For instance:

ubuntu-core-initramfs (72+2.72+g149.d9c5491+25.10) questing; urgency=medium

gets transformed to

ubuntu-core-initramfs (73+2.72+g149.d9c5491+26.04) resolute; urgency=medium
.

When a release becomes unmaintained, the content of the folder must be removed,
and inside the folder a README.md file must be created with content:

"debian folder removed as XX.XX is now unmaintained."

It is also possible to build the package just for one release, for instance:

```
./build-source-pkgs.sh 26.04
```

which is what is done for testing using spread - the package gets built just
for the release after testing.
