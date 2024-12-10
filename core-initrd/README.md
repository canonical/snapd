# Initramfs for Ubuntu Core and hybrid systems

This folder contains files that are used to build the initramfs for
Ubuntu Core 24 / hybrid 24.04 and later versions, and that were
originally in https://github.com/canonical/core-initrd. This contains
subfolders, each of them for a currently supported Ubuntu release.

Each subfolder contains the sources for a debian package. The `latest`
subdir contains the sources for the most recent Ubuntu release. To
build source packages that can later be built by Launchpad, checkout
the matching snapd release and run from this folder:

```
./build-source-pkgs.sh
```

This will pull the sources to build `snap-bootstrap` from the snapd
tree and copy duplicated files from the `latest` folder to older
releases. At this point `dch -i` should be run for each release to
update version and changelog, and this should be commited to the snapd
release and master branches. To build the source packages, run

```
gbp buildpackage -S -sa -d --git-ignore-branch
```

in each release subfolder. Then it is recommended to compare the
sources with the previous versions in the snappy-de PPA:

```
dget https://launchpad.net/~snappy-dev/+archive/ubuntu/image/+sourcefiles/ubuntu-core-initramfs/<old_version>/ubuntu-core-initramfs_<old_version>.dsc
debdiff ubuntu-core-initramfs_<old_version>.dsc ubuntu-core-initramfs_<new_version>.dsc > diff.txt
```

And to finally upload with:

```
dput ppa:snappy-dev/image ubuntu-core-initramfs_<new_version>_source.changes
```
