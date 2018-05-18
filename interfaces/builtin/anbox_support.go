// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd.
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

package builtin

const anboxSupportSummary = `allows operating as the Anbox container management service`

const anboxSupportBaseDeclarationPlugs = `
  anbox-support:
    allow-installation: false
    deny-auto-connection: true
`

const anboxSupportBaseDeclarationSlots = `
  anbox-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const anboxSupportConnectedPlugAppArmor = `
/sys/module/fuse/parameters/userns_mounts rw,
/run/anbox/{,**} rw,     # SNAP_COMMON?

# aa-exec -p ...//lxc -- ...
@{PROC}/[0-9]*/attr/exec w,
@{PROC}/[0-9]*/attr/current r,

change_profile unsafe /** -> @{profile_name}//lxc,
profile lxc (attach_disconnected,mediate_deleted) {
    file,
    ptrace,
    signal,
    dbus,
    capability,
    mount,
    umount,
    remount,
    network,

    # snapctl and its requirements; needed as we're now in a child profile
    /usr/bin/snapctl ixr,
    @{PROC}/sys/net/core/somaxconn r,
    /run/snapd-snap.socket rw,

    #include <abstractions/base>
    #include <abstractions/nameservice>
    /dev/tty rw,
    @{INSTALL_DIR}/@{SNAP_NAME}/{,**} mrix,  # fine-tune
    /var/snap/@{SNAP_NAME}/{,**} rwk,        # fine-tune
    /{,usr/}{,s}bin/* ixr,                   # fine-tune

    @{PROC}/[0-9]*/cgroup r,
    @{PROC}/[0-9]*/mounts r,
    @{PROC}/[0-9]*/mountinfo r,
    /sys/module/apparmor/parameters/enabled r,

    # KERNEL=="loop-control" in udev connected plug
    /dev/loop-control rw,
    # KERNEL=="loop*", SUBSYSTEM=="block" in udev connected plug
    /dev/loop[0-9]* rw,

    /sys/fs/cgroup/unified/cgroup.controllers r,  # checking if using unified?

    # Anbox comes with a squashfs image which contains the entire Android rootfs which
    # has to be mounted in place together with the read-write directories data and cache.
    mount fstype=squashfs options=(ro) /dev/loop[0-9]* -> /var/snap/@{SNAP_NAME}/common/rootfs/,
    mount options=(rw, bind) /var/snap/@{SNAP_NAME}/common/cache/ -> /var/snap/@{SNAP_NAME}/common/rootfs/cache/,
    mount options=(rw, bind) /var/snap/@{SNAP_NAME}/common/data/ -> /var/snap/@{SNAP_NAME}/common/rootfs/data/,
    umount /var/snap/@{SNAP_NAME}/common/rootfs/,
    umount /var/snap/@{SNAP_NAME}/common/rootfs/cache/,
    umount /var/snap/@{SNAP_NAME}/common/rootfs/data/,

    # starting the container
    /run/lxc/ rw,  # should be anbox specific
    /run/lxc/lock/ rw,
    /run/lxc/lock/var/ rw,
    /run/lxc/lock/var/snap/ rw,
    /run/lxc/lock/var/snap/@{SNAP_NAME}/ rw,
    /run/lxc/lock/var/snap/@{SNAP_NAME}/common/ rw,
    /run/lxc/lock/var/snap/@{SNAP_NAME}/common/containers/{,**/} rw,
    /run/lxc/lock/var/snap/@{SNAP_NAME}/common/containers/.default rwk,

    mount options=(rw, bind) /var/snap/@{SNAP_NAME}/common/{,**/} -> /var/snap/@{SNAP_NAME}/common/lxc/{,**/},
    mount options=(rw, bind) /var/snap/@{SNAP_NAME}/common/lxc/proc/sysrq-trigger -> /var/snap/@{SNAP_NAME}/common/lxc/proc/sysrq-trigger,
    mount options=(ro, nosuid, nodev, noexec, remount, bind) -> /var/snap/@{SNAP_NAME}/common/lxc/proc/sysrq-trigger,
    mount options=(rw, rbind) /var/snap/@{SNAP_NAME}/common/{,**/} -> /var/snap/@{SNAP_NAME}/common/lxc/{,**/},
    mount fstype=tmpfs -> /var/snap/@{SNAP_NAME}/common/lxc/**/,
    mount fstype=proc -> /var/snap/@{SNAP_NAME}/common/lxc/proc/,
    mount options=(ro, nosuid, nodev, noexec, remount, bind) -> /var/snap/@{SNAP_NAME}/common/lxc/proc/sys/{,**/},
    mount options=(rw, move) /var/snap/@{SNAP_NAME}/common/lxc/**/ -> /var/snap/@{SNAP_NAME}/common/lxc/**/,
    mount options=(rw, nosuid, nodev, noexec) -> /var/snap/@{SNAP_NAME}/common/lxc/sys/,
    mount options=(ro, nosuid, nodev, noexec, remount, bind) -> /var/snap/@{SNAP_NAME}/common/lxc/sys/,
    mount options=(rw, nosuid, nodev, noexec, remount, bind) -> /var/snap/@{SNAP_NAME}/common/lxc/sys/{,**/},
    mount fstype=sysfs -> /var/snap/@{SNAP_NAME}/common/lxc/**/,

    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/full -> /var/snap/@{SNAP_NAME}/common/lxc/dev/full,
    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/null -> /var/snap/@{SNAP_NAME}/common/lxc/dev/null,
    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/random -> /var/snap/@{SNAP_NAME}/common/lxc/dev/random,
    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/urandom -> /var/snap/@{SNAP_NAME}/common/lxc/dev/urandom,
    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/tty -> /var/snap/@{SNAP_NAME}/common/lxc/dev/tty,
    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/zero -> /var/snap/@{SNAP_NAME}/common/lxc/dev/zero,
    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/pts/* -> /var/snap/@{SNAP_NAME}/common/lxc/dev/pts/*,
    mount options=(rw, bind) /{,var/snap/@{SNAP_NAME}/common/lxc/}dev/pts/0 -> /var/snap/@{SNAP_NAME}/common/lxc/dev/console,

    pivot_root /var/snap/@{SNAP_NAME}/common/lxc/ -> /var/snap/@{SNAP_NAME}/common/lxc/,

    # post pivot_root
    # @{HOME} doesn't work here
    mount options=(rw, bind) /home/*/snap/@{SNAP_NAME}/** -> /var/snap/@{SNAP_NAME}/common/lxc/dev/**,
    mount options=(rw, bind) /home/*/snap/@{SNAP_NAME}/** -> /var/snap/@{SNAP_NAME}/common/devices/*,
    mount options=(rw, bind) /var/snap/@{SNAP_NAME}/common/devices/* -> /var/snap/@{SNAP_NAME}/common/lxc/dev/*,
    mount fstype=devpts options=(rw, nosuid, noexec) -> /dev/pts/,
    mount options=(rw, bind) /dev/{,pts/}ptmx -> /dev/{,pts/}ptmx,

    # apparmor="DENIED" operation="mount" info="failed mntpnt match" error=-13 profile="snap.anbox.container-manager//lxc" name="/" pid=4539 comm="anbox" flags="rw, rslave"
    umount /,
    umount /var/snap/@{SNAP_NAME}/common/lxc/dev/console,
    # done post pivot_root

    # starting processes in the container
    /anbox-init.sh ixr,
    @{PROC}/1/attr/current r,

    # end processes in the container

    owner @{PROC}/@{pid}/net/unix r,
    unix (bind,listen) type=stream addr="@/var/snap/anbox/common/containers/default/command",
    /sys/fs/cgroup/*/lxc/{,**} rw,   # this should be anbox specific
    @{PROC}/@{pid}/uid_map rw,
    @{PROC}/@{pid}/gid_map rw,
    /sys/fs/cgroup/cpuset/cpuset.cpus r,
    /sys/fs/cgroup/cpuset/cpuset.mems r,

    # None of these work with this denial:
    # operation="ptrace" profile="snap.anbox.container-manager" pid=27006 comm="anbox" requested_mask="trace" denied_mask="trace" peer="snap.anbox.container-manager"
    ptrace peer=@{profile_name},
    #ptrace peer=snap.@{SNAP_NAME}.*,
    #ptrace,

    capability chown,
    capability mknod,
    capability fowner,
    capability fsetid,
    capability dac_override,
    capability dac_read_search,
    capability setuid,
    capability setgid,
    capability sys_admin,

    # We need to allow access to the fuse device here as we're in a child
    # profile and fuse-support wouldn't help at this point.
    /dev/fuse rw,

    # container start
    owner @{PROC}/[0-9]*/fd/ r,
    owner @{PROC}/[0-9]*/stat r,
    owner @{PROC}/[0-9]*/ns/ r,
    /dev/ptmx rw,
    /dev/pts/[0-9]* rw,
    capability net_admin,
    /sys/devices/** r,
    capability sys_ptrace,
    / r,
    capability sys_module, # why?
    capability kill,

    # Allow liblxc to change to the container child profile
    # FIXME: Can we get the parent profile name somehow?
    change_profile unsafe /** -> snap.anbox.container-manager//container,

    mount options=(rw,bind),
    mount options=(rw,rbind),
    mount options=(rw,make-slave) -> **,
    mount options=(rw,make-rslave) -> **,
    mount options=(rw,make-shared) -> **,
    mount options=(rw,make-rshared) -> **,
    mount options=(rw,make-private) -> **,
    mount options=(rw,make-rprivate) -> **,
    mount options=(rw,make-unbindable) -> **,
    mount options=(rw,make-runbindable) -> **,
}

# The container child profile is used by the actual Android container the Anbox container
# manager creates and manages through liblxc. The policy here is inheriting from the one
# LXD defines for its containers. See https://github.com/lxc/lxd/blob/master/lxd/apparmor.go
profile "container" flags=(attach_disconnected,mediate_deleted) {
    ### Base profile
    capability,
    dbus,
    file,
    network,
    umount,

    # Allow us to receive signals from anywhere.
    signal (receive),

    # Allow us to send signals to ourselves
    signal peer=@{profile_name},

    # Allow other processes to read our /proc entries, futexes, perf tracing and
    # kcmp for now (they will need 'read' in the first place). Administrators can
    # override with:
    #   deny ptrace (readby) ...
    ptrace (readby),

    # Allow other processes to trace us by default (they will need 'trace' in
    # the first place). Administrators can override with:
    #   deny ptrace (tracedby) ...
    ptrace (tracedby),

    # Allow us to ptrace ourselves
    ptrace peer=@{profile_name},

    # ignore DENIED message on / remount
    deny mount options=(ro, remount) -> /,
    deny mount options=(ro, remount, silent) -> /,

    # allow tmpfs mounts everywhere
    mount fstype=tmpfs,

    # allow hugetlbfs mounts everywhere
    mount fstype=hugetlbfs,

    # allow mqueue mounts everywhere
    mount fstype=mqueue,

    # allow fuse mounts everywhere
    mount fstype=fuse,
    mount fstype=fuse.*,

    # deny access under /proc/bus to avoid e.g. messing with pci devices directly
    deny @{PROC}/bus/** wklx,

    # deny writes in /proc/sys/fs but allow binfmt_misc to be mounted
    mount fstype=binfmt_misc -> /proc/sys/fs/binfmt_misc/,
    deny @{PROC}/sys/fs/** wklx,

    # allow efivars to be mounted, writing to it will be blocked though
    mount fstype=efivarfs -> /sys/firmware/efi/efivars/,

    # block some other dangerous paths
    deny @{PROC}/kcore rwklx,
    deny @{PROC}/sysrq-trigger rwklx,

    # deny writes in /sys except for /sys/fs/cgroup, also allow
    # fusectl, securityfs and debugfs to be mounted there (read-only)
    mount fstype=fusectl -> /sys/fs/fuse/connections/,
    mount fstype=securityfs -> /sys/kernel/security/,
    mount fstype=debugfs -> /sys/kernel/debug/,
    deny mount fstype=debugfs -> /var/lib/ureadahead/debugfs/,
    mount fstype=proc -> /proc/,
    mount fstype=sysfs -> /sys/,
    mount options=(rw, nosuid, nodev, noexec, remount) -> /sys/,
    deny /sys/firmware/efi/efivars/** rwklx,
    # note, /sys/kernel/security/** handled below
    mount options=(move) /sys/fs/cgroup/cgmanager/ -> /sys/fs/cgroup/cgmanager.lower/,
    mount options=(ro, nosuid, nodev, noexec, remount, strictatime) -> /sys/fs/cgroup/,

    # deny reads from debugfs
    deny /sys/kernel/debug/{,**} rwklx,

    # allow paths to be made slave, shared, private or unbindable
    # FIXME: This currently doesn't work due to the apparmor parser treating those as allowing all mounts.
    #  mount options=(rw,make-slave) -> **,
    #  mount options=(rw,make-rslave) -> **,
    #  mount options=(rw,make-shared) -> **,
    #  mount options=(rw,make-rshared) -> **,
    #  mount options=(rw,make-private) -> **,
    #  mount options=(rw,make-rprivate) -> **,
    #  mount options=(rw,make-unbindable) -> **,
    #  mount options=(rw,make-runbindable) -> **,

    # allow bind-mounts of anything except /proc, /sys and /dev
    mount options=(rw,bind) /[^spd]*{,/**},
    mount options=(rw,bind) /d[^e]*{,/**},
    mount options=(rw,bind) /de[^v]*{,/**},
    mount options=(rw,bind) /dev/.[^l]*{,/**},
    mount options=(rw,bind) /dev/.l[^x]*{,/**},
    mount options=(rw,bind) /dev/.lx[^c]*{,/**},
    mount options=(rw,bind) /dev/.lxc?*{,/**},
    mount options=(rw,bind) /dev/[^.]*{,/**},
    mount options=(rw,bind) /dev?*{,/**},
    mount options=(rw,bind) /p[^r]*{,/**},
    mount options=(rw,bind) /pr[^o]*{,/**},
    mount options=(rw,bind) /pro[^c]*{,/**},
    mount options=(rw,bind) /proc?*{,/**},
    mount options=(rw,bind) /s[^y]*{,/**},
    mount options=(rw,bind) /sy[^s]*{,/**},
    mount options=(rw,bind) /sys?*{,/**},

    # allow moving mounts except for /proc, /sys and /dev
    mount options=(rw,move) /[^spd]*{,/**},
    mount options=(rw,move) /d[^e]*{,/**},
    mount options=(rw,move) /de[^v]*{,/**},
    mount options=(rw,move) /dev/.[^l]*{,/**},
    mount options=(rw,move) /dev/.l[^x]*{,/**},
    mount options=(rw,move) /dev/.lx[^c]*{,/**},
    mount options=(rw,move) /dev/.lxc?*{,/**},
    mount options=(rw,move) /dev/[^.]*{,/**},
    mount options=(rw,move) /dev?*{,/**},
    mount options=(rw,move) /p[^r]*{,/**},
    mount options=(rw,move) /pr[^o]*{,/**},
    mount options=(rw,move) /pro[^c]*{,/**},
    mount options=(rw,move) /proc?*{,/**},
    mount options=(rw,move) /s[^y]*{,/**},
    mount options=(rw,move) /sy[^s]*{,/**},
    mount options=(rw,move) /sys?*{,/**},

    # generated by: lxc-generate-aa-rules.py container-rules.base
    deny /proc/sys/[^kn]*{,/**} wklx,
    deny /proc/sys/k[^e]*{,/**} wklx,
    deny /proc/sys/ke[^r]*{,/**} wklx,
    deny /proc/sys/ker[^n]*{,/**} wklx,
    deny /proc/sys/kern[^e]*{,/**} wklx,
    deny /proc/sys/kerne[^l]*{,/**} wklx,
    deny /proc/sys/kernel/[^smhd]*{,/**} wklx,
    deny /proc/sys/kernel/d[^o]*{,/**} wklx,
    deny /proc/sys/kernel/do[^m]*{,/**} wklx,
    deny /proc/sys/kernel/dom[^a]*{,/**} wklx,
    deny /proc/sys/kernel/doma[^i]*{,/**} wklx,
    deny /proc/sys/kernel/domai[^n]*{,/**} wklx,
    deny /proc/sys/kernel/domain[^n]*{,/**} wklx,
    deny /proc/sys/kernel/domainn[^a]*{,/**} wklx,
    deny /proc/sys/kernel/domainna[^m]*{,/**} wklx,
    deny /proc/sys/kernel/domainnam[^e]*{,/**} wklx,
    deny /proc/sys/kernel/domainname?*{,/**} wklx,
    deny /proc/sys/kernel/h[^o]*{,/**} wklx,
    deny /proc/sys/kernel/ho[^s]*{,/**} wklx,
    deny /proc/sys/kernel/hos[^t]*{,/**} wklx,
    deny /proc/sys/kernel/host[^n]*{,/**} wklx,
    deny /proc/sys/kernel/hostn[^a]*{,/**} wklx,
    deny /proc/sys/kernel/hostna[^m]*{,/**} wklx,
    deny /proc/sys/kernel/hostnam[^e]*{,/**} wklx,
    deny /proc/sys/kernel/hostname?*{,/**} wklx,
    deny /proc/sys/kernel/m[^s]*{,/**} wklx,
    deny /proc/sys/kernel/ms[^g]*{,/**} wklx,
    deny /proc/sys/kernel/msg*/** wklx,
    deny /proc/sys/kernel/s[^he]*{,/**} wklx,
    deny /proc/sys/kernel/se[^m]*{,/**} wklx,
    deny /proc/sys/kernel/sem*/** wklx,
    deny /proc/sys/kernel/sh[^m]*{,/**} wklx,
    deny /proc/sys/kernel/shm*/** wklx,
    deny /proc/sys/kernel?*{,/**} wklx,
    deny /proc/sys/n[^e]*{,/**} wklx,
    deny /proc/sys/ne[^t]*{,/**} wklx,
    deny /proc/sys/net?*{,/**} wklx,
    deny /sys/[^fdck]*{,/**} wklx,
    deny /sys/c[^l]*{,/**} wklx,
    deny /sys/cl[^a]*{,/**} wklx,
    deny /sys/cla[^s]*{,/**} wklx,
    deny /sys/clas[^s]*{,/**} wklx,
    deny /sys/class/[^n]*{,/**} wklx,
    deny /sys/class/n[^e]*{,/**} wklx,
    deny /sys/class/ne[^t]*{,/**} wklx,
    deny /sys/class/net?*{,/**} wklx,
    deny /sys/class?*{,/**} wklx,
    deny /sys/d[^e]*{,/**} wklx,
    deny /sys/de[^v]*{,/**} wklx,
    deny /sys/dev[^i]*{,/**} wklx,
    deny /sys/devi[^c]*{,/**} wklx,
    deny /sys/devic[^e]*{,/**} wklx,
    deny /sys/device[^s]*{,/**} wklx,
    deny /sys/devices/[^v]*{,/**} wklx,
    deny /sys/devices/v[^i]*{,/**} wklx,
    deny /sys/devices/vi[^r]*{,/**} wklx,
    deny /sys/devices/vir[^t]*{,/**} wklx,
    deny /sys/devices/virt[^u]*{,/**} wklx,
    deny /sys/devices/virtu[^a]*{,/**} wklx,
    deny /sys/devices/virtua[^l]*{,/**} wklx,
    deny /sys/devices/virtual/[^n]*{,/**} wklx,
    deny /sys/devices/virtual/n[^e]*{,/**} wklx,
    deny /sys/devices/virtual/ne[^t]*{,/**} wklx,
    deny /sys/devices/virtual/net?*{,/**} wklx,
    deny /sys/devices/virtual?*{,/**} wklx,
    deny /sys/devices?*{,/**} wklx,
    deny /sys/f[^s]*{,/**} wklx,
    deny /sys/fs/[^c]*{,/**} wklx,
    deny /sys/fs/c[^g]*{,/**} wklx,
    deny /sys/fs/cg[^r]*{,/**} wklx,
    deny /sys/fs/cgr[^o]*{,/**} wklx,
    deny /sys/fs/cgro[^u]*{,/**} wklx,
    deny /sys/fs/cgrou[^p]*{,/**} wklx,
    deny /sys/fs/cgroup?*{,/**} wklx,
    deny /sys/fs?*{,/**} wklx,

    ### Feature: unix
    # Allow receive via unix sockets from anywhere
    unix (receive),

    # Allow all unix in the container
    unix peer=(label=@{profile_name}),

    ### Feature: cgroup namespace
    mount fstype=cgroup -> /sys/fs/cgroup/**,

    ### Configuration: nesting
    pivot_root,
    ptrace,
    signal,

    deny /dev/.lxd/proc/** rw,
    deny /dev/.lxd/sys/** rw,

    mount /var/lib/lxd/shmounts/ -> /var/lib/lxd/shmounts/,
    mount none -> /var/lib/lxd/shmounts/,
    mount fstype=proc -> /usr/lib/*/lxc/**,
    mount fstype=sysfs -> /usr/lib/*/lxc/**,
    mount options=(rw,bind),
    mount options=(rw,rbind),
    mount options=(rw,make-rshared),

    # there doesn't seem to be a way to ask for:
    # mount options=(ro,nosuid,nodev,noexec,remount,bind),
    # as we always get mount to $cdir/proc/sys with those flags denied
    # So allow all mounts until that is straightened out:
    mount,
    mount options=bind /var/lib/lxd/shmounts/** -> /var/lib/lxd/**,

    mount options=(rw,make-slave) -> **,
    mount options=(rw,make-rslave) -> **,
    mount options=(rw,make-shared) -> **,
    mount options=(rw,make-rshared) -> **,
    mount options=(rw,make-private) -> **,
    mount options=(rw,make-rprivate) -> **,
    mount options=(rw,make-unbindable) -> **,
    mount options=(rw,make-runbindable) -> **,

    mount options=(rw,bind),
    mount options=(rw,rbind),
}
`

const anboxSupportConnectedPlugSecComp = `
# On first startup the container manager needs to ensure that certain directory are
# own by the UID range we assign to the Android container.
chown

# Various things get bind mount into Android container
mount
umount2

# liblxc creates various device nodes in the /dev it sets up for the Android container
mknod - |S_IFCHR -
mknodat - - |S_IFCHR -
setgroups
sethostname
pivot_root

# needed?
setns

# only saw these once with aa-exec -p unconfined -- ...
fchown
fchown32
fchownat
setpriority

# Needed by liblxc
prctl PR_CAP_AMBIENT PR_CAP_AMBIENT_RAISE - - -
`

var anboxSupportConnectedPlugUDev = []string{
	`KERNEL=="loop-control"`,
	`KERNEL=="loop*", SUBSYSTEM=="block"`,
	`KERNEL=="ashmem"`,
	`KERNEL=="binder"`,
	// We can't use fuse-support as the need for FUSE is actually by the
	// container AppArmor child profile.
	`KERNEL=="fuse"`,
}

// Anbox requires two kernel modules to be loaded in the system which are
// currently coming via a DKMS module. See https://docs.anbox.io/userguide/install.html
// for details. Anbox will warn the user if one of the modules isn't loaded.
var anboxSupportConnectedPlugKModModules = []string{
	"binder_linux",
	"ashmem_linux",
}

func init() {
	registerIface(&commonInterface{
		name:                     "anbox-support",
		summary:                  anboxSupportSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		baseDeclarationSlots:     anboxSupportBaseDeclarationSlots,
		baseDeclarationPlugs:     anboxSupportBaseDeclarationPlugs,
		connectedPlugAppArmor:    anboxSupportConnectedPlugAppArmor,
		connectedPlugSecComp:     anboxSupportConnectedPlugSecComp,
		connectedPlugUDev:        anboxSupportConnectedPlugUDev,
		connectedPlugKModModules: anboxSupportConnectedPlugKModModules,
		reservedForOS:            true,
	})
}
