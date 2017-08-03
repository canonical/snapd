// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

const greengrassSupportSummary = `allows operating as the Greengrass service`

const greengrassSupportBaseDeclarationPlugs = `
  greengrass-support:
    allow-installation: false
    deny-auto-connection: true
`

const greengrassSupportBaseDeclarationSlots = `
  greengrass-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const greengrassSupportConnectedPlugAppArmor = `
# Description: can manage greengrass 'things' and their sandboxes. This
# interface is restricted because it gives wide ranging access to the host and
# other processes.

# greengrassd uses 'prctl(PR_CAPBSET_DROP, ...)'
capability setpcap,

# Allow managing child processes (signals, OOM, ptrace, cgroups)
capability kill,

capability sys_resource,
/sys/kernel/mm/hugepages/ r,
owner @{PROC}/[0-9]*/oom_score_adj rw,

capability sys_ptrace,
ptrace (trace) peer=@{profile_name},

owner @{PROC}/[0-9]*/cgroup r,
owner /sys/fs/cgroup/*/{,system.slice/} rw,
owner /sys/fs/cgroup/cpuset/{,system.slice/}cpuset.cpus rw,
owner /sys/fs/cgroup/cpuset/{,system.slice/}cpuset.mems rw,
owner /sys/fs/cgroup/*/system.slice/@{profile_name}.service/{,**} rw,
# for running just after a reboot
owner /sys/fs/cgroup/*/user.slice/ rw,
owner /sys/fs/cgroup/cpuset/user.slice/cpuset.cpus rw,
owner /sys/fs/cgroup/cpuset/user.slice/cpuset.mems rw,
owner /sys/fs/cgroup/*/user.slice/[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]/{,**} rw,

# allow use of ggc_user and ggc_group
capability chown,
capability fowner,
capability fsetid,
capability setuid,
capability setgid,

# Note: when AppArmor supports fine-grained owner matching, can match on
# ggc_user (LP: #1697090)
@{PROC}/[0-9]*/uid_map r,
@{PROC}/[0-9]*/gid_map r,
@{PROC}/[0-9]*/environ r,
owner @{PROC}/[0-9]*/uid_map w,
owner @{PROC}/[0-9]*/gid_map w,

# Allow greengrassd to read restricted non-root directories (LP: #1697090)
capability dac_read_search,

# overlayfs
capability sys_admin,
capability dac_override,  # for various overlayfs accesses

@{PROC}/[0-9]*/mountinfo r,
@{PROC}/filesystems r,

# setup the overlay so we may pivot_root into it
mount fstype=overlay no_source -> /var/snap/@{SNAP_NAME}/**,
mount options=(rw, bind) /var/snap/@{SNAP_NAME}/*/rootfs/ -> /var/snap/@{SNAP_NAME}/*/rootfs/,
mount options=(rw, rbind) /var/snap/@{SNAP_NAME}/*/rootfs/ -> /var/snap/@{SNAP_NAME}/*/rootfs/,
mount fstype=proc proc -> /var/snap/@{SNAP_NAME}/*/rootfs/proc/,
mount options=(rw, nosuid, strictatime) fstype=tmpfs tmpfs -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/,
mount options=(rw, nosuid, nodev, noexec) fstype=mqueue mqueue -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/mqueue/,
mount options=(rw, nosuid, noexec) fstype=devpts devpts -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/pts/,
mount options=(rw, nosuid, nodev, noexec) fstype=tmpfs shm -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/shm/,

# add a few common devices
mount options=(rw, bind) /dev/full -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/full,
mount options=(rw, bind) /dev/null -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/null,
mount options=(rw, bind) /dev/random -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/random,
mount options=(rw, bind) /dev/tty -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/tty,
mount options=(rw, bind) /dev/urandom -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/urandom,
mount options=(rw, bind) /dev/zero -> /var/snap/@{SNAP_NAME}/*/rootfs/dev/zero,

# allow mounting lambda, runtime and whatever else in SNAP_DATA into the
# pivot_root
mount options=(ro, remount, bind) -> /var/snap/@{SNAP_NAME}/**/rootfs/**/,
mount options=(rw, bind) /var/snap/@{SNAP_NAME}/**/ -> /var/snap/@{SNAP_NAME}/*/rootfs/**/,

# setup /proc
mount options=(ro, remount) -> /proc/{asound/,bus/,fs/,irq/,sys/,sysrq-trigger},
mount options=(ro, nosuid, nodev, noexec, remount, rbind) -> /proc/{asound/,bus/,fs/,irq/,sys/,sysrq-trigger},
mount options=(rw, bind) /proc/asound/ -> /proc/asound/,
mount options=(rw, bind) /proc/bus/ -> /proc/bus/,
mount options=(rw, bind) /proc/fs/ -> /proc/fs/,
mount options=(rw, bind) /proc/irq/ -> /proc/irq/,
mount options=(rw, bind) /proc/sys/ -> /proc/sys/,
mount options=(rw, bind) /proc/sysrq-trigger -> /proc/sysrq-trigger,

# remap a few /proc accesses to /dev/null
mount options=(rw, bind) /dev/null -> /proc/kcore,
mount options=(rw, bind) /dev/null -> /proc/sched_debug,
mount options=(rw, bind) /dev/null -> /proc/timer_stats,

# perform the pivot_root into the overlay
pivot_root oldroot=/var/snap/greengrass/@{SNAP_REVISION}/rootfs/.pivot_root*/ /var/snap/greengrass/*/rootfs/,
mount options=(rw, rprivate) -> /.pivot_root*/,
umount /.pivot_root*/,
owner /.pivot_root*/ w,
mount options=(rw, rprivate) -> /,
mount options=(ro, remount, rbind) -> /,

# allow tearing down the overlay
umount /var/snap/@{SNAP_NAME}/**,
/run/mount/utab rw,
/bin/umount ixr,

# For lambda functions, post pivot_root lambda execution accesses
/certs/ r,
/certs/** r,
/group/ r,
/group/** r,
/state/ r,
/state/sqlite* rwk,

# Ideally we would use a child profile for these but since the greengrass
# sandbox is using prctl(PR_SET_NO_NEW_PRIVS, ...) we cannot since that blocks
# profile transitions. With policy stacking we could use a more restrictive
# child profile, but there are bugs which prevent that at this time
# (LP: #1696552, LP: #1696551). As such, must simply rely on the greengrass
# sandbox for now.
/lambda/ r,
/lambda/** ixr,
`

const greengrassSupportConnectedPlugSeccomp = `
# Description: can manage greengrass 'things' and their sandboxes. This
# interface is restricted because it gives wide ranging access to the host and
# other processes.

# allow use of ggc_user and ggc_group
# FIXME: seccomp arg filter by this uid/gid when supported by snap-confine
fchown
fchown32
fchownat
setgroups
setgroups32

# for overlayfs and various bind mounts
mount
umount2
pivot_root

# greengrassd calls 'sethostname("sandbox", 7)'
sethostname

# greengrassd sets up several session keyrings. See:
# https://github.com/torvalds/linux/blob/master/Documentation/security/keys.txt
# Note that the lambda functions themselves run under a seccomp sandbox setup
# by greengrassd.
keyctl
`

func init() {
	registerIface(&commonInterface{
		name:                  "greengrass-support",
		summary:               greengrassSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  greengrassSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  greengrassSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: greengrassSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  greengrassSupportConnectedPlugSeccomp,
		reservedForOS:         true,
	})
}
