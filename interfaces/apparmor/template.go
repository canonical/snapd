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

###PROFILEATTACH### (attach_disconnected) {
  #include <abstractions/base>
  #include <abstractions/consoles>
  #include <abstractions/openssl>

  # While in later versions of the base abstraction, include this explicitly
  # for series 16 and cross-distro
  /etc/ld.so.preload r,

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
  /bin/bash ixr,
  /bin/dash ixr,
  /etc/bash.bashrc r,
  /etc/{passwd,group,nsswitch.conf} r,  # very common
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
  /{,usr/}bin/clear ixr,
  /{,usr/}bin/cmp ixr,
  /{,usr/}bin/cp ixr,
  /{,usr/}bin/cpio ixr,
  /{,usr/}bin/cut ixr,
  /{,usr/}bin/date ixr,
  /{,usr/}bin/dd ixr,
  /{,usr/}bin/diff{,3} ixr,
  /{,usr/}bin/dir ixr,
  /{,usr/}bin/dirname ixr,
  /{,usr/}bin/echo ixr,
  /{,usr/}bin/{,e,f,r}grep ixr,
  /{,usr/}bin/env ixr,
  /{,usr/}bin/expr ixr,
  /{,usr/}bin/false ixr,
  /{,usr/}bin/find ixr,
  /{,usr/}bin/flock ixr,
  /{,usr/}bin/fmt ixr,
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
  /{,usr/}bin/mktemp ixr,
  /{,usr/}bin/more ixr,
  /{,usr/}bin/mv ixr,
  /{,usr/}bin/nice ixr,
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
  # but on 14.04 it is an actual file so it doens't fall under other rules.
  /etc/os-release r,

  # systemd native journal API (see sd_journal_print(4)). This should be in
  # AppArmor's base abstraction, but until it is, include here.
  /run/systemd/journal/socket w,

  # snapctl and its requirements
  /usr/bin/snapctl ixr,
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
  @{PROC}/@{pid}/io r,
  owner @{PROC}/@{pid}/limits r,
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
  @{PROC}/sys/kernel/yama/ptrace_scope r,
  @{PROC}/sys/kernel/shmmax r,
  @{PROC}/sys/fs/file-max r,
  @{PROC}/sys/kernel/pid_max r,
  @{PROC}/sys/kernel/random/uuid r,
  @{PROC}/sys/kernel/random/boot_id r,
  /sys/devices/virtual/tty/{console,tty*}/active r,
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

###PROFILEATTACH### (attach_disconnected) {
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
`
