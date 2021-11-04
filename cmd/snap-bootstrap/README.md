# _snap-bootstrap_

Welcome to the world of the initramfs of UC20! 

## Short intro

_snap-bootstrap_ is the main executable that is run during the initramfs stage of UC20. TLDR; it has several responsibilities:

1. Mounting some partitions from the disk that UC20 is installed to. This includes ubuntu-data, ubuntu-boot, ubuntu-seed, and if present, ubuntu-save (ubuntu-save is optional on unencrypted devices).
1. As part of mounting those partitions, _snap-bootstrap_ may perform the necessary steps to unlock any encrypted partitions such as ubuntu-data and ubuntu-save. 
1. After unlocking and mounting all such partitions, _snap-bootstrap_ then chooses which base snap file is to be used for the root filesystem of userspace (as the root filesystem of the initramfs is just a static set of files built into the initramfs and is not the final root filesystem), and mounts this base snap file.
1. _snap-bootstrap_ then chooses which kernel snap file is to be used to mount and find additional kernel modules that are not compiled into the kernel or shipped as modules inside the initramfs or otherwise loaded as DTBs, etc.
1. _snap-bootstrap_ then also will mount the ubuntu-data partition such that either the writable components of the root filesystem come from this actual partition, or if the mode the system is booting into is an ephemeral system such as install or recover, will mount a temporary filesystem for this.
1. _snap-bootstrap_ on kernel and base snap upgrades will also handle updating bootloader environment variables to implement A/B or try-boot functionality.
1. _snap-bootstrap_ then finally may do some additional setup of the root filesystem such as copying some default files for ephemeral system modes such as recover. 

## In depth walkthrough

_snap-bootstrap_ operates differently depending on snapd_recovery_mode, so each mode is considered separately below.

Note however that snap-bootstrap, while containing the largest chunk of the logic for the initramfs, is not alone; there is other machinery at work in the rest of the initramfs that takes over after snap-bootstrap has exited successfully. Thus, there are more steps to fully complete the initramfs operations beyond these steps, and ideally at some point the rest of those steps will be moved into snap-bootstrap so they can be more fully tested and documented, but for now, just see the unit files in the initrd for "the-modeenv" and "the-tool" to follow what happens after snap-bootstrap is done.

Additionally, note that in all modes where encryption is enabled, we may open/unlock the TPM keys (or other security hardware in the case of custom ARM fde-hooks for example) in order to generally do encryption related things and regardless of result, we must _always_ re-seal and re-lock the TPM (or other security hardware) before exiting snap-bootstrap to protect the sealed-keys. This is implemented specifically with `secboot.LockSealedKeys`.

### Install mode

Install mode has the following steps:

1. The first step of the initramfs-mounts command is always to measure the "epoch" of the secboot version that snap-bootstrap is compiled with to the TPM (if one exists). This is for maximum security and to prevent a newer epoch of secboot from being vulnerable to prior versions.
1. The next step is to pick the first partition to mount as securely as possible, in the case of EFI systems, we will query an EFI variable to indicate the Partition UUID of the disk which the kernel was booted off of, and we use that Partition UUID to identify the partition which should be mounted as ubuntu-seed (since on grub amd64 systems, the kernel is initially booted by mounting the squashfs in grub and then booting the kernel.efi inside the mounted squashfs). If there is no such EFI variable, we fall back to just using the label instead to choose which partition to mount. Although we do have snap-bootstrap ordered to run after udev has fully settled via `After=systemd-udev-settle.service` in the unit file, sometimes we still don't have that Partition UUID device node available in /dev/ by the time we are executing, so we wait in a loop for the device node to appear before giving up.
1. After having identified which partition is ubuntu-seed, we mount it at /run/mnt/ubuntu-seed.
1. Next, we will load the "recovery system seed", which is the set of snaps associated with this recovery system, this includes the base snap, the kernel snap, the snapd snap and the gadget snap. These snaps are verified to match their assertions via hashing.
1. Next we do another measurement to the TPM (if available) of the model assertion from the recovery system we loaded.
1. After having verified that the recovery system seed snaps are valid and that the model assertion is correct, we will then mount these snaps at /run/mnt/base, /run/mnt/kernel, and /run/mnt/snapd (the gadget is not mounted at this time).
1. Next, we create a tmpfs mount at /run/mnt/data, which will be the root filesystem we pivot_root into at the end of the initramfs. 
1. Next, we will "configure" the target system root filesystem using the gadget snap itself, this will handle things like "early snap config" and cloud-init config, etc. that needs to be applied before we fully boot to userspace.
1. Next, we will write out a modeenv file to the root filesystem based on the model assertion and the recovery system seed snaps that will be read by snapd in userspace when we get there.
1. Finally, the last step of all modes is to expose the boot flags that were put into the boot environment for userspace to measure. This is done via `boot.InitramfsExposeBootFlagsForSystem`

### Run mode

1. The first step of the initramfs-mounts command is always to measure the "epoch" of the secboot version that snap-bootstrap is compiled with to the TPM (if one exists). This is for maximum security and to prevent a newer epoch of secboot from being vulnerable to prior versions.
1. The next step is to pick the first partition to mount as securely as possible, in the case of EFI systems, we will query an EFI variable to indicate the Partition UUID of the disk which the kernel was booted off of, and we use that Partition UUID to identify the partition which should be mounted as ubuntu-boot. This is because in run mode (for amd64 grub systems at least), we will boot using the kernel.efi file from the ubuntu-boot partition, as opposed to recover and install modes which use the kernel snap from ubuntu-seed. If there is no such EFI variable, we fall back to just using the label instead to choose which partition to mount. Although we do have snap-bootstrap ordered to run after udev has fully settled via `After=systemd-udev-settle.service` in the unit file, sometimes we still don't have that Partition UUID device node available in /dev/ by the time we are executing, so we wait in a loop for the device node to appear before giving up.
1. After having identified which partition is ubuntu-boot, we mount it at /run/mnt/ubuntu-boot.
1. Using the disk we found ubuntu-boot on as a reference, we will pick the partition with label "ubuntu-seed" and mount this partition at /run/mnt/ubuntu-seed.
1. Next we will measure the model assertion to the TPM as well.
1. Next, we will try to unlock the ubuntu-data partition (if it is encrypted) using the sealed-key which exists on ubuntu-boot. After unlocking (or just finding the unencrypted version if encryption is not being used), we will mount it at /run/mnt/data.
1. If ubuntu-data was encrypted, then we will proceed to attempt to unlock an ubuntu-save partition from the same disk, and mount it at /run/mnt/ubuntu-save. If ubuntu-data was not encrypted, then we will try to mount an unencrypted ubuntu-save at /run/mnt/ubuntu-save, but in the unencrypted case we do not require ubuntu-save to be present so it is not a fatal error if we do not find ubuntu-save in the unencrypted case.
1. After having mounted all of the relevant partitions, we will perform a double check that the mount points /run/mnt/ubuntu-{save,data} come from the same disk. For extra paranoia, we will also validate that ubuntu-data and ubuntu-save, if they were encrypted, were unlocked with the same key pairing.
1. Next we read the modeenv from the data partition, and based on the modeenv, we decide what snaps to mount. On all boots into run mode the base and kernel snap must be identified and mounted. Note that for run mode, we find the snaps to mount for this purpose through `boot.InitramfsRunModeSelectSnapsToMount` which handles kernel / base snap updates and will return the "try" snap if there is a new snap being tried on this boot.
1. If this boot is the first boot into run mode ever, then we will also mount the snapd snap by reading and validating the recovery system seed from ubuntu-seed and mounting the snapd snap at /run/mnt/snap.
1. Finally, the last step of all modes is to expose the boot flags that were put into the boot environment for userspace to measure. This is done via `boot.InitramfsExposeBootFlagsForSystem`

### Recover mode

The first 8 steps for recover mode are shared exactly with install mode, so they are not repeated here, but see the steps 1-8 for install mode, then we continue:

9. The next thing we check is whether we are inside the recovery environment to actually do recover mode, or if we are simply validating that the recovery system we are booting into is valid. We do this by inspecting bootloader environment variables via `boot.InitramfsIsTryingRecoverySystem`. 
10. In the case that we are trying a recovery system, we will ensure that the next reboot will take transition us back to run mode, and additionally, if we are in an inconsistent state, where for example things are not agreeing about the state of this recovery system that was to be tried, then we will reboot and try to go back to run mode and give up on recover mode.
11. If we are either not trying a recovery system or we are in a consistent state and are trying a recovery system, then we enter the following magical state machine. This state diagram essentially allows recover mode to be extra robust against failure modes like having a partition disappear, some keys not being able to unlock some of the partitions, etc. and is referred to as "degraded mode", specifically if we don't use all the happy paths then we are in a "degraded" recover mode as opposed to being in a normal recover mode.


![](/cmd/snap-bootstrap/degraded-recover-mode.svg)


The above state diagram was made with https://app.diagrams.net/ and can be imported by opening the SVG file in this directory there.

12. After exiting the state machine (in all cases), we will then again consider if we are trying a recovery system, and if we are we will inspect if the state machine degraded at all (meaning that the "happy path" for unlocking disks and mounting partitions was not fully executed and we had to use an alternative option at least one time), and if the state machine outputs a degraded state, then we mark the recovery system as a failure and go back to run mode. If it was successful, we mark it as successful and then reboot to run mode as well.
13. In the case where we were not trying a recovery system and we were in fact just booting into the recovery system for normal recover mode, we will first write out a file `degraded.json` with information about whether or not the state machine output us in a degraded state or not. This may affect some choices userspace makes when we get there.
14. If the state machine exited in a state that was at least sufficiently usable such that we can trust the data partition unlocked and mounted, we will then copy some files from the data partition to our tmpfs root filesystem, such as authentication files like ssh keys, networking configuration, and other miscellaneous files like the clock sync file in case the system does not have a battery powered RTC. If we didn't trust the data partition, then "safe" defaults will be used instead. This is to prevent a situation wherein we don't "trust" the data partition enough (but perhaps we did trust ubuntu-save when unlocking it) to copy authentication files over, but then we leave console-conf in such a state where it could allow an attacker to create their own new account and then exfiltrate secret data from the trusted ubuntu-save.
15. Next, we will write out a modeenv file to the root filesystem based on the model assertion and the recovery system seed snaps that will be read by snapd in userspace when we get there.
16. Penultimately, we will ensure that if the system is rebooted at all after this point, the system will be automatically transitioned back to run mode without further input.
17. Finally, the last step of all modes is to expose the boot flags that were put into the boot environment for userspace to measure. This is done via `boot.InitramfsExposeBootFlagsForSystem`

### Classic mode

This mode may eventually be developed to support using the same initramfs + kernel on Ubuntu Classic (i.e. Server or Desktop) as is currently used on Ubuntu Core 20+. This is not yet fully fleshed out however.
