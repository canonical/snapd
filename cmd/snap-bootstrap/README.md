# _snap-bootstrap_

Welcome to the world of the initramfs of UC20! 

_snap-bootstrap_ is the main executable that is run during the initramfs stage of UC20. It has several responsibilities:

1. Mounting some partitions from the disk that UC20 is installed to. This includes ubuntu-data, ubuntu-boot, ubuntu-seed, and if present, ubuntu-save (ubuntu-save is optional on unencrypted devices).
1. As part of mounting those partitions, _snap-bootstrap_ may perform the necessary steps to unlock any encrypted partitions such as ubuntu-data and ubuntu-save. 
1. After unlocking and mounting all such partitions, _snap-bootstrap_ then chooses which base snap file is to be used for the root filesystem of userspace (as the root filesystem of the initramfs is just a static set of files built into the initramfs and is not the final root filesystem), and mounts this base snap file.
1. _snap-bootstrap_ then chooses which kernel snap file is to be used to mount and find additional kernel modules that are not compiled into the kernel or shipped as modules inside the initramfs or otherwise loaded as DTBs, etc.
1. _snap-bootstrap_ then also will mount the ubuntu-data partition such that either the writable components of the root filesystem come from this actual partition, or if the mode the system is booting into is an ephemeral system such as install or recover, will mount a temporary filesystem for this.
1. _snap-bootstrap_ on kernel and base snap upgrades will also handle updating bootloader environment variables to implement A/B or try-boot functionality.
1. _snap-bootstrap_ then finally may do some additional setup of the root filesystem such as copying some default files for ephemeral system modes such as recover. 

## Degraded recover mode

When booting into recover mode, _snap-bootstrap_ has some additional logic setup to try and be as robust as possible. This logic is fairly complicated and best explained in the following state diagram showing the states and transitions that _snap-bootstrap_ operates in during recover mode, which has been called degraded mode.

![](/cmd/snap-bootstrap/degraded-recover-mode.svg)

The above state diagram was made with https://app.diagrams.net/ and can be imported by opening the SVG file in this directory there.