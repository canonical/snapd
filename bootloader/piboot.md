# Support for piboot booloader on UC

## Introduction

In this document we describe the support for piboot on Raspberry Pi
devices when running Ubuntu Core. When we talk about "piboot", we mean
the different closed-source firmware bits that are run on RPi devices
before the Linux kernel or alternative bootloaders like U-Boot are
started.

Support for piboot has been introduced to avoid the dependency on
U-Boot for UC images on Raspberry Pi devices. U-Boot is not officially
supported by the RPi foundation, which has led in the past to delays
in our releases and feature gaps.

## Piboot features

Recently, a failsafe OS update feature was added to piboot [1]. It is
implemented with a flag that is set when rebooting with a special
parameter. When that flag is set, piboot loads as configuration a file
named `tryboot.txt` instead of the usual `config.txt`. This flag is
cleared by piboot on reboots. Furthermore, it is volatile, so it does
not survive after power offs. To enable the flag, the reboot syscall
must be called with the special "0 tryboot" argument (from command
line: `sudo reboot "0 tryboot"`).

With this new feature, it has been possible to implement piboot
support in snapd. However, piboot has far less capabilities than grub
or U-Boot, which are the more usual bootloaders for UC. The main
limitations it has are:

1. Piboot avoids any write to disk
1. We can cold-boot only from the first partition in the disk. This
   implies that we need to write boot assets to the ubuntu-seed
   partition instead of to ubuntu-boot. (NOTE: it seems like there is
   actually an undocumented way to cold-boot from any FAT partition,
   by using a file called `autoboot.txt` - however, this is not
   officially supported by the RPi foundation and that status is not
   expected to change any time soon).
1. As explained, the OS updates mechanism depends on a volatile flag
   that gets removed in cold boots. That makes it not possible to
   distinguish sometimes between failed updates and having
   power-cycled a device before really trying a pending update.
1. There is no scripting language for the RPi bootloader. The only way
   to influence its behavior is by changes to the
   `{config,tryboot}.txt` files. Their capabilities are described in
   the reference for the format [2].

## Implementation

Due to the piboot limitations, the support in snapd/UC has some
peculiarities. The way we will use it is

1. Piboot config files can have an `os_prefix` setting that we will
   leverage to point to different folders for different kernel snaps
   (`/piboot/ubuntu/pi-kernel_<rev>.snap/`, or `/systems/<id>/kernel/`
   on first boot). These folders will contain different kernel,
   initrd, dtbs, dtbos, and `cmdline.txt` each one, so refreshing a
   kernel will be a matter of extracting/creating these assets and
   setting `os_prefix` appropriately.
1. On kernel refresh, we will create a `tryboot.txt` file pointing to
   the new assets in its `os_prefix` setting, and reboot with a "0
   tryboot" parameter so we use `tryboot.txt` after rebooting. The
   `cmdline.txt` in the `os_prefix` folder will contain a special
   parameter (`kernel_status=trying`) so UC knows that we are using
   the configuration from `tryboot.txt`. With this, the OS can
   understand if the new kernel has been used to boot.

To keep state we have environment files that, differently to grub or
U-Boot, are not directly read or written by the bootloader, but are
instead translated to bootloader configuration files when kernel
updates happen. These environment files are named `piboot.conf` and
live in the `piboot/ubuntu/` folder inside ubuntu-seed partition, and
for robustness they use the same format as the `uboot.env` files, with
a CRC header. In run mode, the folder from the boot partition would be
mounted in the `/boot/piboot/` directory. This is analogous to what is
done by UC for grub and U-Boot environment files.

### Snapd

The piboot environment files have the same format as U-Boot
environment files so we leverage the existing snapd codebase and take
advantage of error detection capabilities. We store there key/value
pairs. The `GetBootVars()`/`SetBootVars()` methods for the bootloader
interface read and modify these files in the usual way. However, these
files do not directly affect the bootloader any more, so we need to
generate the bootloader configuration files when some of the variables
are changed, that is, when `SetBootVars()` is called it will re-create
the bootloader configuration depending on what has changed. The method
that implements the changes will have as input the environment file in
the seed partition, and will generate `{config,tryboot}.txt` and
`cmdline.txt` accordingly.

The logic that changes the `kernel_status` variable in other
bootloaders (see `bootloader/assets/data/grub.cfg`) cannot be done by
piboot as it does not support scripting and is closed source. Instead,
basically the same thing has been implemented in the initramfs, and it
is run as part of the snap bootstrap code (see
`boot.updateNotScriptableBootloaderStatus()` function). Running it
from the initramfs ensures that the code is run only once in
boot. This code looks at the kernel command line and checks if
`kernel_status=trying` is present to change the status in the
environment file to `try`. Once snapd starts, it will check and set
`kernel_status` in the usual way for any bootloader.

### RPi gadget

Gadgets of the “bootloader: piboot” type need to have

1. An empty `piboot.conf file` (which will contain environment variables
   at runtime as explained above, and will be filled with values by
   snapd during image preparation, installation and kernel updates)
1. Reference configuration file for the bootloader, named
   `config.txt`. It will be used to create `config.txt` in the seed
   partition, with a different `os_prefix`.
1. Reference kernel command line file for the bootloader, named
   `cmdline.txt`. It will be used to generate the UC kernel
   command line.

The reference files will be used while configuring the bootloader from
snapd. The rest of it will be very similar to our current pi gadget
snaps.

## References

[1] https://www.raspberrypi.com/documentation/computers/raspberry-pi.html#fail-safe-os-updates-tryboot

[2] https://www.raspberrypi.com/documentation/computers/config_txt.html
