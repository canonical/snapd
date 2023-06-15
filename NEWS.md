# New in snapd 2.60:
* Support for dynamic snapshot data exclusions
* Apparmor userspace is vendored inside the snapd snap
* Added a default-configure hook that exposes gadget default configuration
  options to snaps during first install before services are started
* Allow install from initrd to speed up the initial installation for
  systems that do not have a install-device hook
* New `snap sign --chain` flag that appends the account and account-key
  assertions
* Support validation-sets in the model assertion
* Support new "min-size" field in gadget.yaml
* New interface: "userns"

# New in snapd 2.59.5:
* Explicitly disallow the use of ioctl + TIOCLINUX
  This fixes CVE-2023-1523.

# New in snapd 2.59.4:
* Retry when looking for disk label on non-UEFI systems
* Fix remodel from UC20 to UC22

# New in snapd 2.59.3:
* Fix quiet boot
* Ignore case for vfat paritions when validating
* Restart always enabled units

# New in snapd 2.59.2:
* Notify users when a user triggered auto refresh finished

# New in snapd 2.59.1:

* Add udev rules from steam-devices to steam-support interface
* Bugfixes for layout path checking, dm_crypt permissions,
  mount-control interface parameter checking, kernel commandline
  parsing, docker-support, refresh-app-awareness

# New in snapd 2.59:

* Support setting extra kernel command line parameters via snap
  configuration and under a gadget allow-list
* Support for Full-Disk-Encryption using ICE
* Support for arbitrary home dir locations via snap configuration
* New nvidia-drivers-support interface
* Support for udisks2 snap
* Pre-download of snaps ready for refresh and automatic refresh of the
  snap when all apps are closed
* New microovn interface
* Support uboot with `CONFIG_SYS_REDUNDAND_ENV=n`
* Make "snap-preseed --reset" re-exec when needed
* Update the fwupd interface to support fully confined fwupd
* The memory,cpu,thread quota options are no longer experimental
* Support debugging snap client requests via the `SNAPD_CLIENT_DEBUG_HTTP`
  environment variable
* Support ssh listen-address via snap configuration
* Support for quotas on single services
* prepare-image now takes into account snapd versions going into the image,
  including in the kernel initrd, to fetch supported assertion formats
