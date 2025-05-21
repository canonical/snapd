# New in snapd 2.68.5
* LP: #2109843 fix missing preseed files when running in a container

# New in snapd 2.68.4
* Snap components: LP: #2104933 workaround for classic 24.04/24.10 models that incorrectly specify core22 instead of core24
* Update build dependencies

# New in snapd 2.68.3
* FDE: LP: #2101834 snapd 2.68+ and snap-bootstrap <2.68 fallback to old keyring path
* Fix Plucky snapd deb build issue related to /var/lib/snapd/void permissions
* Fix snapd deb build complaint about ifneq with extra bracket

# New in snapd 2.68.2
* FDE: use boot mode for FDE hooks
* FDE: add snap-bootstrap compatibility check to prevent image creation with incompatible snapd and kernel snap
* FDE: add argon2 out-of-process KDF support
* FDE: have separate mutex for the sections writing a fresh modeenv
* FDE: LP: #2099709 update secboot to e07f4ae48e98
* Confdb: support pruning ephemeral data and process alternative types in order
* core-initrd: look at env to mount directly to /sysroot
* core-initrd: prepare for Plucky build and split out 24.10 (Oracular)
* Fix missing primed packages in snapd snap manifest
* Interfaces: posix-mq | fix incorrect clobbering of global variable and make interface more precise
* Interfaces: opengl | add more kernel fusion driver files

# New in snapd 2.68.1
* Fix snap-confine type specifier type mismatch on armhf

# New in snapd 2.68
* FDE: add support for new and more extensible key format that is unified between TPM and FDE hook
* FDE: add support for adding passphrases during installation
* FDE: update secboot to 30317622bbbc
* Snap components: make kernel components available on firstboot after either initramfs or ephemeral rootfs style install
* Snap components: mount drivers tree from initramfs so kernel modules are available in early boot stages
* Snap components: support remodeling to models that contain components
* Snap components: support offline remodeling to models that contain components
* Snap components: support creating new recovery systems with components
* Snap components: support downloading components with 'snap download' command
* Snap components: support sideloading asserted components
* AppArmor Prompting(experimental): improve version checks and handling of listener notification protocol for communication with kernel AppArmor
* AppArmor Prompting(experimental): make prompt replies idempotent, and have at most one rule for any given path pattern, with potentially mixed outcomes and lifespans
* AppArmor Prompting(experimental): timeout unresolved prompts after a period of client inactivity
* AppArmor Prompting(experimental): return an error if a patch request to the API would result in a rule without any permissions
* AppArmor Prompting(experimental): warn if there is no prompting client present but prompting is enabled, or if a prompting-related error occurs during snapd startup
* AppArmor Prompting(experimental): do not log error when converting empty permissions to AppArmor permissions
* Confdb(experimental): rename registries to confdbs (including API /v2/registries => /v2/confdb)
* Confdb(experimental): support marking confdb schemas as ephemeral
* Confdb(experimental): add confdb-control assertion and feature flag
* Refresh App Awareness(experimental): LP: #2089195 prevent possibility of incorrect notification that snap will quit and update
* Confidential VMs: snap-bootstrap support for loading partition information from a manifest file for cloudimg-rootfs mode
* Confidential VMs: snap-bootstrap support for setting up cloudimg-rootfs as an overlayfs with integrity protection
* dm-verity for essential snaps: add support for snap-integrity assertion
* Interfaces: modify AppArmor template to allow owner read on @{PROC}/@{pid}/fdinfo/*
* Interfaces: LP: #2072987 modify AppArmor template to allow using setpriv to run daemon as non-root user
* Interfaces: add configfiles backend that ensures the state of configuration files in the filesystem
* Interfaces: add ldconfig backend that exposes libraries coming from snaps to either the rootfs or to other snaps
* Interfaces: LP: #1712808 LP: 1865503 disable udev backend when inside a container
* Interfaces: add auditd-support interface that grants audit_control capability and required paths for auditd to function
* Interfaces: add checkbox-support interface that allows unrestricted access to all devices
* Interfaces: fwupd | allow access to dell bios recovery
* Interfaces: fwupd | allow access to shim and fallback shim
* Interfaces: mount-control | add mount option validator to detect mount option conflicts early
* Interfaces: cpu-control | add read access to /sys/kernel/irq/<IRQ>
* Interfaces: locale-control | changed to be implicit on Ubuntu Core Desktop
* Interfaces: microstack-support | support for utilizing of AMD SEV capabilities
* Interfaces: u2f | added missing OneSpan device product IDs
* Interfaces: auditd-support | grant seccomp setpriority
* Interfaces: opengl interface | enable parsing of nvidia driver information files
* Allow mksquashfs 'xattrs' when packing snap types os, core, base and snapd as part of work to support non-root snap-confine
* Upstream/downstream packaging changes and build updates
* Improve error logs for malformed desktop files to also show which desktop file is at fault
* Provide more precise error message when overriding channels with grade during seed creation
* Expose 'snap prepare-image' validation parameter
* Add snap-seccomp 'dump' command that dumps the filter rules from a compiled profile
* Add fallback release info location /etc/initrd-release
* Added core-initrd to snapd repo and fixed issues with ubuntu-core-initramfs deb builds
* Remove stale robust-mount-namespace-updates experimental feature flag
* Remove snapd-snap experimental feature (rejected) and it's feature flag
* Changed snap-bootstrap to mount base directly on /sysroot
* Mount ubuntu-seed mounted as no-{suid,exec,dev}
* Mapping volumes to disks: add support for volume-assignments in gadget
* Fix silently broken binaries produced by distro patchelf 0.14.3 by using locally build patchelf 0.18
* Fix mismatch between listed refresh candidates and actual refresh due to outdated validation sets
* Fix 'snap get' to produce compact listing for tty
* Fix missing store-url by keeping it as part of auxiliary store info
* Fix snap-confine attempting to retrieve device cgroup setup inside container where it is not available
* Fix 'snap set' and 'snap get' panic on empty strings with early error checking
* Fix logger debug entries to show correct caller and file information
* Fix issue preventing hybrid systems from being seeded on first boot
* LP: #1966203 remove auto-import udev rules not required by deb package to avoid unwanted syslog errors
* LP: #1886414 fix progress reporting when stdout is on a tty, but stdin is not

# New in snapd 2.67.1
* Fix apparmor permissions to allow snaps access to kernel modules and firmware on UC24, which also fixes the kernel-modules-control interface on UC24
* AppArmor prompting (experimental): disallow /./ and /../ in path patterns
* Fix 'snap run' getent based user lookup in case of bad PATH
* Fix snapd using the incorrect AppArmor version during undo of an refresh for regenerating snap profiles
* Add new syscalls to base templates
* hardware-observe interface: allow riscv_hwprobe syscall
* mount-observe interface: allow listmount and statmount syscalls

# New in snapd 2.67
* AppArmor prompting (experimental): allow overlapping rules
* Registry view (experimental): Changes to registry data (from both users and snaps) can be validated and saved by custodian snaps
* Registry view (experimental): Support 'snapctl get --pristine' to read the registry data excluding staged transaction changes
* Registry view (experimental): Put registry commands behind experimental feature flag
* Components: Make modules shipped/created by kernel-modules components available right after reboot
* Components: Add tab completion for local component files
* Components: Allow installing snaps and components from local files jointly on the CLI
* Components: Allow 'snapctl model' command for gadget and kernel snaps
* Components: Add 'snap components' command
* Components: Bug fixes
* eMMC gadget updates (WIP): add syntax support in gadget.yaml for eMMC schema
* Support for ephemeral recovery mode on hybrid systems
* Support for dm-verity options in snap-bootstrap
* Support for overlayfs options and allow empty what argument for tmpfs
* Enable ubuntu-image to determine the size of the disk image to create
* Expose 'snap debug' commands 'validate-seed' and 'seeding'
* Add debug API option to use dedicated snap socket /run/snapd-snap.socket
* Hide experimental features that are no longer required (accepted/rejected)
* Mount ubuntu-save partition with no{exec,dev,suid} at install, run and factory-reset
* Improve memory controller support with cgroup v2
* Support ssh socket activation configurations (used by ubuntu 22.10+)
* Fix generation of AppArmor profile with incorrect revision during multi snap refresh
* Fix refresh app awareness related deadlock edge case
* Fix not caching delta updated snap download
* Fix passing non root uid, guid to initial tmpfs mount
* Fix ignoring snaps in try mode when amending
* Fix reloading of service activation units to avoid systemd errors
* Fix snapd snap FIPS build on Launchpad to use Advantage Pro FIPS updates PPA
* Make killing of snap apps best effort to avoid possibility of malicious failure loop
* Alleviate impact of auto-refresh failure loop with progressive delay
* Dropped timedatex in selinux-policy to avoid runtime issue
* Fix missing syscalls in seccomp profile
* Modify AppArmor template to allow using SNAP_REEXEC on arch systems
* Modify AppArmor template to allow using vim.tiny (available in base snaps)
* Modify AppArmor template to add read-access to debian_version
* Modify AppArmor template to allow owner to read @{PROC}/@{pid}/sessionid
* {common,personal,system}-files interface: prohibit trailing @ in filepaths
* {desktop,shutdown,system-observe,upower-observe} interface: improve for Ubuntu Core Desktop
* custom-device interface: allow @ in custom-device filepaths
* desktop interface: improve launch entry and systray integration with session
* desktop-legacy interface: allow DBus access to com.canonical.dbusmenu
* fwupd interface: allow access to nvmem for thunderbolt plugin
* mpris interface: add plasmashell as label
* mount-control interface: add support for nfs mounts
* network-{control,manager} interface: add missing dbus link rules
* network-manager-observe interface: add getDevices methods
* opengl interface: add Kernel Fusion Driver access to opengl
* screen-inhibit-control interface: improve screen inhibit control for use on core
* udisks2 interface: allow ping of the UDisks2 service
* u2f-devices interface: add Nitrokey Passkey

# New in snapd 2.66.1:
* AppArmor prompting (experimental): Fix kernel prompting support check
* Allow kernel snaps to have content slots
* Fix ignoring snaps in try mode when amending

# New in snapd 2.66:
* AppArmor prompting (experimental): expand kernel support checks
* AppArmor prompting (experimental): consolidate error messages and add error kinds
* AppArmor prompting (experimental): grant /v2/snaps/{name} via snap-interfaces-requests-control
* AppArmor prompting (experimental): add checks for duplicate pattern variants
* Registry views (experimental): add handlers that commit (and cleanup) registry transactions
* Registry views (experimental): add a snapctl fail command for rejecting registry transactions
* Registry views (experimental): allow custodian snaps to implement registry hooks that modify and save registry data
* Registry views (experimental): run view-changed hooks only for snaps plugging views affected by modified paths
* Registry views (experimental): make registry transactions serialisable
* Snap components: handle refreshing components to revisions that have been on the system before
* Snap components: enable creating Ubuntu Core images that contain components
* Snap components: handle refreshing components independently of snaps
* Snap components: handle removing components when refreshing a snap that no longer defines them
* Snap components: extend snapd Ubuntu Core installation API to allow for picking optional snaps and components to install
* Snap components: extend kernel.yaml with "dynamic-modules", allowing kernel to define a location for kmods from component hooks
* Snap components: renamed component type "test" to "standard"
* Desktop IDs: support installing desktop files with custom names based on desktop-file-ids desktop interface plug attr
* Auto-install snapd on classic systems as prerequisite for any non-essential snap install
* Support loading AppArmor profiles on WSL2 with non-default kernel and securityfs mounted
* Debian/Fedora packaging updates
* Add snap debug command for investigating execution aspects of the snap toolchain
* Improve snap pack error for easier parsing
* Add support for user services when refreshing snaps
* Add snap remove --terminate flag for terminating running snap processes
* Support building FIPS complaint snapd deb and snap
* Fix to not use nss when looking up for users/groups from snapd snap
* Fix ordering in which layout changes are saved
* Patch snapd snap dynamic linker to ignore LD_LIBRARY_PATH and related variables
* Fix libexec dir for openSUSE Slowroll
* Fix handling of the shared snap directory for parallel installs
* Allow writing to /run/systemd/journal/dev-log by default
* Avoid state lock during snap removal to avoid delaying other snapd operations
* Add nomad-support interface to enable running Hashicorp Nomad
* Add intel-qat interface
* u2f-devices interface: add u2f trustkey t120 product id and fx series fido u2f devices
* desktop interface: improve integration with xdg-desktop-portal
* desktop interface: add desktop-file-ids plug attr to desktop interface
* unity7 interface: support desktop-file-ids in desktop files rule generation
* desktop-legacy interface: support desktop-file-ids in desktop files rule generation
* desktop-legacy interface: grant access to gcin socket location
* login-session-observe interface: allow introspection
* custom-device interface: allow to explicitly identify matching device in udev tagging block
* system-packages-doc interface: allow reading /usr/share/javascript
* modem-manager interface: add new format of WWAN ports
* pcscd interface: allow pcscd to read opensc.conf
* cpu-control interface: add IRQ affinity control to cpu_control
* opengl interface: add support for cuda workloads on Tegra iGPU in opengl interface

# New in snapd 2.65.3:
* Fix missing aux info from store on snap setup

# New in snapd 2.65.2:
* Bump squashfuse from version 0.5.0 to 0.5.2 (used in snapd deb only)

# New in snapd 2.65.1:
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
* Fix snapd riscv64 build
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
