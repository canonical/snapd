# Core-initrd architecture

In UC20 and further, initrd is migrated from script based
implementation (UC16/18) to **systemd based**. Here we do a high-level
description of UC's core-initrd architecture. The repository contains
a native debian source package which contains different files and
helper scripts that are used to build an initramfs that is used in
Ubuntu Core systems.

This package is meant to be installed in a classic Ubuntu system
jointly with a kernel so an initrd and a `kernel.efi` binary are
built. On x86, the latter must be included in a kernel snap so it can
be loaded by grub to start a UC system. For other architectures, we
might need only the generated initramfs.

The generated `kernel.efi` binaries are EFI Unified Kernel Images, as
described under "Type #2" in systemd's [bootloader
specification](https://systemd.io/BOOT_LOADER_SPECIFICATION/) (BLS).

## Directory structure

```
|-bin        -> helper scripts to build the initramfs
|-debian     -> debian folder to build deb package
|-factory    -> some files that will be included verbatim in the initramfs
|-features   -> files also part of the initramfs, but optional
|-postinst.d -> script that will run on kernel updates
|-snakeoil   -> keys to sign binaries for testing
|-tests      -> spread tests
└-vendor     -> vendordized systemd
```

In more detail,

- `bin/` contains the `ubuntu-core-initramfs` python script, that is
  used to generate the initramfs and the `kernel.efi` file.
- `debian/` is used to build the debian binary package, including the
  vendordized systemd
- `factory/` essentially consists of configuration files and some
  additional scripts used by some services. These files are copied to
  the target initramfs.
- `features/` defines additional files to add to the initramfs, but
  selectable when running `ubuntu-core-initramfs` via the `--feature`
  argument. This allows some flexibility when creating the
  initramfs. Each "feature" matches a subfolder inside
  `features/`. Currently we have `server` and `cloudimg-rootfs`. The
  former is selected when building x86 images, while the latter is
  added when building cloud images.
- `postinst.d/` contains a script that is installed as a kernel hook
  when the deb is installed. This scripts rebuilds initramfs and EFI
  image on kernel installation.
- `snakeoil/` contains a public/private key pair that is used to sign
  the EFI image by default by `ubuntu-core-initramfs`, and a file with
  UEFI variables for OVMF (which includes the public key) that is
  used by the spread tests.
- `tests/` contains spread tests that are run by CI/CD
- `vendor/` contains a vendordized version of systemd with some
  modifications compared to the one in Ubuntu classic. The additional
  patches are
    - `ubuntu-core-initramfs-clock-util-read-timestamp-from-usr-lib-clock-epoch.patch`,
       which was taken from upstream
    - `ubuntu-core-initramfs-fix-default.target`, that changes the default
      target for the initrd

### Updating the systemd fork

The systemd sources are the same as in the Ubuntu debian package,
although further modified with additional patches and compiled with
slightly different configuration options. The sources can be updated
by using git subtree to import new versions from the Ubuntu debian
package auto-import repo. To do that:

    $ git subtree pull --prefix vendor/systemd/ https://git.launchpad.net/ubuntu/+source/systemd ubuntu/<release> --squash

Where release could be focal, jammy, etc. Note that when a development
version is released we will probably want `ubuntu/<release>-updates`
branch instead.

When moving to a newer Ubuntu release, the way to update is to remove
the old sources in a commit and then import with something like:

    $ git subtree add --prefix vendor/systemd https://git.launchpad.net/ubuntu/+source/systemd ubuntu/<new_release> --squash

## Typical boot sequence 

On arm SBC's:
FSBL-->SBL-->Uboot-->Kernel-->**Initrd**-->pivot-root to persistent storage root.

On amd64 devices:
Bootrom-->UEFI Firmware-->shim-->GRUB-->sd-stub->Kernel-->**Initrd**-->pivot-root to persistent storage root.
