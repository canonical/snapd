// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package apparmor

// defaultTemplate contains default apparmor template.
//
// It can be overridden for testing using MockTemplate().
//
// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/templates/ubuntu-core/16.04/default
var defaultTemplate = `
# Description: Allows access to app-specific directories and basic runtime
# Usage: common

# vim:syntax=apparmor

#include <tunables/global>

###VAR###

###PROFILEATTACH### (attach_disconnected,mediate_deleted) {
  #include <abstractions/base>
  #include <abstractions/consoles>
  #include <abstractions/openssl>

  # While in later versions of the base abstraction, include this explicitly
  # for series 16 and cross-distro
  /etc/ld.so.preload r,

  # The base abstraction doesn't yet have this
  /lib/terminfo/** rk,
  /usr/share/terminfo/** k,
  /usr/share/zoneinfo/** k,
  owner @{PROC}/@{pid}/maps k,

  # for python apps/services
  #include <abstractions/python>
  /usr/bin/python{,2,2.[0-9]*,3,3.[0-9]*} ixr,

  # explicitly deny noisy denials to read-only filesystems (see LP: #1496895
  # for details)
  deny /usr/lib/python3*/{,**/}__pycache__/ w,
  deny /usr/lib/python3*/{,**/}__pycache__/**.pyc.[0-9]* w,
  deny @{INSTALL_DIR}/@{SNAP_NAME}/**/__pycache__/             w,
  deny @{INSTALL_DIR}/@{SNAP_NAME}/**/__pycache__/*.pyc.[0-9]* w,

  # for perl apps/services
  #include <abstractions/perl>
  /usr/bin/perl{,5*} ixr,
  # AppArmor <2.12 doesn't have rules for perl-base, so add them here
  /usr/lib/@{multiarch}/perl{,5,-base}/**            r,
  /usr/lib/@{multiarch}/perl{,5,-base}/[0-9]*/**.so* mr,

  # Note: the following dangerous accesses should not be allowed in most
  # policy, but we cannot explicitly deny since other trusted interfaces might
  # add them.
  # Explicitly deny ptrace for now since it can be abused to break out of the
  # seccomp sandbox. https://lkml.org/lkml/2015/3/18/823
  #audit deny ptrace (trace),

  # Explicitly deny capability mknod so apps can't create devices
  #audit deny capability mknod,

  # Explicitly deny mount, remount and umount so apps can't modify things in
  # their namespace
  #audit deny mount,
  #audit deny remount,
  #audit deny umount,

  # End dangerous accesses

  # Note: this potentially allows snaps to DoS other snaps via resource
  # exhaustion but we can't sensibly mediate this today. In the future we may
  # employ cgroup limits, AppArmor rlimit mlock rules or something else.
  capability ipc_lock,

  # for bash 'binaries' (do *not* use abstractions/bash)
  # user-specific bash files
  /{,usr/}bin/bash ixr,
  /{,usr/}bin/dash ixr,
  /etc/bash.bashrc r,
  /etc/{passwd,group,nsswitch.conf} r,  # very common
  /etc/default/nss r,
  /etc/libnl-3/{classid,pktloc} r,      # apps that use libnl
  /var/lib/extrausers/{passwd,group} r,
  /etc/profile r,
  /etc/environment r,
  /usr/share/terminfo/** r,
  /etc/inputrc r,
  # Common utilities for shell scripts
  /{,usr/}bin/arch ixr,
  /{,usr/}bin/{,g,m}awk ixr,
  /{,usr/}bin/basename ixr,
  /{,usr/}bin/bunzip2 ixr,
  /{,usr/}bin/bzcat ixr,
  /{,usr/}bin/bzdiff ixr,
  /{,usr/}bin/bzgrep ixr,
  /{,usr/}bin/bzip2 ixr,
  /{,usr/}bin/cat ixr,
  /{,usr/}bin/chmod ixr,
  /{,usr/}bin/chown ixr,
  /{,usr/}bin/clear ixr,
  /{,usr/}bin/cmp ixr,
  /{,usr/}bin/cp ixr,
  /{,usr/}bin/cpio ixr,
  /{,usr/}bin/cut ixr,
  /{,usr/}bin/date ixr,
  /{,usr/}bin/dbus-send ixr,
  /{,usr/}bin/dd ixr,
  /{,usr/}bin/diff{,3} ixr,
  /{,usr/}bin/dir ixr,
  /{,usr/}bin/dirname ixr,
  /{,usr/}bin/du ixr,
  /{,usr/}bin/echo ixr,
  /{,usr/}bin/{,e,f,r}grep ixr,
  /{,usr/}bin/env ixr,
  /{,usr/}bin/expr ixr,
  /{,usr/}bin/false ixr,
  /{,usr/}bin/find ixr,
  /{,usr/}bin/flock ixr,
  /{,usr/}bin/fmt ixr,
  /{,usr/}bin/getconf ixr,
  /{,usr/}bin/getent ixr,
  /{,usr/}bin/getopt ixr,
  /{,usr/}bin/groups ixr,
  /{,usr/}bin/gzip ixr,
  /{,usr/}bin/head ixr,
  /{,usr/}bin/hostname ixr,
  /{,usr/}bin/id ixr,
  /{,usr/}bin/igawk ixr,
  /{,usr/}bin/infocmp ixr,
  /{,usr/}bin/kill ixr,
  /{,usr/}bin/ldd ixr,
  /{usr/,}lib{,32,64}/ld{,32,64}-*.so ix,
  /{usr/,}lib/@{multiarch}/ld{,32,64}-*.so ix,
  /{,usr/}bin/less{,file,pipe} ixr,
  /{,usr/}bin/ln ixr,
  /{,usr/}bin/line ixr,
  /{,usr/}bin/link ixr,
  /{,usr/}bin/locale ixr,
  /{,usr/}bin/logger ixr,
  /{,usr/}bin/ls ixr,
  /{,usr/}bin/md5sum ixr,
  /{,usr/}bin/mkdir ixr,
  /{,usr/}bin/mkfifo ixr,
  /{,usr/}bin/mknod ixr,
  /{,usr/}bin/mktemp ixr,
  /{,usr/}bin/more ixr,
  /{,usr/}bin/mv ixr,
  /{,usr/}bin/nice ixr,
  /{,usr/}bin/nohup ixr,
  /{,usr/}bin/openssl ixr, # may cause harmless capability block_suspend denial
  /{,usr/}bin/pgrep ixr,
  /{,usr/}bin/printenv ixr,
  /{,usr/}bin/printf ixr,
  /{,usr/}bin/ps ixr,
  /{,usr/}bin/pwd ixr,
  /{,usr/}bin/readlink ixr,
  /{,usr/}bin/realpath ixr,
  /{,usr/}bin/rev ixr,
  /{,usr/}bin/rm ixr,
  /{,usr/}bin/rmdir ixr,
  /{,usr/}bin/run-parts ixr,
  /{,usr/}bin/sed ixr,
  /{,usr/}bin/seq ixr,
  /{,usr/}bin/sha{1,224,256,384,512}sum ixr,
  /{,usr/}bin/shuf ixr,
  /{,usr/}bin/sleep ixr,
  /{,usr/}bin/sort ixr,
  /{,usr/}bin/stat ixr,
  /{,usr/}bin/stdbuf ixr,
  /{,usr/}bin/stty ixr,
  /{,usr/}bin/sync ixr,
  /{,usr/}bin/systemd-cat ixr,
  /{,usr/}bin/tac ixr,
  /{,usr/}bin/tail ixr,
  /{,usr/}bin/tar ixr,
  /{,usr/}bin/tee ixr,
  /{,usr/}bin/test ixr,
  /{,usr/}bin/tempfile ixr,
  /{,usr/}bin/tset ixr,
  /{,usr/}bin/touch ixr,
  /{,usr/}bin/tput ixr,
  /{,usr/}bin/tr ixr,
  /{,usr/}bin/true ixr,
  /{,usr/}bin/tty ixr,
  /{,usr/}bin/uname ixr,
  /{,usr/}bin/uniq ixr,
  /{,usr/}bin/unlink ixr,
  /{,usr/}bin/unxz ixr,
  /{,usr/}bin/unzip ixr,
  /{,usr/}bin/vdir ixr,
  /{,usr/}bin/wc ixr,
  /{,usr/}bin/which ixr,
  /{,usr/}bin/xargs ixr,
  /{,usr/}bin/xz ixr,
  /{,usr/}bin/yes ixr,
  /{,usr/}bin/zcat ixr,
  /{,usr/}bin/z{,e,f}grep ixr,
  /{,usr/}bin/zip ixr,
  /{,usr/}bin/zipgrep ixr,

  # For snappy reexec on 4.8+ kernels
  /usr/lib/snapd/snap-exec m,

  # For gdb support
  /usr/lib/snapd/snap-gdb-shim ixr,

  # For in-snap tab completion
  /etc/bash_completion.d/{,*} r,
  /usr/lib/snapd/etelpmoc.sh ixr,               # marshaller (see complete.sh for out-of-snap unmarshal)
  /usr/share/bash-completion/bash_completion r, # user-provided completions (run in-snap) may use functions from here

  # For printing the cache (we don't allow updating the cache)
  /{,usr/}sbin/ldconfig{,.real} ixr,

  # uptime
  /{,usr/}bin/uptime ixr,
  @{PROC}/uptime r,
  @{PROC}/loadavg r,

  # lsb-release
  /usr/bin/lsb_release ixr,
  /usr/bin/ r,
  /usr/share/distro-info/*.csv r,

  # Allow reading /etc/os-release. On Ubuntu 16.04+ it is a symlink to /usr/lib
  # which is allowed by the base abstraction, but on 14.04 it is an actual file
  # so need to add it here. Also allow read locks on the file.
  /etc/os-release rk,
  /usr/lib/os-release k,

  # systemd native journal API (see sd_journal_print(4)). This should be in
  # AppArmor's base abstraction, but until it is, include here.
  /run/systemd/journal/socket w,
  /run/systemd/journal/stdout rw, # 'r' shouldn't be needed, but journald
                                  # doesn't leak anything so allow

  # snapctl and its requirements
  /usr/bin/snapctl ixr,
  /usr/lib/snapd/snapctl ixr,
  @{PROC}/sys/net/core/somaxconn r,
  /run/snapd-snap.socket rw,

  # Note: for now, don't explicitly deny this noisy denial so --devmode isn't
  # broken but eventually we may conditionally deny this since it is an
  # information leak.
  #deny /{,var/}run/utmp r,

  # java
  @{PROC}/@{pid}/ r,
  @{PROC}/@{pid}/fd/ r,
  owner @{PROC}/@{pid}/auxv r,
  @{PROC}/sys/vm/zone_reclaim_mode r,
  /etc/lsb-release r,
  /sys/devices/**/read_ahead_kb r,
  /sys/devices/system/cpu/** r,
  /sys/devices/system/node/node[0-9]*/* r,
  /sys/kernel/mm/transparent_hugepage/enabled r,
  /sys/kernel/mm/transparent_hugepage/defrag r,
  # NOTE: this leaks running process but java seems to want it (even though it
  # seems to operate ok without it) and SDL apps crash without it. Allow owner
  # match until AppArmor kernel var is available to solve this properly (see
  # LP: #1546825 for details)
  owner @{PROC}/@{pid}/cmdline r,
  owner @{PROC}/@{pid}/comm r,

  # Per man(5) proc, the kernel enforces that a thread may only modify its comm
  # value or those in its thread group.
  owner @{PROC}/@{pid}/task/@{tid}/comm rw,

  # Allow reading and writing to our file descriptors in /proc which, for
  # example, allow access to /dev/std{in,out,err} which are all symlinks to
  # /proc/self/fd/{0,1,2} respectively. To support the open(..., O_TMPFILE)
  # linkat() temporary file technique, allow all fds. Importantly, access to
  # another's task's fd via this proc interface is mediated via 'ptrace (read)'
  # (readonly) and 'ptrace (trace)' (read/write) which is denied by default, so
  # this rule by itself doesn't allow opening another snap's fds via proc.
  owner @{PROC}/@{pid}/{,task/@{tid}}fd/[0-9]* rw,

  # Miscellaneous accesses
  /dev/{,u}random w,
  /etc/machine-id r,
  /etc/mime.types r,
  @{PROC}/ r,
  @{PROC}/version r,
  @{PROC}/version_signature r,
  /etc/{,writable/}hostname r,
  /etc/{,writable/}localtime r,
  /etc/{,writable/}mailname r,
  /etc/{,writable/}timezone r,
  owner @{PROC}/@{pid}/cgroup r,
  @{PROC}/@{pid}/io r,
  owner @{PROC}/@{pid}/limits r,
  owner @{PROC}/@{pid}/loginuid r,
  @{PROC}/@{pid}/smaps r,
  @{PROC}/@{pid}/stat r,
  @{PROC}/@{pid}/statm r,
  @{PROC}/@{pid}/status r,
  @{PROC}/@{pid}/task/ r,
  @{PROC}/@{pid}/task/[0-9]*/smaps r,
  @{PROC}/@{pid}/task/[0-9]*/stat r,
  @{PROC}/@{pid}/task/[0-9]*/statm r,
  @{PROC}/@{pid}/task/[0-9]*/status r,
  @{PROC}/sys/kernel/hostname r,
  @{PROC}/sys/kernel/osrelease r,
  @{PROC}/sys/kernel/ostype r,
  @{PROC}/sys/kernel/yama/ptrace_scope r,
  @{PROC}/sys/kernel/shmmax r,
  @{PROC}/sys/fs/file-max r,
  @{PROC}/sys/fs/inotify/max_* r,
  @{PROC}/sys/kernel/pid_max r,
  @{PROC}/sys/kernel/random/uuid r,
  @{PROC}/sys/kernel/random/boot_id r,
  /sys/devices/virtual/tty/{console,tty*}/active r,
  /sys/fs/cgroup/memory/memory.limit_in_bytes r,
  /sys/fs/cgroup/memory/snap.@{SNAP_NAME}{,.*}/memory.limit_in_bytes r,
  /{,usr/}lib/ r,

  # Reads of oom_adj and oom_score_adj are safe
  owner @{PROC}/@{pid}/oom_{,score_}adj r,

  # Note: for now, don't explicitly deny write access so --devmode isn't broken
  # but eventually we may conditionally deny this since it allows the process
  # to increase the oom heuristic of other processes (make them more likely to
  # be killed). Once AppArmor kernel var is available to solve this properly,
  # this can safely be allowed since non-root processes won't be able to
  # decrease the value and root processes will only be able to with
  # 'capability sys_resource,' which we deny be default.
  # deny owner @{PROC}/@{pid}/oom_{,score_}adj w,

  # Eases hardware assignment (doesn't give anything away)
  /etc/udev/udev.conf r,
  /sys/       r,
  /sys/bus/   r,
  /sys/class/ r,

  # this leaks interface names and stats, but not in a way that is traceable
  # to the user/device
  @{PROC}/net/dev r,
  @{PROC}/@{pid}/net/dev r,

  # Read-only for the install directory
  @{INSTALL_DIR}/@{SNAP_NAME}/                   r,
  @{INSTALL_DIR}/@{SNAP_NAME}/@{SNAP_REVISION}/    r,
  @{INSTALL_DIR}/@{SNAP_NAME}/@{SNAP_REVISION}/**  mrklix,

  # Read-only install directory for other revisions to help with bugs like
  # LP: #1616650 and LP: #1655992
  @{INSTALL_DIR}/@{SNAP_NAME}/**  mrkix,

  # Read-only home area for other versions
  owner @{HOME}/snap/@{SNAP_NAME}/                  r,
  owner @{HOME}/snap/@{SNAP_NAME}/**                mrkix,

  # Writable home area for this version.
  owner @{HOME}/snap/@{SNAP_NAME}/@{SNAP_REVISION}/** wl,
  owner @{HOME}/snap/@{SNAP_NAME}/common/** wl,

  # Read-only system area for other versions
  /var/snap/@{SNAP_NAME}/   r,
  /var/snap/@{SNAP_NAME}/** mrkix,

  # Writable system area only for this version
  /var/snap/@{SNAP_NAME}/@{SNAP_REVISION}/** wl,
  /var/snap/@{SNAP_NAME}/common/** wl,

  # The ubuntu-core-launcher creates an app-specific private restricted /tmp
  # and will fail to launch the app if something goes wrong. As such, we can
  # simply allow full access to /tmp.
  /tmp/   r,
  /tmp/** mrwlkix,

  # App-specific access to files and directories in /dev/shm. We allow file
  # access in /dev/shm for shm_open() and files in subdirectories for open()
  /{dev,run}/shm/snap.@{SNAP_NAME}.** mrwlkix,
  # Also allow app-specific access for sem_open()
  /{dev,run}/shm/sem.snap.@{SNAP_NAME}.* mrwk,

  # Snap-specific XDG_RUNTIME_DIR that is based on the UID of the user
  owner /run/user/[0-9]*/snap.@{SNAP_NAME}/   rw,
  owner /run/user/[0-9]*/snap.@{SNAP_NAME}/** mrwklix,

  # Allow apps from the same package to communicate with each other via an
  # abstract or anonymous socket
  unix peer=(label=snap.@{SNAP_NAME}.*),

  # Allow apps from the same package to communicate with each other via DBus.
  # Note: this does not grant access to the DBus sockets of well known buses
  # (will still need to use an appropriate interface for that).
  dbus (receive, send) peer=(label=snap.@{SNAP_NAME}.*),

  # Allow apps from the same package to signal each other via signals
  signal peer=snap.@{SNAP_NAME}.*,

  # Allow receiving signals from all snaps (and focus on mediating sending of
  # signals)
  signal (receive) peer=snap.*,

  # Allow receiving signals from unconfined (eg, systemd)
  signal (receive) peer=unconfined,

  # for 'udevadm trigger --verbose --dry-run --tag-match=snappy-assign'
  /{,s}bin/udevadm ixr,
  /etc/udev/udev.conf r,
  /{,var/}run/udev/tags/snappy-assign/ r,
  @{PROC}/cmdline r,
  /sys/devices/**/uevent r,

  # LP: #1447237: adding '--property-match=SNAPPY_APP=<pkgname>' to the above
  # requires:
  #   /run/udev/data/* r,
  # but that reveals too much about the system and cannot be granted to apps
  # by default at this time.

  # For convenience, allow apps to see what is in /dev even though cgroups
  # will block most access
  /dev/ r,
  /dev/**/ r,

  # Allow setting up pseudoterminal via /dev/pts system. This is safe because
  # the launcher uses a per-app devpts newinstance.
  /dev/ptmx rw,

  # Do the same with /sys/devices and /sys/class to help people using hw-assign
  /sys/devices/ r,
  /sys/devices/**/ r,
  /sys/class/ r,
  /sys/class/**/ r,

  # Allow all snaps to chroot
  capability sys_chroot,
  /{,usr/}sbin/chroot ixr,

  # Lttng tracing is very noisy and should not be allowed by confined apps. Can
  # safely deny for the normal case (LP: #1260491). If/when an lttng-trace
  # interface is needed, we can rework this.
  deny /{dev,run,var/run}/shm/lttng-ust-* rw,

  # Allow read-access on /home/ for navigating to other parts of the
  # filesystem. While this allows enumerating users, this is already allowed
  # via /etc/passwd and getent.
  @{HOMEDIRS}/ r,

  # Allow read-access to / for navigating to other parts of the filesystem.
  / r,

###SNIPPETS###
}
`

// classicTemplate contains apparmor template used for snaps with classic
// confinement. This template was Designed by jdstrand:
// https://github.com/snapcore/snapd/pull/2366#discussion_r90101320
//
// The classic template intentionally provides no confinement and is used
// simply to ensure that processes have the proper command-specific security
// label instead of 'unconfined'.
//
// It can be overridden for testing using MockClassicTemplate().
var classicTemplate = `
#include <tunables/global>

###VAR###

###PROFILEATTACH### (attach_disconnected,mediate_deleted) {
  # set file rules so that exec() inherits our profile unless there is
  # already a profile for it (eg, snap-confine)
  / rwkl,
  /** rwlkm,
  /** pix,

  capability,
  change_profile,
  dbus,
  network,
  mount,
  remount,
  umount,
  pivot_root,
  ptrace,
  signal,
  unix,

###SNIPPETS###
}
`

// classicJailmodeSnippet contains extra rules that allow snaps using classic
// confinement, that were put in to jailmode, to execute by at least having
// access to the core snap (e.g. for the dynamic linker and libc).

var classicJailmodeSnippet = `
  # Read-only access to the core snap.
  @{INSTALL_DIR}/core/** r,
  # Read only access to the core snap to load libc from.
  # This is related to LP: #1666897
  @{INSTALL_DIR}/core/*/{,usr/}lib/@{multiarch}/{,**/}lib*.so* m,

  # For snappy reexec on 4.8+ kernels
  @{INSTALL_DIR}/core/*/usr/lib/snapd/snap-exec m,
`

// nfsSnippet contains extra permissions necessary for snaps and snap-confine
// to operate when NFS is used. This is an imperfect solution as this grants
// some network access to all the snaps on the system.
// For tracking see https://bugs.launchpad.net/apparmor/+bug/1724903
var nfsSnippet = `
  # snapd autogenerated workaround for systems using NFS, for details see:
  # https://bugs.launchpad.net/ubuntu/+source/snapd/+bug/1662552
  network inet,
  network inet6,
`

// overlayRootSnippet contains the extra permissions necessary for snap and
// snap-confine to operate on systems where '/' is a writable overlay fs.
// AppArmor requires directory reads for upperdir (but these aren't otherwise
// visible to the snap). While we filter AppArmor regular expression (AARE)
// characters elsewhere, we double quote the path in case UPPERDIR has spaces.
var overlayRootSnippet = `
  # snapd autogenerated workaround for systems using '/' on overlayfs. For
  # details see: https://bugs.launchpad.net/apparmor/+bug/1703674
  "###UPPERDIR###/{,**/}" r,
`

// updateNSTemplate defines the apparmor profile for per-snap snap-update-ns.
//
// The per-snap snap-update-ns profiles are composed via a template and
// snippets for the snap. The template allows:
// - accesses to libraries, files and /proc entries required to run
// - using global and per-snap lock files
// - reading per-snap mount namespaces and mount profiles
// - managing per-snap freezer state files
// - per-snap mounting/unmounting fonts from the host
// - denying mounts to restricted places (eg, /snap/bin and /media)
var updateNSTemplate = `
# Description: Allows snap-update-ns to construct the mount namespace specific
# to a particular snap (see the name below). This specifically includes the
# precise locations of the layout elements.

# vim:syntax=apparmor

#include <tunables/global>

profile snap-update-ns.###SNAP_NAME### (attach_disconnected) {
  # The next four rules mirror those above. We want to be able to read
  # and map snap-update-ns into memory but it may come from a variety of places.
  /usr/lib{,exec,64}/snapd/snap-update-ns mr,
  /var/lib/snapd/hostfs/usr/lib{,exec,64}/snapd/snap-update-ns mr,
  /{,var/lib/snapd/}snap/core/*/usr/lib/snapd/snap-update-ns mr,
  /var/lib/snapd/hostfs/{,var/lib/snapd/}snap/core/*/usr/lib/snapd/snap-update-ns mr,

  # Allow reading the dynamic linker cache.
  /etc/ld.so.cache r,
  # Allow reading, mapping and executing the dynamic linker.
  /{,usr/}lib{,32,64,x32}/{,@{multiarch}/}ld-*.so mrix,
  # Allow reading and mapping various parts of the standard library and
  # dynamically loaded nss modules and what not.
  /{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libc{,-[0-9]*}.so* mr,
  /{,usr/}lib{,32,64,x32}/{,@{multiarch}/}libpthread{,-[0-9]*}.so* mr,

  # Allow reading the command line (snap-update-ns uses it in pre-Go bootstrap code).
  @{PROC}/@{pid}/cmdline r,

  # Allow reading file descriptor paths
  @{PROC}/@{pid}/fd/* r,

  # Allow reading the os-release file (possibly a symlink to /usr/lib).
  /{etc/,usr/lib/}os-release r,

  # Allow creating/grabbing global and per-snap lock files.
  /run/snapd/lock/###SNAP_NAME###.lock rwk,
  /run/snapd/lock/.lock rwk,

  # Allow reading stored mount namespaces,
  /run/snapd/ns/ r,
  /run/snapd/ns/###SNAP_NAME###.mnt r,

  # Allow reading per-snap desired mount profiles. Those are written by
  # snapd and represent the desired layout and content connections.
  /var/lib/snapd/mount/snap.###SNAP_NAME###.fstab r,
  /var/lib/snapd/mount/snap.###SNAP_NAME###.user-fstab r,

  # Allow reading and writing actual per-snap mount profiles. Note that
  # the wildcard in the rule to allow an atomic write + rename strategy.
  # Those files are written by snap-update-ns and represent the actual
  # mount profile at a given moment.
  /run/snapd/ns/snap.###SNAP_NAME###.fstab{,.*} rw,

  # NOTE: at this stage the /snap directory is stable as we have called
  # pivot_root already.

  # Needed to perform mount/unmounts.
  capability sys_admin,
  # Needed for mimic construction.
  capability chown,
  # Needed for dropping to calling user when processing per-user mounts
  capability setuid,
  capability setgid,
  # Allow snap-update-ns to override file ownership and permission checks.
  # This is required because writable mimics now preserve the permissions
  # of the original and hence we may be asked to create a directory when the
  # parent is a tmpfs without DAC write access.
  capability dac_override,

  # Allow freezing and thawing the per-snap cgroup freezers
  /sys/fs/cgroup/freezer/snap.###SNAP_NAME###/freezer.state rw,

  # Allow the content interface to bind fonts from the host filesystem
  mount options=(ro bind) /var/lib/snapd/hostfs/usr/share/fonts/ -> /snap/###SNAP_NAME###/*/**,
  umount /snap/###SNAP_NAME###/*/**,

  # set up user mount namespace
  mount options=(rslave) -> /,

  # Allow traversing from the root directory and several well-known places.
  # Specific directory permissions are added by snippets below.
  / r,
  /etc/ r,
  /snap/ r,
  /tmp/ r,
  /usr/ r,
  /var/ r,
  /var/snap/ r,

  # Allow reading timezone data.
  /usr/share/zoneinfo/** r,

  # Don't allow anyone to touch /snap/bin
  audit deny mount /snap/bin/** -> /**,
  audit deny mount /** -> /snap/bin/**,

  # Don't allow bind mounts to /media which has special
  # sharing and propagates mount events outside of the snap namespace.
  audit deny mount -> /media,

###SNIPPETS###
}
`
