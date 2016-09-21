# Gadget snappy package

The `gadget` snap package is a snap package `type` that is used to setup and
personalize the system.

There can only be *one* snap package of `type: gadget` and it can only be
installed during image provision.

## gadget snaps

The gadget snap is a regular snap with some additional files and
conventions.

### meta/gadget.yaml

The gadget snap has an additional `meta/gadget.yaml` file that describes
the gadget specific configuration. The `gadget.yaml` structure is:

    bootloader: {grub,uboot}

More entries to describe the partitions and bootloader installation
will be added.

### File layout conventions

The bootloader configuration is expected to be at the toplevel of the
gadget snap. The filename is `${bootloader_name}.conf`, e.g.
`grub.conf` or `uboot.conf`. This file will be instaleld into the boot
partition as `grub.cfg` and `uboot.env`. The bootloader configuration
contains the boot logic. Examples for the boot logic can be found in
the `pc` and the `pi2` gadget snaps.

