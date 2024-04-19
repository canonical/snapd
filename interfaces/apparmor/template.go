// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

// Rules for app snaps are comprised of:
//
// - preamble and rules common regardless of base runtime
// - base-specific runtime rules
// - snippet rules from interfaces, etc, regardless of base runtime
//
// As part of the mount namespace setup, some directories from the host will be
// bind mounted onto the base snap (these are defined by snap-confine). The
// locations of the target mounts that the snap sees at runtime are (for
// clarity, not all subdirectories are listed (eg, /var/lib/snapd/hostfs is not
// listed since /var/lib/snapd is)):
//
// - /dev
// - /etc
// - /home
// - /lib/modules and /usr/lib/modules
// - /lib/firmware and /usr/lib/firmware
// - /mnt, /media and /run/media
// - /proc
// - /root
// - /run
// - /snap and /var/snap
// - /sys
// - /usr/lib/snapd
// - /usr/src
// - /var/lib/dhcp
// - /var/lib/extrausers
// - /var/lib/jenkins
// - /var/lib/snapd
// - /var/log
// - /var/tmp
//
// For files coming from the host in this manner, accesses should be common to
// all bases, either via the template or interface rules (eg, given the same
// connected interfaces, access to devices in /dev should generally be the
// same, regardless of whether the snap specifies 'base: core18' or
// 'base: other').
//
// The preamble and default accesses common to all bases go in templateCommon.
// These rules include the aformentioned host file rules as well as non-file
// rules (eg signal, dbus, unix, etc).
var templateCommon = `
# vim:syntax=apparmor

#include <tunables/global>

###INCLUDE_SYSTEM_TUNABLES_HOME_D_WITH_VENDORED_APPARMOR###
###INCLUDE_IF_EXISTS_SNAP_TUNING###

# snapd supports the concept of 'parallel installs' where snaps with the same
# name are differentiated by '_<instance>' such that foo, foo_bar and foo_baz
# may all be installed on the system. To support this, SNAP_NAME is set to the
# name (eg, 'foo') while SNAP_INSTANCE_NAME is set to the instance name (eg
# 'foo_bar'). The profile name and most rules therefore reference
# SNAP_INSTANCE_NAME. In some cases, snapd will adjust the snap's runtime
# environment so the snap doesn't have to be aware of the distinction (eg,
# SNAP, SNAP_DATA and SNAP_COMMON are all bind mounted onto a directory with
# SNAP_NAME so the security policy will allow writing to both locations (since
# they are equivalent).

###VAR###

###PROFILEATTACH### ###FLAGS### {
  #include <abstractions/base>
  #include <abstractions/consoles>
  #include <abstractions/openssl>

  # While in later versions of the base abstraction, include this explicitly
  # for series 16 and cross-distro
  /etc/ld.so.preload r,

  # The base abstraction doesn't yet have this
  /etc/sysconfig/clock r,
  owner @{PROC}/@{pid}/maps k,

  # /proc/XXXX/map_files contains the same info than /proc/XXXX/maps, but
  # in a format that is simpler to manage, because it doesn't require to
  # parse the text data inside a file, but just reading the contents of
  # a directory.
  # Reading /proc/XXXX/maps is already allowed in the base template
  # via <abstractions/base>. Also, only the owner can read it, and the
  # kernel limits access to it by requiring 'ptrace' enabled, so allowing
  # to access /proc/XXXX/map_files can be considered secure too.
  owner @{PROC}/@{pid}/map_files/ r,

  # While the base abstraction has rules for encryptfs encrypted home and
  # private directories, it is missing rules for directory read on the toplevel
  # directory of the mount (LP: #1848919)
  owner @{HOME}/.Private/ r,
  owner @{HOMEDIRS}/.ecryptfs/*/.Private/ r,

  # for python apps/services
  #include <abstractions/python>
  /etc/python3.[0-9]*/**                                r,

  ###PYCACHEDENY###

  # for perl apps/services
  #include <abstractions/perl>
  # Missing from perl abstraction
  /usr/lib/@{multiarch}/perl{,5,-base}/auto/**.so* mr,

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
  /etc/bash.bashrc r,
  /etc/inputrc r,
  /etc/environment r,
  /etc/profile r,

  # user/group/seat lookups
  /etc/{passwd,group,nsswitch.conf} r,  # very common
  /var/lib/extrausers/{passwd,group} r,
  /run/systemd/users/[0-9]* r,
  /etc/default/nss r,

  # libnss-systemd (subset from nameservice abstraction)
  #
  #   https://systemd.io/USER_GROUP_API/
  #   https://systemd.io/USER_RECORD/
  #   https://www.freedesktop.org/software/systemd/man/nss-systemd.html
  #
  # Allow User/Group lookups via common VarLink socket APIs. Applications need
  # to either consult all of them or the io.systemd.Multiplexer frontend.
  /run/systemd/userdb/ r,
  /run/systemd/userdb/io.systemd.Multiplexer rw,
  /run/systemd/userdb/io.systemd.DynamicUser rw,        # systemd-exec users
  /run/systemd/userdb/io.systemd.Home rw,               # systemd-home dirs
  /run/systemd/userdb/io.systemd.NameServiceSwitch rw,  # UNIX/glibc NSS
  /run/systemd/userdb/io.systemd.Machine rw,            # systemd-machined

  /etc/libnl-3/{classid,pktloc} r,      # apps that use libnl

  # For snappy reexec on 4.8+ kernels
  /usr/lib/snapd/snap-exec m,

  # For gdb support
  /usr/lib/snapd/snap-gdb-shim ixr,
  /usr/lib/snapd/snap-gdbserver-shim ixr,

  # For in-snap tab completion
  /etc/bash_completion.d/{,*} r,
  /usr/lib/snapd/etelpmoc.sh ixr,               # marshaller (see complete.sh for out-of-snap unmarshal)
  /usr/share/bash-completion/bash_completion r, # user-provided completions (run in-snap) may use functions from here

  # uptime
  @{PROC}/uptime r,
  @{PROC}/loadavg r,

  # Allow reading /etc/os-release. On Ubuntu 16.04+ it is a symlink to /usr/lib
  # which is allowed by the base abstraction, but on 14.04 it is an actual file
  # so need to add it here. Also allow read locks on the file.
  /etc/os-release rk,
  /usr/lib/os-release k,

  # systemd native journal API (see sd_journal_print(4)). This should be in
  # AppArmor's base abstraction, but until it is, include here. We include
  # the base journal path as well as the journal namespace pattern path. Each
  # journal namespace for quota groups will be prefixed with 'snap-'.
  /run/systemd/journal{,.snap-*}/socket w,
  /run/systemd/journal{,.snap-*}/stdout rw, # 'r' shouldn't be needed, but journald
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
  # LP: #1546825 for details). comm is a subset of cmdline, so allow it too.
  owner @{PROC}/@{pid}/cmdline r,
  owner @{PROC}/@{pid}/comm r,

  # Per man(5) proc, the kernel enforces that a thread may only modify its comm
  # value or those in its thread group.
  owner @{PROC}/@{pid}/task/@{tid}/comm rw,

  # Allow reading and writing to our file descriptors in /proc which, for
  # example, allow access to /dev/std{in,out,err} which are all symlinks to
  # /proc/self/fd/{0,1,2} respectively. To support the open(..., O_TMPFILE)
  # linkat() temporary file technique, allow all fds. Importantly, access to
  # another task's fd via this proc interface is mediated via 'ptrace (read)'
  # (readonly) and 'ptrace (trace)' (read/write) which is denied by default, so
  # this rule by itself doesn't allow opening another snap's fds via proc.
  owner @{PROC}/@{pid}/{,task/@{tid}}fd/[0-9]* rw,

  # Miscellaneous accesses
  /dev/{,u}random w,
  /etc/machine-id r,
  /etc/mime.types r,
  /etc/default/keyboard r,
  @{PROC}/ r,
  @{PROC}/version r,
  @{PROC}/version_signature r,
  /etc/{,writable/}hostname r,
  /etc/{,writable/}localtime r,
  /etc/{,writable/}mailname r,
  /etc/{,writable/}timezone r,
  owner @{PROC}/@{pid}/cgroup rk,
  @{PROC}/@{pid}/cpuset r,
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
  @{PROC}/sys/fs/pipe-max-size r,
  @{PROC}/sys/kernel/hostname r,
  @{PROC}/sys/kernel/osrelease r,
  @{PROC}/sys/kernel/ostype r,
  @{PROC}/sys/kernel/pid_max r,
  @{PROC}/sys/kernel/yama/ptrace_scope r,
  @{PROC}/sys/kernel/shmmax r,
  # Allow apps to introspect the level of dbus mediation AppArmor implements.
  /sys/kernel/security/apparmor/features/dbus/mask r,
  @{PROC}/sys/fs/file-max r,
  @{PROC}/sys/fs/file-nr r,
  @{PROC}/sys/fs/inotify/max_* r,
  @{PROC}/sys/kernel/pid_max r,
  @{PROC}/sys/kernel/random/boot_id r,
  @{PROC}/sys/kernel/random/entropy_avail r,
  @{PROC}/sys/kernel/random/uuid r,
  @{PROC}/sys/kernel/cap_last_cap r,
  # Allow access to the uuidd daemon (this daemon is a thin wrapper around
  # time and getrandom()/{,u}random and, when available, runs under an
  # unprivilged, dedicated user).
  /run/uuidd/request rw,
  /sys/devices/virtual/tty/{console,tty*}/active r,
  /sys/fs/cgroup/memory/{,user.slice/}memory.limit_in_bytes r,
  /sys/fs/cgroup/memory/{,**/}snap.@{SNAP_INSTANCE_NAME}{,.*}/memory.limit_in_bytes r,
  /sys/fs/cgroup/memory/{,**/}snap.@{SNAP_INSTANCE_NAME}{,.*}/memory.stat r,
  /sys/fs/cgroup/cpu,cpuacct/{,user.slice/}cpu.cfs_{period,quota}_us r,
  /sys/fs/cgroup/cpu,cpuacct/{,**/}snap.@{SNAP_INSTANCE_NAME}{,.*}/cpu.cfs_{period,quota}_us r,
  /sys/fs/cgroup/cpu,cpuacct/{,user.slice/}cpu.shares r,
  /sys/fs/cgroup/cpu,cpuacct/{,**/}snap.@{SNAP_INSTANCE_NAME}{,.*}/cpu.shares r,
  /sys/kernel/mm/transparent_hugepage/hpage_pmd_size r,
  /sys/module/apparmor/parameters/enabled r,
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

  # Read-only of this snap
  /var/lib/snapd/snaps/@{SNAP_NAME}_*.snap r,

  # Read-only of snapd restart state for snapctl specifically
  /var/lib/snapd/maintenance.json r,

  # Read-only for the install directory
  # bind mount used here (see 'parallel installs', above)
  @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/                   r,
  @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}/@{SNAP_REVISION}}/    r,
  @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}/@{SNAP_REVISION}}/**  mrklix,

  # Read-only install directory for other revisions to help with bugs like
  # LP: #1616650 and LP: #1655992
  @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/**  mrkix,

  # Read-only home area for other versions
  # bind mount *not* used here (see 'parallel installs', above)
  owner @{HOME}/snap/@{SNAP_INSTANCE_NAME}/                  r,
  owner @{HOME}/snap/@{SNAP_INSTANCE_NAME}/**                mrkix,

  # Experimental snap folder changes
  owner @{HOME}/.snap/data/@{SNAP_INSTANCE_NAME}/                    r,
  owner @{HOME}/.snap/data/@{SNAP_INSTANCE_NAME}/**                  mrkix,
  owner @{HOME}/.snap/data/@{SNAP_INSTANCE_NAME}/@{SNAP_REVISION}/** wl,
  owner @{HOME}/.snap/data/@{SNAP_INSTANCE_NAME}/common/**           wl,

  owner @{HOME}/Snap/@{SNAP_INSTANCE_NAME}/                          r,
  owner @{HOME}/Snap/@{SNAP_INSTANCE_NAME}/**                        mrkixwl,

  # Writable home area for this version.
  # bind mount *not* used here (see 'parallel installs', above)
  owner @{HOME}/snap/@{SNAP_INSTANCE_NAME}/@{SNAP_REVISION}/** wl,
  owner @{HOME}/snap/@{SNAP_INSTANCE_NAME}/common/** wl,

  # Read-only system area for other versions
  # bind mount used here (see 'parallel installs', above)
  /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/   r,
  /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** mrkix,

  # Writable system area only for this version
  # bind mount used here (see 'parallel installs', above)
  /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/@{SNAP_REVISION}/** wl,
  /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/common/** wl,

  # The ubuntu-core-launcher creates an app-specific private restricted /tmp
  # and will fail to launch the app if something goes wrong. As such, we can
  # simply allow full access to /tmp.
  /tmp/   r,
  /tmp/** mrwlkix,

  # App-specific access to files and directories in /dev/shm. We allow file
  # access in /dev/shm for shm_open() and files in subdirectories for open()
  # bind mount *not* used here (see 'parallel installs', above)
  /{dev,run}/shm/snap.@{SNAP_INSTANCE_NAME}.** mrwlkix,
  # Also allow app-specific access for sem_open()
  /{dev,run}/shm/sem.snap.@{SNAP_INSTANCE_NAME}.* mrwlk,

  # Snap-specific XDG_RUNTIME_DIR that is based on the UID of the user
  # bind mount *not* used here (see 'parallel installs', above)
  owner /run/user/[0-9]*/snap.@{SNAP_INSTANCE_NAME}/   rw,
  owner /run/user/[0-9]*/snap.@{SNAP_INSTANCE_NAME}/** mrwklix,

  # Allow apps from the same package to communicate with each other via an
  # abstract or anonymous socket
  unix (bind, listen) addr="@snap.@{SNAP_INSTANCE_NAME}.**",
  unix peer=(label=snap.@{SNAP_INSTANCE_NAME}.*),

  # Allow apps from the same package to communicate with each other via DBus.
  # Note: this does not grant access to the DBus sockets of well known buses
  # (will still need to use an appropriate interface for that).
  dbus (receive, send) peer=(label=snap.@{SNAP_INSTANCE_NAME}.*),
  # In addition to the above, dbus-run-session attempts reading these files
  # from the snap base runtime.
  /usr/share/dbus-1/services/{,*} r,
  /usr/share/dbus-1/system-services/{,*} r,
  # Allow apps to perform DBus introspection on org.freedesktop.DBus for both
  # the system and session buses.
  # Note: this does not grant access to the DBus sockets of these buses, but
  # we grant it here since it is missing from the dbus abstractions
  # (LP: #1866168)
  dbus (send)
      bus={session,system}
      path=/org/freedesktop/DBus
      interface=org.freedesktop.DBus.Introspectable
      member=Introspect
      peer=(label=unconfined),

  # Allow apps from the same package to signal each other via signals
  signal peer=snap.@{SNAP_INSTANCE_NAME}.*,

  # Allow receiving signals from all snaps (and focus on mediating sending of
  # signals)
  signal (receive) peer=snap.*,

  # Allow receiving signals from unconfined (eg, systemd)
  signal (receive) peer=unconfined,

  # for 'udevadm trigger --verbose --dry-run --tag-match=snappy-assign'
  /{,usr/}{,s}bin/udevadm ixr,
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

  # Snap-specific run directory. Bind mount *not* used here
  # (see 'parallel installs', above)
  /run/snap.@{SNAP_INSTANCE_NAME}/ rw,
  /run/snap.@{SNAP_INSTANCE_NAME}/** mrwklix,

  # Snap-specific lock directory and prerequisite navigation permissions.
  /run/lock/ r,
  /run/lock/snap.@{SNAP_INSTANCE_NAME}/ rw,
  /run/lock/snap.@{SNAP_INSTANCE_NAME}/** mrwklix,
  
  ###DEVMODE_SNAP_CONFINE###
`

var templateFooter = `
###SNIPPETS###
}
`

// defaultCoreRuntimeTemplateRules contains core* runtime-specific rules. In general,
// binaries exposed here declare what the core runtime has historically been
// expected to support.
var defaultCoreRuntimeTemplateRules = `
  # Default rules for core base runtimes

  # The base abstraction doesn't yet have this
  /{,usr/}lib/terminfo/** rk,
  /usr/share/terminfo/** k,
  /usr/share/zoneinfo/** k,

  # for python apps/services
  /usr/bin/python{,2,2.[0-9]*,3,3.[0-9]*} ixr,
  # additional accesses needed for newer pythons in later bases
  /usr/lib{,32,64}/python3.[0-9]*/**.{pyc,so}           mr,
  /usr/lib{,32,64}/python3.[0-9]*/**.{egg,py,pth}       r,
  /usr/lib{,32,64}/python3.[0-9]*/{site,dist}-packages/ r,
  /usr/lib{,32,64}/python3.[0-9]*/lib-dynload/*.so      mr,
  /usr/include/python3.[0-9]*/pyconfig.h               r,

  # for perl apps/services
  /usr/bin/perl{,5*} ixr,
  # AppArmor <2.12 doesn't have rules for perl-base, so add them here
  /usr/lib/@{multiarch}/perl{,5,-base}/**            r,
  /usr/lib/@{multiarch}/perl{,5,-base}/[0-9]*/**.so* mr,

  # for bash 'binaries' (do *not* use abstractions/bash)
  # user-specific bash files
  /{,usr/}bin/bash ixr,
  /{,usr/}bin/dash ixr,
  /usr/share/terminfo/** r,

  # Common utilities for shell scripts
  /{,usr/}bin/arch ixr,
  /{,usr/}bin/{,g,m}awk ixr,
  /{,usr/}bin/base32 ixr,
  /{,usr/}bin/base64 ixr,
  /{,usr/}bin/basename ixr,
  /{,usr/}bin/bunzip2 ixr,
  /{,usr/}bin/busctl ixr,
  /{,usr/}bin/bzcat ixr,
  /{,usr/}bin/bzdiff ixr,
  /{,usr/}bin/bzgrep ixr,
  /{,usr/}bin/bzip2 ixr,
  /{,usr/}bin/cat ixr,
  /{,usr/}bin/chgrp ixr,
  /{,usr/}bin/chmod ixr,
  /{,usr/}bin/chown ixr,
  /{,usr/}bin/clear ixr,
  /{,usr/}bin/cmp ixr,
  /{,usr/}bin/cp ixr,
  /{,usr/}bin/cpio ixr,
  /{,usr/}bin/cut ixr,
  /{,usr/}bin/date ixr,
  /{,usr/}bin/dbus-daemon ixr,
  /{,usr/}bin/dbus-run-session ixr,
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
  /{,usr/}bin/fold ixr,
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
  /{usr/,}lib/@{multiarch}/ld{,32,64}-*.so* ix,
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
  /{,usr/}bin/numfmt ixr,
  /{,usr/}bin/od ixr,
  /{,usr/}bin/openssl ixr, # may cause harmless capability block_suspend denial
  /{,usr/}bin/paste ixr,
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
  /{,usr/}bin/uptime ixr,
  /{,usr/}bin/vdir ixr,
  /{,usr/}bin/wc ixr,
  /{,usr/}bin/which{,.debianutils} ixr,
  /{,usr/}bin/xargs ixr,
  /{,usr/}bin/xz ixr,
  /{,usr/}bin/yes ixr,
  /{,usr/}bin/zcat ixr,
  /{,usr/}bin/z{,e,f}grep ixr,
  /{,usr/}bin/zip ixr,
  /{,usr/}bin/zipgrep ixr,

  # lsb-release
  /usr/bin/lsb_release ixr,
  /usr/bin/ r,
  /usr/share/distro-info/*.csv r,

  # For printing the cache (we don't allow updating the cache)
  /{,usr/}sbin/ldconfig{,.real} ixr,

  # Allow all snaps to chroot
  /{,usr/}sbin/chroot ixr,
`

// defaultCoreRuntimeTemplate contains the default apparmor template for core* bases. It
// can be overridden for testing using MockTemplate().
var defaultCoreRuntimeTemplate = templateCommon + defaultCoreRuntimeTemplateRules + templateFooter

// defaultOtherBaseTemplateRules for non-core* bases. When a snap specifies an
// alternative base to core*, it is allowed read-only access to all files
// within the base, but all other accesses (eg, host file rules, signal, dbus,
// unix, etc rules) should be the same as the default template.
//
// For clarity and ease of maintenance, we will whitelist top-level directories
// here instead of using glob rules (we can add more if specific bases
// dictate).
var defaultOtherBaseTemplateRules = `
  # Default rules for non-core base runtimes

  # /bin and /sbin (/usr/{,local/}{s,bin} handled in /usr)
  /{,s}bin/ r,
  /{,s}bin/** mrklix,

  # /lib - the mount setup may bind mount to:
  #
  # - /lib/firmware
  # - /lib/modules
  #
  # Everything but /lib/firmware and /lib/modules
  /{,usr/}lib/ r,
  /{,usr/}lib/[^fm]** mrklix,
  /{,usr/}lib/{f[^i],m[^o]}** mrklix,
  /{,usr/}lib/{fi[^r],mo[^d]}** mrklix,
  /{,usr/}lib/{fir[^m],mod[^u]}** mrklix,
  /{,usr/}lib/{firm[^w],modu[^l]}** mrklix,
  /{,usr/}lib/{firmw[^a],modul[^e]}** mrklix,
  /{,usr/}lib/{firmwa[^r],module[^s]}** mrklix,
  /{,usr/}lib/modules[^/]** mrklix,
  /{,usr/}lib/firmwar[^e]** mrklix,
  /{,usr/}lib/firmware[^/]** mrklix,

  # /lib64, etc
  /{,usr/}lib[^/]** mrklix,

  # /opt
  /opt/ r,
  /opt/** mrklix,

  # /usr - the mount setup may bind mount to:
  #
  # - /usr/lib/modules
  # - /usr/lib/firmware
  # - /usr/lib/snapd
  # - /usr/src
  #
  # Everything but /usr/lib and /usr/src, which are handled elsewhere.
  /usr/ r,
  /usr/[^ls]** mrklix,
  /usr/{l[^i],s[^r]}** mrklix,
  /usr/{li[^b],sr[^c]}** mrklix,
  /usr/{lib,src}[^/]** mrklix,
  # Everything in /usr/lib except /usr/lib/firmware, /usr/lib/modules and
  # /usr/lib/snapd, which are handled elsewhere.
  /usr/lib/[^fms]** mrklix,
  /usr/lib/{f[^i],m[^o],s[^n]}** mrklix,
  /usr/lib/{fi[^r],mo[^d],sn[^a]}** mrklix,
  /usr/lib/{fir[^m],mod[^u],sna[^p]}** mrklix,
  /usr/lib/{firm[^w],modu[^l],snap[^d]}** mrklix,
  /usr/lib/snapd[^/]** mrklix,

  # /var - the mount setup may bind mount in:
  #
  # - /var/lib/dhcp
  # - /var/lib/extrausers
  # - /var/lib/jenkins
  # - /var/lib/snapd
  # - /var/log
  # - /var/snap
  # - /var/tmp
  #
  # Everything but /var/lib, /var/log, /var/snap and /var/tmp, which are
  # handled elsewhere.
  /var/ r,
  /var/[^lst]** mrklix,
  /var/{l[^io],s[^n],t[^m]}** mrklix,
  /var/{li[^b],lo[^g],sn[^a],tm[^p]}** mrklix,
  /var/{lib,log,tmp}[^/]** mrklix,
  /var/sna[^p]** mrklix,
  /var/snap[^/]** mrklix,
  # Everything in /var/lib except /var/lib/dhcp, /var/lib/extrausers,
  # /var/lib/jenkins and /var/lib/snapd which are handled elsewhere.
  /var/lib/ r,
  /var/lib/[^dejs]** mrklix,
  /var/lib/{d[^h],e[^x],j[^e],s[^n]}** mrklix,
  /var/lib/{dh[^c],ex[^t],je[^n],sn[^a]}** mrklix,
  /var/lib/{dhc[^p],ext[^r],jen[^k],sna[^p]}** mrklix,
  /var/lib/dhcp[^/]** mrklix,
  /var/lib/{extr[^a],jenk[^i],snap[^d]}** mrklix,
  /var/lib/snapd[^/]** mrklix,
  /var/lib/{extra[^u],jenki[^n]}** mrklix,
  /var/lib/{extrau[^s],jenkin[^s]}** mrklix,
  /var/lib/jenkins[^/]** mrklix,
  /var/lib/extraus[^e]** mrklix,
  /var/lib/extrause[^r]** mrklix,
  /var/lib/extrauser[^s]** mrklix,
  /var/lib/extrausers[^/]** mrklix,
`

// defaultOtherBaseTemplate contains the default apparmor template for non-core
// bases
var defaultOtherBaseTemplate = templateCommon + defaultOtherBaseTemplateRules + templateFooter

// Template for privilege drop and chown operations. The specific setuid,
// setgid and chown operations are controlled via seccomp.
//
// To expand on the policy comment below: "this is not a problem in practice":
// access to sockets is mediated by file and unix AppArmor rules. When the
// access is allowed, the snap is expected to be able to use the socket. Some
// service listeners will employ additional checks, such as 'is the connecting
// (snap) process root' or 'is the connecting non-root (snap) process in a
// particular group', etc. Since snapd daemons start as root and because the
// service listeners typically let the root process do anything, the snap
// doesn't gain anything from being able to forge a uid since it has full
// access to the socket API already. A snap could forge a check to bypass the
// theoretical case of the service listener wanting to limit root to something
// less than another user, but in practice service listeners won't do this
// because it is ineffective against unconfined root processes which can
// manipulate the service listener in other ways to subvert a check like this.
//
// For CAP_KILL, AppArmor mediates signals and the default policy allows
// sending signals only to processes with a security label that matches the
// snap, but AppArmor does not currently mediate the uid/gid of the
// sender/receiver to finely mediate what non-root uid/gids a root process may
// send to, so we have always required the process-control interface for snaps
// to send signals to other users (even within the same snap). We want to
// maintain this with our privilege dropping rules, so we omit 'capability
// kill' since snaps can work within the system without 'capability kill':
//   - root parent can drop, spawn a child and later (dropped) parent can send a
//     signal
//   - root parent can spawn a child that drops, then later temporarily drop
//     (ie, seteuid/setegid), send the signal, then reraise
var privDropAndChownRules = `
  # allow setuid, setgid and chown for privilege dropping (mediation is done
  # via seccomp). Note: CAP_SETUID allows (and CAP_SETGID is the same, but
  # for gid operations):
  # - forging of UIDs when passing passing socket credentials via UNIX domain
  #   sockets and we don't currently mediate socket credentials, between
  #   mediating socket access in general and the execve() boundary that drops
  #   the capability for non-root commands, this is not a problem in practice.
  # - accessing the persistent keyring via keyctl, but keyctl is mediated via
  #   seccomp.
  # - writing a user ID mapping in a user namespace, but we mediate access to
  #   /proc/*/uid_map with AppArmor
  #
  # CAP_DAC_OVERRIDE and CAP_DAC_READ_SEARCH are intentionally omitted from the
  # policy since we want traditional DAC to be enforced for root. It is
  # expected that a program that is dropping privileges, etc will create/modify
  # files in a way that doesn't require these capabilities.
  capability setuid,
  capability setgid,
  capability chown,
  #capability dac_override,
  #capability dac_read_search,

  # Similarly, CAP_KILL is intentionally omitted since we want traditional
  # DAC to be enforced for root. It is expected that a program that is spawning
  # processes that ultimately run as non-root will send signals to those
  # processes as the matching non-root user.
  #capability kill,
`

// coreSnippet contains apparmor rules specific only for
// snaps on native core systems.
var coreSnippet = `
# Allow each snaps to access each their own folder on the
# ubuntu-save partition, with write permissions.
/var/lib/snapd/save/snap/@{SNAP_INSTANCE_NAME}/ rw,
/var/lib/snapd/save/snap/@{SNAP_INSTANCE_NAME}/** mrwklix,
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

###INCLUDE_SYSTEM_TUNABLES_HOME_D_WITH_VENDORED_APPARMOR###

###VAR###

###PROFILEATTACH### ###FLAGS### {
  # set file rules so that exec() inherits our profile unless there is
  # already a profile for it (eg, snap-confine)
  / rwkl,
  /** rwlkm,
  /** pix,

  capability,
  ###CHANGEPROFILE_RULE###
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

var ptraceTraceDenySnippet = `
# While commands like 'ps', 'ip netns identify <pid>', 'ip netns pids foo', etc
# trigger a 'ptrace (trace)' denial, they aren't actually tracing other
# processes. Unfortunately, the kernel overloads trace such that the LSMs are
# unable to distinguish between tracing other processes and other accesses.
# ptrace (trace) can be used to break out of the seccomp sandbox unless the
# kernel has 93e35efb8de45393cf61ed07f7b407629bf698ea (in 4.8+). Until snapd
# has full ptrace support conditional on kernel support, explicitly deny to
# silence noisy denials/avoid confusion and accidentally giving away this
# dangerous access frivolously.
deny ptrace (trace),
deny capability sys_ptrace,
`

var pycacheDenySnippet = `
# explicitly deny noisy denials to read-only filesystems (see LP: #1496895
# for details)
deny /usr/lib/python3*/{,**/}__pycache__/ w,
deny /usr/lib/python3*/{,**/}__pycache__/**.pyc.[0-9]* w,
# bind mount used here (see 'parallel installs', above)
deny @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/**/__pycache__/             w,
deny @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/**/__pycache__/*.pyc.[0-9]* w,
`

var sysModuleCapabilityDenySnippet = `
# The rtnetlink kernel interface can trigger the loading of kernel modules,
# first attempting to operate on a network module (this requires the net_admin
# capability) and falling back to loading ordinary modules (and this requires
# the sys_module capability). For reference, see the dev_load() function in:
# https://kernel.ubuntu.com/git/ubuntu/ubuntu-focal.git/tree/net/core/dev_ioctl.c?h=v5.13#n354
# The following rule is used to silence the denials for attempting to load
# generic kernel modules, while still allowing the loading of network modules.
deny capability sys_module,
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

###INCLUDE_SYSTEM_TUNABLES_HOME_D_WITH_VENDORED_APPARMOR###

profile snap-update-ns.###SNAP_INSTANCE_NAME### (attach_disconnected) {
  # The next four rules mirror those above. We want to be able to read
  # and map snap-update-ns into memory but it may come from a variety of places.
  /usr/lib{,exec,64}/snapd/snap-update-ns mr,
  /var/lib/snapd/hostfs/usr/lib{,exec,64}/snapd/snap-update-ns mr,
  /{,var/lib/snapd/}snap/{core,snapd}/*/usr/lib/snapd/snap-update-ns mr,
  /var/lib/snapd/hostfs/{,var/lib/snapd/}snap/core/*/usr/lib/snapd/snap-update-ns mr,

  # Allow reading the dynamic linker cache.
  /etc/ld.so.cache r,
  # Allow reading, mapping and executing the dynamic linker.
  /{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld-*.so mrix,
  # Allow reading and mapping various parts of the standard library and
  # dynamically loaded nss modules and what not.
  /{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}libc{,-[0-9]*}.so* mr,
  /{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}libpthread{,-[0-9]*}.so* mr,

  # Common devices accesses
  /dev/null rw,
  /dev/full rw,
  /dev/zero rw,
  /dev/random r,
  /dev/urandom r,

  # golang runtime variables
  /sys/kernel/mm/transparent_hugepage/hpage_pmd_size r,
  # glibc 2.27+ may poke this file to find out the number of CPUs
  # available in the system when creating a new arena for malloc, see
  # Golang issue 25628
  /sys/devices/system/cpu/online r,

  # Allow reading the command line (snap-update-ns uses it in pre-Go bootstrap code).
  owner @{PROC}/@{pid}/cmdline r,

  # Allow reading of own maps (Go runtime)
  owner @{PROC}/@{pid}/maps r,

  # Allow reading file descriptor paths
  owner @{PROC}/@{pid}/fd/* r,

  # Allow reading /proc/version. For release.go WSL detection.
  @{PROC}/version r,

  # Allow reading own cgroups
  owner @{PROC}/@{pid}/cgroup r,

  # Allow reading somaxconn, required in newer distro releases
  @{PROC}/sys/net/core/somaxconn r,
  # but silence noisy denial of inet/inet6
  deny network inet,
  deny network inet6,

  # Allow reading the os-release file (possibly a symlink to /usr/lib).
  /{etc/,usr/lib/}os-release r,

  # Allow creating/grabbing global and per-snap lock files.
  /run/snapd/lock/###SNAP_INSTANCE_NAME###.lock rwk,
  /run/snapd/lock/.lock rwk,

  # While the base abstraction has rules for encryptfs encrypted home and
  # private directories, it is missing rules for directory read on the toplevel
  # directory of the mount (LP: #1848919)
  owner @{HOME}/.Private/ r,
  owner @{HOMEDIRS}/.ecryptfs/*/.Private/ r,

  # Allow reading stored mount namespaces,
  /run/snapd/ns/ r,
  /run/snapd/ns/###SNAP_INSTANCE_NAME###.mnt r,

  # Allow reading per-snap desired mount profiles. Those are written by
  # snapd and represent the desired layout and content connections.
  /var/lib/snapd/mount/snap.###SNAP_INSTANCE_NAME###.fstab r,
  /var/lib/snapd/mount/snap.###SNAP_INSTANCE_NAME###.user-fstab r,

  # Allow reading and writing actual per-snap mount profiles. Note that
  # the wildcard in the rule to allow an atomic write + rename strategy.
  # Those files are written by snap-update-ns and represent the actual
  # mount profile at a given moment.
  /run/snapd/ns/snap.###SNAP_INSTANCE_NAME###.fstab{,.*} rw,

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
  # v1 hierarchy where we know the group name of all processes of
  # a given snap upfront
  /sys/fs/cgroup/freezer/snap.###SNAP_INSTANCE_NAME###/freezer.state rw,
  # v2 hierarchy, where we need to walk the tree to looking for the tracking
  # groups and act on each one
  /sys/fs/cgroup/ r,
  /sys/fs/cgroup/** r,
  /sys/fs/cgroup/**/snap.###SNAP_INSTANCE_NAME###.*.scope/cgroup.freeze rw,
  /sys/fs/cgroup/**/snap.###SNAP_INSTANCE_NAME###.*.service/cgroup.freeze rw,

  # Allow the content interface to bind fonts from the host filesystem
  mount options=(ro bind) /var/lib/snapd/hostfs/usr/share/fonts/ -> /snap/###SNAP_INSTANCE_NAME###/*/**,
  mount options=(rw private) -> /snap/###SNAP_INSTANCE_NAME###/*/**,
  umount /snap/###SNAP_INSTANCE_NAME###/*/**,

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

  # Allow receiving signals from unconfined (eg, systemd)
  signal (receive) peer=unconfined,
  # Allow sending and receiving signals from ourselves.
  signal peer=@{profile_name},

  # Commonly needed permissions for writable mimics.
  /tmp/ r,
  /tmp/.snap/{,**} rw,

  # snapd logger.go checks /proc/cmdline
  @{PROC}/cmdline r,

  # snap checks if vendored apparmor parser should be used at startup
  /usr/lib/snapd/info r,
  /lib/apparmor/functions r,

  # Allow snap-update-ns to open home directory
  owner @{HOME}/ r,

###SNIPPETS###
}
`
