# Initramfs for Ubuntu Core and hybrid systems

This folder contains files that are used to build the initramfs for
Ubuntu Core 24 / hybrid 24.04 and later versions, and that were
originally in https://github.com/canonical/core-initrd. This contains
subfolders, each of them for a currently supported Ubuntu release.

Each subfolder contains the sources for a debian package. The `latest`
subdir contains the sources for the most recent Ubuntu release. When
releasing, first checkout the desired snapd release. Then `dch -i`
should be run for each package to update version and changelog, and
this should be committed to the snapd release and master branches.

To finally create source packages that can later be built by
Launchpad, run from this folder:

```
./build-source-pkgs.sh
```

This will pull the sources to build `snap-bootstrap` from the snapd
tree and copy duplicated files from the `latest` folder to older
releases. Then it is recommended to compare the source packages with
the previous versions in the snappy-dev PPA:

```
dget https://launchpad.net/~snappy-dev/+archive/ubuntu/image/+sourcefiles/ubuntu-core-initramfs/<old_version>/ubuntu-core-initramfs_<old_version>.dsc
debdiff ubuntu-core-initramfs_<old_version>.dsc ubuntu-core-initramfs_<new_version>.dsc > diff.txt
```

And finally upload with:

```
dput ppa:snappy-dev/image ubuntu-core-initramfs_<new_version>_source.changes
```
