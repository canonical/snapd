# New in snapd 2.65:
* Support building snapd using base Core22 (Snapcraft 8.x)
* FIPS: support building FIPS complaint snapd variant that switches to FIPS mode when the system boots with FIPS enabled
* AppArmor: update to latest 4.0.2 release
* AppArmor: enable using ABI 4.0 from host parser
* AppArmor: fix parser lookup
* AppArmor: support AppArmor snippet priorities
* AppArmor: allow reading cgroup memory.max file
* AppArmor: allow using snap-exec coming from the snapd snap when starting a confined process with jailmode
* AppArmor prompting (experimental): add checks for prompting support, include prompting status in system key, and restart snapd if prompting flag changes
* AppArmor prompting (experimental): include prompt prefix in AppArmor rules if prompting is supported and enabled
* AppArmor prompting (experimental): add common types, constraints, and mappings from AppArmor permissions to abstract permissions
* AppArmor prompting (experimental): add path pattern parsing and matching
* AppArmor prompting (experimental): add path pattern precedence based on specificity
* AppArmor prompting (experimental): add packages to manage outstanding request prompts and rules
* AppArmor prompting (experimental): add prompting API and notice types, which require snap-interfaces-requests-control interface
* AppArmor prompting (experimental): feature flag can only be enabled if prompting is supported, handler service connected, and the service can be started
* Registry views (experimental): rename from aspects to registries
* Registry views (experimental): support reading registry views and setting/unsetting registry data using snapctl
* Registry views (experimental): fetch and refresh registry assertions as needed
* Registry views (experimental): restrict view paths from using a number as first character and view names to storage path style patterns
* Snap components: support installing snaps and components from files at the same time (no REST API/CLI)
* Snap components: support downloading components related assertions from the store
* Snap components: support installing components from the store
* Snap components: support removing components individually and during snap removal
* Snap components: support kernel modules as components
* Snap components: support for component install, pre-refresh and post-refresh hooks
* Snap components: initial support for building systems that contain components
* Refresh app awareness (experimental): add data field for /v2/changes REST API to allow associating each task with affected snaps
* Refresh app awareness (experimental): use the app name from .desktop file in notifications
* Refresh app awareness (experimental): give snap-refresh-observe interface access to /v2/snaps/{name} endpoint
* Improve snap-confine compatibility with nvidia drivers
* Allow re-exec when SNAP_REEXEC is set for unlisted distros to simplify testing
* Allow mixing revision and channel on snap install
* Generate GNU build ID for Go binaries
* Add missing etelpmoc.sh for shell completion
* Do not attempt to run snapd on classic when re-exec is disabled
* Packaging/build maintenance for Debian sid, Fedora, Arch, openSuse
* Add snap debug API command to enable running raw queries
* Enable snap-confine snap mount directory detection
* Replace global seccomp filter with deny rules in standard seccomp template
* Remove support for Ubuntu Core Launcher (superseded by snap-confine)
* Support creating pending serial bound users after serial assertion becomes available
* Support disabling cloud-init using kernel command-line
* In hybrid systems, apps can refresh without waiting for restarts required by essential snaps
* Ship snap-debug-info.sh script used for system diagnostics
* Improve error messages when attempting to run non-existent snap
* Switch to -u UID:GID for strace-static
* Support enabling snapd logging with snap set system debug.snapd.{log,log-level}
* Add options system.coredump.enable and system.coredump.maxuse to support using systemd-coredump on Ubuntu Core
* Provide documentation URL for 'snap interface <iface-name>'
* Fix restarting activated services instead of their activator units (i.e. sockets, timers)
* Fix potential unexpected auto-refresh of snap on managed schedule
* Fix potential segfault by guarding against kernel command-line changes on classic system
* Fix proxy entries in /etc/environment with missing newline that caused later manual entries to not be usable
* Fix offline remodelling by ignoring prerequisites that will otherwise be downloaded from store
* Fix devmode seccomp deny regression that caused spamming the log instead of actual denies
* Fix snap lock leak during refresh
* Fix not re-pinning validation sets that were already pinned when enforcing new validation sets
* Fix handling of unexpected snapd runtime failure
* Fix /v2/notices REST API skipping notices with duplicate timestamps
* Fix comparing systemd versions that may contain pre-release suffixes
* Fix udev potentially starting before snap-device-helper is made available
* Fix race in snap seed metadata loading
* Fix treating cloud-init exit status 2 as error
* Fix to prevent sending refresh complete notification if snap snap-refresh-observe interface is connected
* Fix to queue snapctl service commands if run from the default-configure hook to ensure they get up-to-date config values
* Fix stop service failure when the service is not actually running anymore
* Fix parsing /proc/PID/mounts with spaces
* Add registry interface that provides snaps access to a particular registry view
* Add snap-interfaces-requests-control interface to enable prompting client snaps
* steam-support interface: remove all AppArmor and seccomp restrictions to improve user experience
* opengl interface: improve compatibility with nvidia drivers
* home interface: autoconnect home on Ubuntu Core Desktop
* serial-port interface: support RPMsg tty
* display-control interface: allow changing LVDS backlight power and brightness
* power-control interface: support for battery charging thesholds, type/status and AC type/status
* cpu-control interface: allow CPU C-state control
* raw-usb interface: support RPi5 and Thinkpad x13s
* custom-device interface: allow device file locking
* lxd-support interface: allow LXD to self-manage its own cgroup
* network-manager interface: support MPTCP sockets
* network-control interface: allow plug/slot access to gnutls config and systemd resolved cache flushing via D-Bus
* network-control interface: allow wpa_supplicant dbus api
* gpio-control interface: support gpiochip* devices
* polkit interface: fix "rw" mount option check
* u2f-devices interface: enable additional security keys
* desktop interface: enable kde theming support

# New in snapd 2.64:
* Support building snapd using base Core22 (Snapcraft 8.x)
* FIPS: support building FIPS complaint snapd variant that switches to FIPS mode when the system boots with FIPS enabled
* AppArmor: update to AppArmor 4.0.1
* AppArmor: support AppArmor snippet priorities
* AppArmor prompting: add checks for prompting support, include prompting status in system key, and restart snapd if prompting flag changes
* AppArmor prompting: include prompt prefix in AppArmor rules if prompting is supported and enabled
* AppArmor prompting: add common types, constraints, and mappings from AppArmor permissions to abstract permissions
* AppArmor prompting: add path pattern parsing and matching
* Registry views (experimental): rename from aspects to registries
* Registry views (experimental): support reading registry views using snapctl
* Registry views (experimental): restrict view paths from using a number as first character and view names to storage path style patterns
* Snap components: support installing snaps and components from files at the same time (no REST API/CLI)
* Snap components: support downloading components related assertions from the store
* Snap components: support installing components from the store (no REST API/CLI)
* Snap components: support removing components (REST API, no CLI)
* Snap components: started support for component hooks
* Snap components: support kernel modules as components
* Refresh app awareness (experimental): add data field for /v2/changes REST API to allow associating each task with affected snaps
* Refresh app awareness (experimental): use the app name from .desktop file in notifications
* Refresh app awareness (experimental): give snap-refresh-observe interface access to /v2/snaps/{name} endpoint
* Allow re-exec when SNAP_REEXEC is set for unlisted distros to simplify testing
* Generate GNU build ID for Go binaries
* Add missing etelpmoc.sh for shell completion
* Do not attempt to run snapd on classic when re-exec is disabled
* Packaging/build maintenance for Debian sid, Fedora, Arch, openSuse
* Add snap debug api command to enable running raw queries
* Enable snap-confine snap mount directory detection
* Replace global seccomp filter with deny rules in standard seccomp template
* Remove support for Ubuntu Core Launcher (superseded by snap-confine)
* Support creating pending serial bound users after serial assertion becomes available
* Support disabling cloud-init using kernel command-line
* In hybrid systems, apps can refresh without waiting for restarts required by essential snaps
* Ship snap-debug-info.sh script used for system diagnostics
* Improve error messages when attempting to run non-existent snap
* Switch to -u UID:GID for strace-static
* Support enabling snapd logging with snap set system debug.snapd.{log,log-level}
* Fix restarting activated services instead of their activator units (i.e. sockets, timers)
* Fix potential unexpected auto-refresh of snap on managed schedule
* Fix potential segfault by guarding against kernel command-line changes on classic system
* Fix proxy entries in /etc/environment with missing newline that caused later manual entries to not be usable
* Fix offline remodelling by ignoring prerequisites that will otherwise be downloaded from store
* Fix devmode seccomp deny regression that caused spamming the log instead of actual denies
* Fix snap lock leak during refresh
* Fix not re-pinning validation sets that were already pinned when enforcing new validation sets
* Fix handling of unexpected snapd runtime failure
* Fix /v2/notices REST API skipping notices with duplicate timestamps
* Fix comparing systemd versions that may contain pre-release suffixes
* Fix udev potentially starting before snap-device-helper is made available
* Fix race in snap seed metadata loading
* Fix treating cloud-init exit status 2 as error
* Fix to prevent sending refresh complete notification if snap snap-refresh-observe interface is connected
* Fix to queue snapctl service commands if run from the default-configure hook to ensure they get up-to-date config values
* Fix stop service failure when the service is not actually running anymore
* Add registry interface that provides snaps access to a particular registry view
* steam-support interface: relaxed AppArmor and seccomp restrictions to improve user experience
* home interface: autoconnect home on Ubuntu Core Desktop
* serial-port interface: support RPMsg tty
* display-control interface: allow changing LVDS backlight power and brightness
* power-control interface: support for battery charging thesholds, type/status and AC type/status
* cpu-control interface: allow CPU C-state control
* raw-usb interface: support RPi5 and Thinkpad x13s
* custom-device interface: allow device file locking
* lxd-support interface: allow LXD to self-manage its own cgroup
* network-manager interface: support MPTCP sockets
* network-control interface: allow plug/slot access to gnutls config and systemd resolved cache flushing via D-Bus

# New in snapd 2.63:
* Support for snap services to show the current status of user services (experimental)
* Refresh app awareness: record snap-run-inhibit notice when starting app from snap that is busy with refresh (experimental)
* Refresh app awareness: use warnings as fallback for desktop notifications (experimental)
* Aspect based configuration: make request fields in the aspect-bundle's rules optional (experimental)
* Aspect based configuration: make map keys conform to the same format as path sub-keys (experimental)
* Aspect based configuration: make unset and set behaviour similar to configuration options (experimental)
* Aspect based configuration: limit nesting level for setting value (experimental)
* Components: use symlinks to point active snap component revisions
* Components: add model assertion support for components
* Components: fix to ensure local component installation always gets a new revision number
* Add basic support for a CIFS remote filesystem-based home directory
* Add support for AppArmor profile kill mode to avoid snap-confine error
* Allow more than one interface to grant access to the same API endpoint or notice type
* Allow all snapd service's control group processes to send systemd notifications to prevent warnings flooding the log
* Enable not preseeded single boot install
* Update secboot to handle new sbatlevel
* Fix to not use cgroup for non-strict confined snaps (devmode, classic)
* Fix two race conditions relating to freedesktop notifications
* Fix missing tunables in snap-update-ns AppArmor template
* Fix rejection of snapd snap udev command line by older host snap-device-helper
* Rework seccomp allow/deny list
* Clean up files removed by gadgets
* Remove non-viable boot chains to avoid secboot failure
* posix_mq interface: add support for missing time64 mqueue syscalls mq_timedreceive_time64 and mq_timedsend_time64
* password-manager-service interface: allow kwalletd version 6
* kubernetes-support interface: allow SOCK_SEQPACKET sockets
* system-observe interface: allow listing systemd units and their properties
* opengl interface: enable use of nvidia container toolkit CDI config generation

# New in snapd 2.62:
* Aspects based configuration schema support (experimental)
* Refresh app awareness support for UI (experimental)
* Support for user daemons by introducing new control switches --user/--system/--users for service start/stop/restart (experimental)
* Add AppArmor prompting experimental flag (feature currently unsupported)
* Installation of local snap components of type test
* Packaging of components with snap pack
* Expose experimental features supported/enabled in snapd REST API endpoint /v2/system-info
* Support creating and removing recovery systems for use by factory reset
* Enable API route for creating and removing recovery systems using /v2/systems with action create and /v2/systems/{label} with action remove
* Lift requirements for fde-setup hook for single boot install
* Enable single reboot gadget update for UC20+
* Allow core to be removed on classic systems
* Support for remodeling on hybrid systems
* Install desktop files on Ubuntu Core and update after snapd upgrade
* Upgrade sandbox features to account for cgroup v2 device filtering
* Support snaps to manage their own cgroups
* Add support for AppArmor 4.0 unconfined profile mode
* Add AppArmor based read access to /etc/default/keyboard
* Upgrade to squashfuse 0.5.0
* Support useradd utility to enable removing Perl dependency for UC24+
* Support for recovery-chooser to use console-conf snap
* Add support for --uid/--gid using strace-static
* Add support for notices (from pebble) and expose via the snapd REST API endpoints /v2/notices and /v2/notice
* Add polkit authentication for snapd REST API endpoints /v2/snaps/{snap}/conf and /v2/apps
* Add refresh-inhibit field to snapd REST API endpoint /v2/snaps
* Add refresh-inhibited select query to REST API endpoint /v2/snaps
* Take into account validation sets during remodeling
* Improve offline remodeling to use installed revisions of snaps to fulfill the remodel revision requirement
* Add rpi configuration option sdtv_mode
* When snapd snap is not installed, pin policy ABI to 4.0 or 3.0 if present on host
* Fix gadget zero-sized disk mapping caused by not ignoring zero sized storage traits
* Fix gadget install case where size of existing partition was not correctly taken into account
* Fix trying to unmount early kernel mount if it does not exist
* Fix restarting mount units on snapd start
* Fix call to udev in preseed mode
* Fix to ensure always setting up the device cgroup for base bare and core24+
* Fix not copying data from newly set homedirs on revision change
* Fix leaving behind empty snap home directories after snap is removed (resulting in broken symlink)
* Fix to avoid using libzstd from host by adding to snapd snap
* Fix autorefresh to correctly handle forever refresh hold
* Fix username regex allowed for system-user assertion to not allow '+'
* Fix incorrect application icon for notification after autorefresh completion
* Fix to restart mount units when changed
* Fix to support AppArmor running under incus
* Fix case of snap-update-ns dropping synthetic mounts due to failure to match  desired mount dependencies
* Fix parsing of base snap version to enable pre-seeding of Ubuntu Core Desktop
* Fix packaging and tests for various distributions
* Add remoteproc interface to allow developers to interact with Remote Processor Framework which enables snaps to load firmware to ARM Cortex microcontrollers
* Add kernel-control interface to enable controlling the kernel firmware search path
* Add nfs-mount interface to allow mounting of NFS shares
* Add ros-opt-data interface to allow snaps to access the host /opt/ros/ paths
* Add snap-refresh-observe interface that provides refresh-app-awareness clients access to relevant snapd API endpoints
* steam-support interface: generalize Pressure Vessel root paths and allow access to driver information, features and container versions
* steam-support interface: make implicit on Ubuntu Core Desktop
* desktop interface: improved support for Ubuntu Core Desktop and limit autoconnection to implicit slots
* cups-control interface: make autoconnect depend on presence of cupsd on host to ensure it works on classic systems
* opengl interface: allow read access to /usr/share/nvidia
* personal-files interface: extend to support automatic creation of missing parent directories in write paths
* network-control interface: allow creating /run/resolveconf
* network-setup-control and network-setup-observe interfaces: allow busctl bind as required for systemd 254+
* libvirt interface: allow r/w access to /run/libvirt/libvirt-sock-ro and read access to /var/lib/libvirt/dnsmasq/**
* fwupd interface: allow access to IMPI devices (including locking of device nodes), sysfs attributes needed by amdgpu and the COD capsule update directory
* uio interface: allow configuring UIO drivers from userspace libraries
* serial-port interface: add support for NXP Layerscape SoC
* lxd-support interface: add attribute enable-unconfined-mode to require LXD to opt-in to run unconfined
* block-devices interface: add support for ZFS volumes
* system-packages-doc interface: add support for reading jquery and sphinx documentation
* system-packages-doc interface: workaround to prevent autoconnect failure for snaps using base bare
* microceph-support interface: allow more types of block devices to be added as an OSD
* mount-observe interface: allow read access to /proc/{pid}/task/{tid}/mounts and proc/{pid}/task/{tid}/mountinfo
* polkit interface: changed to not be implicit on core because installing policy files is not possible
* upower-observe interface: allow stats refresh
* gpg-public-keys interface: allow creating lock file for certain gpg operations
* shutdown interface: allow access to SetRebootParameter method
* media-control interface: allow device file locking
* u2f-devices interface: support for Trustkey G310H, JaCarta U2F, Kensington VeriMark Guard, RSA DS100, Google Titan v2

# New in snapd 2.61.3:
* Install systemd files in correct location for 24.04

# New in snapd 2.61.2:
* Fix to enable plug/slot sanitization for prepare-image
* Fix panic when device-service.access=offline
* Support offline remodeling
* Allow offline update only remodels without serial
* Fail early when remodeling to old model revision
* Fix to enable plug/slot sanitization for validate-seed
* Allow removal of core snap on classic systems
* Fix network-control interface denial for file lock on /run/netns
* Add well-known core24 snap-id
* Fix remodel snap installation order
* Prevent remodeling from UC18+ to UC16
* Fix cups auto-connect on classic with cups snap installed
* u2f-devices interface support for GoTrust Idem Key with USB-C
* Fix to restore services after unlink failure
* Add libcudnn.so to Nvidia libraries
* Fix skipping base snap download due to false snapd downgrade conflict

# New in snapd 2.61.1:
* Stop requiring default provider snaps on image building and first boot if alternative providers are included and available
* Fix auth.json access for login as non-root group ID
* Fix incorrect remodelling conflict when changing track to older snapd version
* Improved check-rerefresh message
* Fix UC16/18 kernel/gadget update failure due volume mismatch with installed disk
* Stop auto-import of assertions during install modes
* Desktop interface exposes GetIdletime
* Polkit interface support for new polkit versions
* Fix not applying snapd snap changes in tracked channel when remodelling

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
