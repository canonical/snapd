# Snappy Transactional System Updates on a/b systems

## Introduction

This document provides an overview of how system-level updates are applied to
a snappy system, and how the system boot operates.

Note that this document describes systems that use a snappy a/b partition
layout. Newer snappy systems use a different mechanism called "all-snap" that
is using a different strategy for the transnational updates.

## System-level updates

Unlike a traditional Ubuntu system where individual packages are updated as
required, a snappy system is split into two logical pieces:

*   The read-only minimal base system

    This comprises required configuration file, standard directories,
    libraries, utilities and core system services.This portion of the system
    is read-only and individual elements cannot be updated. It is known as a
    "system image". There are up to two system images available on a snappy
    system.

*   The writable apps ("snaps") and frameworks part of the system.

    These make use of services provided by the base system.

## Root partitions

The base system is embodied conventionally by a root filesystem (containing
a system image) on a separate disk partition. However, unlike a standard
Ubuntu system, a snappy system has dual read-only root filesystem partitions
(two system images).

Snappy manages these "A/B" system partitions which are used both to:

* Allow a root partition to be updated as a single unit (by applying a
  new system image).

* Support rolling back to the "other" root filesystem in the case of
  problems with the most recently-installed root filesystem (by making
  the bootloader switch root filesystems).

## Partitions

All snappy systems comprises the following disk partitions:

Partition Label | Default Size (MB)       | Filesystem type | Writable? | Description
--------------  | ----------------------- | --------------- | --------- | --------------------------
`system-boot`   | 64                      | vfat            |  Yes      | Boot-related files
`system-a`      | 1024                    | ext4            |  No       | Primary root filesystem
`system-b`      | 1024                    | ext4            |  No       | Secondary root filesystem
`writable`      | *(all remaining space)* | ext4            |  Yes      | All persistent user data

Notes:

The boot partition must be formatted as a vfat filesystem since:

* This is required for EFI systems.
* vfat is supported by the majority of u-boot variants and grub.

### U-Boot-based systems

Systems that use the u-boot bootloader (such as armhf hosts) have the
following partition layout:

Partition Label | Default Size (MB)       | Writable? | Notes
--------------- | ----------------------- | --------- | --------------------------------------------------------------------------------------
`system-boot`   | 64                      | Yes       | Used to store snappy bootloader variables.
`system-a`      | 1024                    | No        | Provisioned automatically by `ubuntu-device-flash`.
`system-b`      | 1024                    | No        | Provisioned automatically by `ubuntu-device-flash` at the same revision as `system-a`.
`writable`      | *(all remaining space)* | Yes

### Grub-based systems

Systems that use the grub bootloader (such as `i386` and `amd64` hosts) have
the following partition layout:

Partition Label | Default Size (MB)       | Writable? | Notes
--------------- | ----------------------- | --------- | ----------------------------------------------------------------------------------------------------------------
`grub`          | 4                       | Yes       | Partition containing grub installation.
`system-boot`   | 64                      | Yes       | EFI-capable partition (note that EFI is not currently used though. Also used to store snappy bootloader variables.
`system-a`      | 1024                    | No        | Provisioned automatically by ubuntu-device-flash.
`system-b`      | 1024                    | No        | Formatted, but not provisioned for cloud hosts.
`writable`      | *(all remaining space)* | Yes       |

## Bootloader Configuration

Snappy sets a small number of bootloader variables which provide state
both for snappy and the bootloader itself.

### Primary variables

Bootloader variable | Default value    | Permissible values        | Description
------------------- | ---------------- | ------------------------- | ----------------------------------
`snappy_mode`       | `regular`        | "`regular`" or "`try`"    | Type of boot in operation.
`snappy_ab`         | "`a`" or "`b`"   | "`a`" or "`b`"            | Denotes rootfs to attempt to boot.

#### `snappy_mode`

This variable is initially set to "`regular`" which corresponds to a normal
boot (no special behaviour occurs).

Setting the variable to "try" will inform the bootloader that it should
attempt to boot a brand-new (never booted) root filesystem.

#### `snappy_ab`

This variable specifies which of the two possible root filesystems the
bootloader should attempt to use when "`snappy_mode=try`".

### Configuration Files

The variables in the table above are stored in different locations, depending
on the system bootloader:

Bootloader  | Configuration file               | Description
----------- | -------------------------------- | ---------------------------------------------------------
grub        | `/boot/grub/grubenv`             | Default location for grub environment block.
u-boot      | `/boot/uboot/snappy-system.txt`  | File sourced by `/boot/uboot/uEnv.txt` on snappy systems.

## Boot Assets

The location of the boot assets depends on the bootloader being used:

### Grub

Boot assets                 | Boot assets partition
--------------------------- | --------------------------
`/boot/vmlinuz-$version`    | `system-a` and `system-b`
`/boot/initrd.img-$version` |

On grub systems, "`/boot/grub`" is where the system-boot partition is
mounted. However, "`/boot`" is part of the read-only root filesystem image.

### u-boot

Boot assets                              | Boot assets partition
-----------------------------------------| ---------------------
`/boot/uboot/$snappy_ab/vmlinuz`         | `system-boot`
`/boot/uboot/$snappy_ab/initrd.img`      |
`/boot/uboot/$snappy_ab/dtbs/$board.dtb` |


Key:

* `$version` expands to a kernel version.
* `$snappy_ab` expands to a value of the `snappy_ab` variable.
* `$board` expands to a name representing the device.

## Updating the System

When a new system image is available it can be applied simply by
running::

$ sudo snappy update

This command will:

1.  Download the latest system image.
1.  Apply the latest system image to the other root partition.
1.  Update the bootloader configuration such that the next boot will
    automatically be using the latest system image by setting the
    following bootloader variables:
    1. Set "`snappy_mode=try`".
    2. Set "`snappy_ab=$rootfs`" where "`$rootfs`" depends on which rootfs
       should be attempted on next boot.

### Booting in "try-mode"

When the bootloader runs on next boot, it will detect that
"`snappy_mode=try`" and know that the rootfs specified by "`snappy_ab`" has
not yet been successfully booted. It will then perform an action that it
expects to be "undone" if the next boot is successful:

Bootloader | Action on "`snappy_mode=try`"                      | Undone by
---------- | -------------------------------------------------- | ------------------------------------------------
grub       | Sets "`snappy_trial_boot=1`" bootloader variable   | `/lib/systemd/system/ubuntu-core-snappy.service`
u-boot     | Creates empty file ``/boot/uboot/snappy-stamp.txt` | `/lib/systemd/system/ubuntu-core-snappy.service`

Notes:

* The action is undone using a boot script (which calls `snappy`).
* The actions performed by u-boot are different to grub since some versions
  of u-boot are unable to reliably write files on `vfat` partitions (which
  would be required if it were to rewrite the snappy variables file). As
  such, u-boot is simply expected to "`touch`" a zero-length file on a `vfat
  partition, which most versions of u-boot are able to do.

#### Successful next boot

If the next boot succeeds, snappy will undo the actions performed by the
bootloader:

* Change "`snappy_mode`" from "`try`" to "`regular`".
* On Grub systems, unset the standard "`snappy_trial_boot`" bootloader
  variable.
* On U-boot systems, remove the file `/boot/uboot/snappy-stamp.txt`.

These actions will inform the bootloader on next boot to continue to use
the current root filesystem, since it is now known to be usable.

#### Failed next boot

If the next boot fails, the actions performed by the bootloader will have
failed to be undone. Thus on subsequent boot, the bootloader will still see
"`snappy_mode=try`" and know the rootfs specified by "`snappy_ab`" is
bad. The bootloader itself will then force a revert to the other root
filesystem, which is known to be usable:

* Change "`snappy_mode`" from "`try`" to "`regular`".
* Change "`snappy_ab`" from its current value to the "other" value.

snappy will simply fail to modify the "`snappy_mode`" bootloader variable
such that on the subsequent boot, the bootloader will detect that
"`snappy_mode`" is still set to "`try`" and thus realise that the last boot
failed to change this setting. The bootloader will then know that the rootfs
specified by "`snappy_ab`" is "bad" and automatically revert to using the
"other" rootfs (which is known to be usable).

### Rolling Back the System

To revert to using the previous system image, simply run::

    $ sudo snappy rollback ubuntu-core

This command will update the bootloader configuration to ensure that the
next boot automatically uses the "other" system image rather than the
current one:

* Set "`snappy_ab`" to the "other" value.

For example, if "`snappy_ab=b`", the rollback will set "`snappy_ab=a`".

Note that there is no need to modify "`snappy_mode`" since the previous
rootfs is always usable.

## Failure Resilience

Snappy currently offers a few strategies to ensure recovery from failure
scenarios.

### Current Story

#### Kernel or Init system failure

The kernel command-line is automatically set to include "`panic=-1`"
meaning that if either the kernel panics or if the init system fails,
the system will automatically reboot.

*   If the failure occurs early in boot, the first time after a new system
    image has been applied, the system will automatically reboot, reverting
    back to the "last good system image", using the same logic as outlined in
    the section *Updating the System*.

    Early boot is defined as being "any time before a login prompt is
    available".

*   If the failure occurs after the system has successfully booted, the
    system will be restarted automatically and will continue to use the
    existing system image.

#### Hanging Boot

If the system hangs in early boot, the first time after a new system image
has been applied, forcibly power-cycling the system will cause it to boot
using the "last good system image".

#### Corrupt Bootloader Variables

Since the bootloader variables are on a writable device, it is possible
the files could become corrupt either due to bad media, user error or
power failure whilst writes were occurring. Snappy attempts to protect
against the last scenario by mounting the boot partition using the
"`sync`" option.

Snappy systems attempt to deal with corrupt bootloader variables by
providing default values to the bootloader in the case where the
bootloader cannot read the values written by snappy.

### Future

The following sections are ideas for future improvements; there is no
guarantee that they will be implemented.

#### Automatic reboot on hung boot

It would be beneficial to automatically detect a hung boot and reboot
without requiring manual intervention to power cycle. However, this is a
difficult problem since some devices are inherently slow to boot so it
is currently unclear how best to solve this problem.

A compromise may be to introduce a well-known boot sequence point /
"try_watchdog" service which could be configured as required by users
for systems with fixed hardware (and thus a known "worst-case" boot
experience). Snappy could provide this service, disabled by default,
along with a default value (in seconds) for the maximum time a system is
expected to boot to a `getty` login prompt within. Users would be able to
modify the default number of seconds to suite their particular systems.

#### Minimise writes to boot partition

It would be possible to further reduce the amount of data written to the
boot partition on u-boot systems by making `/boot/uboot/snappy-system.txt`
only include the current snappy variables (`snappy_mode` and `snappy_ab`)
and introducing an intermediate file such that:

* `/boot/uboot/uEnv.txt` sources `/boot/uboot/snappy-common.txt`.
* `/boot/uboot/snappy-common.txt` sources `/boot/uboot/snappy-system.txt`.

This would further reduce the possibility of an unbootable system since
`snappy-common.txt` (which would never be written) could contain default
values for `snappy_mode` and `snappy_ab`.

#### Disaster-recovery

Snappy systems should:

* Tolerate systems where the writable partition is full or corrupt.
* Allow the bootloader to be re-installed if the system-boot partition
  becomes corrupt.
* Allow the bootloader configuration to be re-installed if it becomes
  corrupt.

#### Multiple Disks

*   Support systems with dual local disk devices:
*   Support systems with minimal writable partition and a remote
    writable mount for user data.
  
    This would make most of the snappy image recreatable by
    ubuntu-device-flash.

#### Miscellaneous

* Support UEFI secure boot.
* Handle booting with encrypted partitions.

## References

* https://developer.ubuntu.com/en/snappy/porting/
* https://developer.ubuntu.com/en/snappy/guides/filesystem-layout/

