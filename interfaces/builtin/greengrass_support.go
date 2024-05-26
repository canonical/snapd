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

import (
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

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

const greengrassSupportConnectedPlugAppArmorCore = `
# these accesses are necessary for Ubuntu Core 16, likely due to the version
# of apparmor or the kernel which doesn't resolve the upper layer of an
# overlayfs mount correctly
# the accesses show up as runc trying to read from
# /system-data/var/snap/greengrass/x1/ggc-writable/packages/1.7.0/var/worker/overlays/$UUID/upper/
/system-data/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/ rw,
/system-data/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/{,**} rw,
`

const greengrassSupportProcessModeConnectedPlugAppArmor = `
# Description: can manage greengrass 'things' and their sandboxes. This policy
# is meant currently only to enable Greengrass to run _only_ process-mode or
# "no container" lambdas.
# needed by older versions of cloneBinary.ensureSelfCloned() to avoid
# CVE-2019-5736
/ ix,
# newer versions of runC have this denial instead of "/ ix" above
/bin/runc rix,
`

const greengrassSupportFullContainerConnectedPlugAppArmor = `
# Description: can manage greengrass 'things' and their sandboxes. This
# policy is intentionally not restrictive and is here to help guard against
# programming errors and not for security confinement. The greengrassd
# daemon by design requires extensive access to the system and
# cannot be effectively confined against malicious activity.

# greengrassd uses 'prctl(PR_CAPBSET_DROP, ...)'
capability setpcap,

# Allow managing child processes (signals, OOM, ptrace, cgroups)
capability kill,

capability sys_resource,
/sys/kernel/mm/hugepages/ r,
/sys/kernel/mm/transparent_hugepage/{,**} r,
owner @{PROC}/[0-9]*/oom_score_adj rw,

capability sys_ptrace,
ptrace (trace) peer=@{profile_name},

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

# for setting up mounts
@{PROC}/[0-9]*/mountinfo r,
@{PROC}/filesystems r,

# runc needs this
@{PROC}/[0-9]*/setgroups r,

# cgroup accesses
# greengrassd extensively uses cgroups to confine it's containers (AKA lambdas)
# and needs to read what cgroups are available; we allow reading any cgroup,
# but limit writes below
# also note that currently greengrass is not implemented in such a way that it
# can stack it's cgroups inside the cgroup that snapd would normally enforce
# but this may change in the future
# an example cgroup access looks like this:
# /old_rootfs/sys/fs/cgroup/cpuset/system.slice/7d23e67f-13f5-4b7e-5a85-83f8773345a8/
# the old_rootfs prefix is due to the pivot_root - the "old" rootfs is mounted
# at /old_rootfs before
@{PROC}/cgroups r,
owner @{PROC}/[0-9]*/cgroup r,
owner /old_rootfs/sys/fs/cgroup/{,**} r,
owner /old_rootfs/sys/fs/cgroup/{blkio,cpuset,devices,hugetlb,memory,perf_event,pids,freezer/snap.@{SNAP_NAME}}/{,system.slice/}system.slice/ rw,
owner /old_rootfs/sys/fs/cgroup/{blkio,cpuset,devices,hugetlb,memory,perf_event,pids,freezer/snap.@{SNAP_NAME}}/{,system.slice/}system.slice/[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]/{,**} rw,
# separated from the above rule for clarity due to the comma in "net_cls,net_prio"
owner /old_rootfs/sys/fs/cgroup/net_cls,net_prio/{,system.slice/}system.slice/ rw,
owner /old_rootfs/sys/fs/cgroup/net_cls,net_prio/{,system.slice/}system.slice/[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]/{,**} rw,
owner /old_rootfs/sys/fs/cgroup/cpu,cpuacct/{,system.slice/}system.slice/ rw,
owner /old_rootfs/sys/fs/cgroup/cpu,cpuacct/{,system.slice/}system.slice/[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]/{,**} rw,
owner /old_rootfs/sys/fs/cgroup/{devices,memory,pids,blkio,systemd}/{,system.slice/}snap.@{SNAP_NAME}.greengrass{,d.service}/system.slice/ rw,
owner /old_rootfs/sys/fs/cgroup/{devices,memory,pids,blkio,systemd}/{,system.slice/}snap.@{SNAP_NAME}.greengrass{,d.service}/system.slice/[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]/{,**} rw,
owner /old_rootfs/sys/fs/cgroup/cpu,cpuacct/system.slice/snap.@{SNAP_NAME}.greengrass{,d.service}/system.slice/ rw,
owner /old_rootfs/sys/fs/cgroup/cpu,cpuacct/system.slice/snap.@{SNAP_NAME}.greengrass{,d.service}/system.slice/{,**} rw,
# specific rule for cpuset files
owner /old_rootfs/sys/fs/cgroup/cpuset/{,system.slice/}cpuset.{cpus,mems} rw,

# the wrapper scripts need to use mount/umount and pivot_root from the
# core snap
/{,usr/}bin/{,u}mount ixr,
/{,usr/}sbin/pivot_root ixr,

# allow pivot_root'ing into the rootfs prepared for the greengrass daemon
# parallel-installs: SNAP_{DATA,COMMON} are remapped, need to use SNAP_NAME, for
# completeness allow SNAP_INSTANCE_NAME too
pivot_root
	oldroot=/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/old_rootfs/
	/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/,

# miscellaneous accesses by greengrassd
/sys/devices/virtual/block/loop[0-9]*/loop/autoclear r,
/sys/devices/virtual/block/loop[0-9]*/loop/backing_file r,

# greengrassd needs protected hardlinks, symlinks, etc to run securely, but
# won't turn them on itself, hence only read access for these things -
# the user is clearly informed if these are disabled and so the user can
# enable these themselves rather than give the snap permission to turn these
# on
@{PROC}/sys/fs/protected_hardlinks r,
@{PROC}/sys/fs/protected_symlinks r,
@{PROC}/sys/fs/protected_fifos r,
@{PROC}/sys/fs/protected_regular r,

# mount tries to access this, but it doesn't really need it
deny /run/mount/utab rw,

# these accesses are needed in order to mount a squashfs file for the rootfs
# note that these accesses allow reading other snaps and thus grants device control
/dev/loop-control rw,
/dev/loop[0-9]* rw,
/sys/devices/virtual/block/loop[0-9]*/ r,
/sys/devices/virtual/block/loop[0-9]*/** r,

# mount for mounting the rootfs which is a squashfs image inside $SNAP_DATA/rootfs
mount options=ro /dev/loop[0-9]* -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/,

# generic mounts for allowing anything inside $SNAP_DATA to be remounted anywhere else inside $SNAP_DATA
# parallel-installs: SNAP_{DATA,COMMON} are remapped, need to use SNAP_NAME, for
# completeness allow SNAP_INSTANCE_NAME too
mount options=(rw, bind) /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** ,
mount options=(rw, rbind) /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** ,
# also allow mounting new files anywhere underneath the rootfs of the target
# overlayfs directory, which is the rootfs of the container
# this is for allowing local resource access which first makes a mount at
# the target destination and then a bind mount from the source to the destination
# the source destination mount will be allowed under the above rule
mount -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/**,

# specific mounts for setting up the mount namespace that greengrassd runs inside
mount options=(rw, bind) /proc/ -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/proc/,
mount /sys -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/sys/,
mount options=(rw, bind) /dev/ -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/dev/,
mount options=(rw, bind) /{,var/}run/ -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/{,var/}run/,
mount options=(rw, nosuid, strictatime) fstype=tmpfs tmpfs -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/dev/,
# note that we don't mount a new tmpfs here so that everytime we run and setup
# the mount ns for greengrassd it uses the same tmpfs which will be the tmpfs
# that snapd sets up for the snap
mount options=(rw, bind) /tmp/ -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/tmp/,
mount options=(rw, nosuid, nodev, noexec) fstype=mqueue mqueue -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/dev/mqueue/,
mount options=(rw, nosuid, noexec) fstype=devpts devpts -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/dev/pts/,
mount options=(rw, nosuid, nodev, noexec) fstype=tmpfs shm -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/dev/shm/,
mount fstype=proc proc -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/proc/,

# mounts for setting up child container rootfs
mount options=(rw, rprivate) -> /,
mount options=(ro, remount, rbind) -> /,
mount fstype=overlay -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/,

# for jailing the process by removing the rootfs when the overlayfs is setup
umount /,

# mounts greengrassd performs for the containers
mount fstype="tmpfs" options=(rw, nosuid, strictatime) tmpfs -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/,
mount fstype="proc" proc -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/proc/,
mount fstype="devpts" options=(rw, nosuid, noexec) devpts -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/pts/,
mount fstype="tmpfs" options=(rw, nosuid, nodev, noexec) shm -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/shm/,
mount fstype="mqueue" options=(rw, nosuid, nodev, noexec) mqueue -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/mqueue/,
mount options=(ro, remount, bind) -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/lambda/,
mount options=(ro, remount, bind) -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/runtime/,
mount options=(rw, bind) /dev/null -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/null,
mount options=(rw, bind) /dev/random -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/random,
mount options=(rw, bind) /dev/full -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/full,
mount options=(rw, bind) /dev/tty -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/tty,
mount options=(rw, bind) /dev/zero -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/zero,
mount options=(rw, bind) /dev/urandom -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/dev/urandom,

# mounts for /run in the greengrassd mount namespace
mount options=(rw, bind) /run/ -> /run/,

# mounts for resolv.conf inside the container
# we have to manually do this otherwise the go DNS resolver fails to work, because it isn't configured to
# use the system DNS server and attempts to do DNS resolution itself, manually inspecting /etc/resolv.conf
mount options=(ro, bind) /run/systemd/resolve/stub-resolv.conf -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/etc/resolv.conf,
mount options=(ro, bind) /run/resolvconf/resolv.conf -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/etc/resolv.conf,
mount options=(ro, remount, bind) -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/rootfs/etc/resolv.conf,

# pivot_root for the container initialization into the rootfs
# note that the actual syscall is pivotroot(".",".")
# so the oldroot is the same as the new root
pivot_root
	oldroot=/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/
	/var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/ggc-writable/packages/*/rootfs/merged/,

# mounts for /proc
mount options=(ro, remount) -> /proc/{asound/,bus/,fs/,irq/,sys/,sysrq-trigger},
mount options=(ro, remount, rbind) -> /proc/{asound/,bus/,fs/,irq/,sys/,sysrq-trigger},
mount options=(ro, nosuid, nodev, noexec, remount, rbind) -> /proc/{asound/,bus/,fs/,irq/,sys/,sysrq-trigger},
mount options=(rw, bind) /proc/asound/ -> /proc/asound/,
mount options=(rw, bind) /proc/bus/ -> /proc/bus/,
mount options=(rw, bind) /proc/fs/ -> /proc/fs/,
mount options=(rw, bind) /proc/irq/ -> /proc/irq/,
mount options=(rw, bind) /proc/sys/ -> /proc/sys/,
mount options=(rw, bind) /proc/sysrq-trigger -> /proc/sysrq-trigger,

# mount some devices using /dev/null
mount options=(rw, bind) /dev/null -> /proc/kcore,
mount options=(rw, bind) /dev/null -> /proc/sched_debug,
mount options=(rw, bind) /dev/null -> /proc/timer_stats,

# greengrass will also mount over /proc/latency_stats when running on
# kernels configured with CONFIG_LATENCYTOP set
mount options=(rw, bind) /dev/null -> /proc/latency_stats,

# umounts for tearing down containers
umount /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/*/**,

# this is for container device creation
# also need mknod and mknodat in seccomp
capability mknod,

# for the greengrassd pid file
# note we can't use layouts for this because /var/run is a symlink to /run
# and /run is explicitly disallowed for use by layouts
# also note that technically this access is post-pivot_root, but during the setup
# for the mount ns that the snap performs (not snapd), /var/run is bind mounted
# from outside the pivot_root to inside the pivot_root, so this will always
# access the same files inside or outside the pivot_root
owner /{var/,}run/greengrassd.pid rw,

# all of the rest of the accesses are made by child containers and as such are
# "post-pivot_root", meaning that they aren't accessing these files on the
# host root filesystem, but rather somewhere inside $SNAP_DATA/rootfs/
# Note: eventually greengrass will gain the ability to specify child profiles
# for it's containers and include these rules in that profile so they won't
# be here, but that work isn't done yet
# Additionally see LP bug #1791711 for apparmor resolving file accesses after
# a pivot_root

# for IPC communication via lambda helpers
/[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f]-[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]/upper/{,greengrass_ipc.sock} rw,

# for child container lambda certificates
/certs/ r,
/certs/** r,
/group/ r,
/group/** r,
/state/ r,
/state/{,**} krw,
# the child containers need to use a file lock here
owner /state/secretsmanager/secrets.db krw,
owner /state/secretsmanager/secrets.db-journal rw,
owner /state/shadow/ rw,
owner /state/shadow/{,**} krw,
# more specific accesses for writing
owner /state/server/ rw,
owner /state/server/{,**} rw,

# for executing python, nodejs, java, and C (executable) lambda functions
# currently the runtimes are "python2.7", "nodejs6.10", "java8" and "executable",
# but those version numbers could change so we add a "*" on the end of the folders to be safe for
# future potential upgrades
/runtime/{python*,executable*,nodejs*,java*}/ r,
/runtime/{python*,executable*,nodejs*,java*}/** r,

# Ideally we would use a child profile for these but since the greengrass
# sandbox is using prctl(PR_SET_NO_NEW_PRIVS, ...) we cannot since that blocks
# profile transitions. With policy stacking we could use a more restrictive
# child profile, but there are bugs which prevent that at this time
# (LP: #1696552, LP: #1696551). As such, must simply rely on the greengrass
# sandbox for now.
/lambda/ r,
/lambda/** ixr,

# needed by cloneBinary.ensureSelfCloned()
/ ix,

# the python runtime tries to access /etc/debian_version, presumably to identify what system it's running on
# note there may be other accesses that the containers try to run...
/etc/ r,
/etc/debian_version r,
#include <abstractions/python>
# additional accesses needed for newer pythons in later bases
/usr/lib{,32,64}/python3.[0-9]/**.{pyc,so}           mr,
/usr/lib{,32,64}/python3.[0-9]/**.{egg,py,pth}       r,
/usr/lib{,32,64}/python3.[0-9]/{site,dist}-packages/ r,
/usr/lib{,32,64}/python3.[0-9]/lib-dynload/*.so      mr,
/etc/python3.[0-9]/**                                r,
/usr/include/python3.[0-9]*/pyconfig.h               r,

# manually add java certs here
# see also https://bugs.launchpad.net/apparmor/+bug/1816372
/etc/ssl/certs/java/{,*} r,
#include <abstractions/ssl_certs>
`

const greengrassSupportConnectedPlugAppArmorUserNS = `
# allow use of user namespaces
userns,
`

const greengrassSupportConnectedPlugSeccomp = `
# Description: can manage greengrass 'things' and their sandboxes. This
# policy is intentionally not restrictive and is here to help guard against
# programming errors and not for security confinement. The greengrassd
# daemon by design requires extensive access to the system and
# cannot be effectively confined against malicious activity.

# allow use of ggc_user and ggc_group
# FIXME: seccomp arg filter by this uid/gid when supported by snap-confine
lchown
lchown32
fchown
fchown32
fchownat
setgroups
setgroups32

# for creating a new mount namespace for the containers
setns - CLONE_NEWNS
unshare

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

# special character device creation is necessary for creating the overlayfs
# mounts
# Unfortunately this grants device ownership to the snap.
mknod - |S_IFCHR -
mknodat - - |S_IFCHR -
`

func (iface *greengrassSupportInterface) ServicePermanentPlug(plug *snap.PlugInfo) []string {
	var flavor string
	_ = plug.Attr("flavor", &flavor)

	// only no-container flavor does not get Delegate=true, all other flavors
	// (including no flavor, which is the same as legacy-container flavor)
	// are usable to manage control groups of processes/containers, and thus
	// need Delegate=true
	if flavor == "no-container" {
		return nil
	}

	return []string{"Delegate=true"}
}

func (iface *greengrassSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// check the flavor
	var flavor string
	_ = plug.Attr("flavor", &flavor)
	switch flavor {
	case "", "legacy-container":
		// default, legacy version of the interface
		if release.OnClassic {
			spec.AddSnippet(greengrassSupportFullContainerConnectedPlugAppArmor)
		} else {
			spec.AddSnippet(greengrassSupportFullContainerConnectedPlugAppArmor + greengrassSupportConnectedPlugAppArmorCore)
		}
		// greengrass needs to use ptrace for controlling it's containers
		spec.SetUsesPtraceTrace()
		// if apparmor supports userns mediation then add this too as we
		// allow unshare in the seccomp profile in this flavor
		if apparmor_sandbox.ProbedLevel() != apparmor_sandbox.Unsupported {
			features := mylog.Check2(apparmor_sandbox.ParserFeatures())

			if strutil.ListContains(features, "userns") {
				spec.AddSnippet(greengrassSupportConnectedPlugAppArmorUserNS)
			}
		}
	case "no-container":
		// this is the no-container version, it does not use as much privilege
		// as the default "legacy-container" flavor
		spec.AddSnippet(greengrassSupportProcessModeConnectedPlugAppArmor)
	}

	return nil
}

func (iface *greengrassSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// check the flavor
	var flavor string
	_ = plug.Attr("flavor", &flavor)
	switch flavor {
	case "", "legacy-container":
		spec.AddSnippet(greengrassSupportConnectedPlugSeccomp)
	case "no-container":
		// no-container has no additional seccomp available to it
	}

	return nil
}

func (iface *greengrassSupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var flavor string
	_ = plug.Attr("flavor", &flavor)
	switch flavor {
	case "", "legacy-container":
		// default containerization controls the device cgroup
		spec.SetControlsDeviceCgroup()
	case "no-container":
		// no-container does not control the device cgroup
	}

	return nil
}

type greengrassSupportInterface struct {
	commonInterface
}

func init() {
	registerIface(&greengrassSupportInterface{commonInterface{
		name:                 "greengrass-support",
		summary:              greengrassSupportSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: greengrassSupportBaseDeclarationSlots,
		baseDeclarationPlugs: greengrassSupportBaseDeclarationPlugs,
	}})
}
