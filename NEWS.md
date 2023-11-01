# New in snapd 2.61:
* Fix control of activated services in 'snap start' and 'snap stop'
* Correctly reflect activated services in 'snap services'
* Disabled services are no longer enabled again when snap is refreshed
* interfaces/builtin: added support for Token2 U2F keys
* interfaces/u2f-devices: add Swissbit iShield Key
* interfaces/builtin: update gpio apparmor to match pattern that contains multiple subdirectories under /sys/devices/platform
* interfaces: add a polkit-agent interface
* interfaces: add pcscd interface
* Kernel command-line can now be edited in the gadget.yaml
* Only track validation-sets in run-mode, fixes validation-set issues on first boot.
* Added support for using store.access to disable access to snap store
* Support for fat16 partition in gadget
* Pre-seed authority delegation is now possible
* Support new system-user name  _daemon_
* Several bug fixes and improvements around remodelling
* Offline remodelling support

# New in snapd 2.60.4:
* Switch to plug/slot in the "qualcomm-ipc-router" interface
  but keeping backward compatibility
* Fix "custom-device" udev KERNEL values
* Allow firmware-updater snap to install user-daemons
* Allow loopback as a block device

# NEW in snapd 2.60.3:
* Fix bug in the "private" plug attribute of the shared-memory
  interface that can result in a crash when upgrading from an
  old version of snapd.
* Fix missing integration of the /etc/apparmor.d/tunables/home.d/
  apparmor to support non-standard home directories

# New in snapd 2.60.2:
* Performance improvements for apparmor_parser to compensate for
  the slower `-O expr-simplify` default used. This should bring
  the performance back to the 2.60 level and even increase it
  for many use-cases.
* Bugfixes

# New in snapd 2.60.1:
* Bugfixes
* Use "aes-cbc-essiv:sha256" in cryptsetup on arm 32bit devices
  to increase speed on devices with CAAM support
* Stop using `-O no-expr-simplify` in apparmor_parser to avoid
  potential exponential memory use. This can lead to slower 
  policy complication in some cases but it is much safer on
  low memory devices.

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
