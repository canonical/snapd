// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

/*
 * Microstack is a full OpenStack in a single snap package.
 * Virtual machines are spawned as QEMU processes with libvirt acting as a management
 * daemon (including for activities such as applying AppArmor profiles).
 * Networking is provided largely via OpenVSwitch and Neutron with dnsmasq acting
 * as an auxiliary daemon. tun/tap kernel module is used for creating virtual interfaces.
 * Virtual machines rely on KVM for virtualization acceleration and on vhost
 * framework in the kernel (vhost_net, vhost_scsi, vhost_vsock).
 *
 * This interface uses the controlsDeviceCgroup flag (which implies
 * `Delegate=true` on the systemd unit) since the snap already manages the
 * cgroup configuration of its containers.
 */

const microStackSupportSummary = `allows operating as the MicroStack service`

const microStackSupportBaseDeclarationPlugs = `
  microstack-support:
    allow-installation: false
    deny-auto-connection: true
`

const microStackSupportBaseDeclarationSlots = `
  microstack-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const microStackSupportConnectedPlugAppArmor = `

# Used by QEMU to work with the kernel-side virtio implementations.
/dev/vhost-net rw,
/dev/vhost-scsi rw,
/dev/vhost-vsock rw,
# Used by QEMU to work with VFIO (https://www.kernel.org/doc/Documentation/vfio.txt).
# For vfio hotplug on systems without static vfio (LP: #1775777)
# VFIO userspace driver interface.
/dev/vfio/vfio rw,
# Access to VFIO group character devices such as /dev/vfio/<group> where <group> is the group number.
/dev/vfio/* rw,
# Used by Nova for mounting images via qemu-nbd.
/dev/nbd* rw,

# Allow creating dm-* devices, /dev/<vg-name> directories, /dev/mapper directory and symlinks under it.
# Allow issuing ioctls to the Device Mapper for LVM tools via /dev/mapper/control.
/dev/mapper/control rw,
# Besides symlinks for LVs prefixed with a VG name this is also needed for DM devices created with
# dm-crypt and other DM modules.
/dev/mapper/{,**} rw,
# Allow device mapper devices to be accessed.
/dev/dm-* rw,
/dev/microstack-*/{,**} rw,
# Allow bcache devices to be accessed since DM devices may be set up on top of those.
/dev/bcache[0-9]{,[0-9],[0-9][0-9]} rw,                   # bcache (up to 1000 devices)

# Allow access to loop devices and loop-control to be able to associate a file with a loop device
# for the purpose of using a file-backed LVM setup.
/dev/loop-control rw,
/dev/loop[0-9]* rw,

# Description: this policy intentionally allows Microstack services to configure AppArmor
# as libvirt generates AppArmor profiles for the utility processes it spawns.
/sys/kernel/security/apparmor/{,**} r,
/sys/kernel/security/apparmor/.remove w,
/sys/kernel/security/apparmor/.replace w,

# Used by libvirt to work with IOMMU.
/sys/kernel/iommu_groups/{,**} r,
/sys/bus/pci/devices/**/iommu_group/** r,

# Used by libvirt's QEMU driver state initialization code path.
# The path used is hard-coded in libvirt to <huge-page-mnt-dir>/libvirt/qemu.
/dev/hugepages/libvirt/ rw,
/dev/hugepages/libvirt/** mrwklix,

# Used by QEMU to get the maximum number of memory regions allowed in the vhost kernel module.
/sys/module/vhost/parameters/max_mem_regions r,

# Used by libvirt (cgroup-related):
/sys/fs/cgroup/unified/cgroup.controllers r,
/sys/fs/cgroup/cpuset/cpuset.cpus r,

# Non-systemd layout: https://libvirt.org/cgroups.html#currentLayoutGeneric
/sys/fs/cgroup/*/ r,
/sys/fs/cgroup/*/machine/ rw,
/sys/fs/cgroup/*/machine/** rw,

# systemd-layout: https://libvirt.org/cgroups.html#systemdLayout
/sys/fs/cgroup/*/machine.slice/machine-qemu*/{,**} rw,

@{PROC}/[0-9]*/cgroup r,
@{PROC}/cgroups r,

# Used by libvirt.
@{PROC}/filesystems r,
@{PROC}/mtrr w,
@{PROC}/@{pids}/environ r,
@{PROC}/@{pids}/sched r,
@{PROC}/@{pids}/task/@{tid}/sched r,
@{PROC}/@{pids}/task/@{tid}/schedstat r,

@{PROC}/*/status r,

@{PROC}/sys/fs/nr_open r,

# Libvirt needs access to the PCI config space in order to be able to reset devices.
/sys/devices/pci*/**/config rw,

# Spice
owner /{dev,run}/shm/spice.* rw,

# Used by libvirt to create lock files for /dev/pts/<num> devices
# when handling virsh console access requests.
/run/lock/ r,
/run/lock/LCK.._pts_* rwk,

# Used by LVM tools.
/run/lock/lvm/ rw,
/run/lock/lvm/** rwk,
# Files like /run/lvm/pvs_online, /run/lvm/vgs_online, /run/lvm/hints
/run/lvm/ rw,
/run/lvm/** rwlk,
/run/dmeventd-client rwlk,
/run/dmeventd-server rwlk,

# Used by targetcli tools to work with LIO.
/sys/kernel/config/target/ rw,
/sys/kernel/config/target/** rw,

# Used by targetcli.
/{var/,}run/targetcli.lock rwlk,

# Paths accessed by iscsid during its operation.
/run/lock/iscsi/ rw,
/run/lock/iscsi/** rwlk,
/sys/devices/virtual/iscsi_transport/tcp/** r,
/sys/devices/virtual/iscsi_transport/iser/** r,
/sys/class/iscsi_session/** rw,
/sys/class/iscsi_host/** r,
/sys/devices/platform/host*/scsi_host/host*/** rw,
/sys/devices/platform/host*/session*/connection*/iscsi_connection/connection*/** rw,
/sys/devices/platform/host*/session*/iscsi_session/session*/** rw,
/sys/devices/platform/host*/session*/target*/** rw,
/sys/devices/platform/host*/iscsi_host/host*/** rw,

# While the block-devices interface allows rw access, Libvirt also needs to be able to lock those.
/dev/sd{,[a-h]}[a-z] rwk,
/dev/sdi[a-v] rwk,
# os-brick needs access to those when detaching a scsi device from an instance.
/sys/block/sd{,[a-h]}[a-z]/device/delete rw,
/sys/block/sdi[a-v]/device/delete rw,

# Used by open-iscsi to avoid being killed by the OOM killer.
owner @{PROC}/@{pid}/oom_score_adj rw,


# Allow running utility processes under the specialized AppArmor profiles.
# These profiles will prevent utility processes escaping confinement.
capability mac_admin,

# MicroStack services such as libvirt use a server/client design where
# unix sockets are used for IPC.
capability chown,

# Required by Nova.
capability dac_override,
capability dac_read_search,
capability fowner,

# Used by libvirt to alter process capabilities via prctl.
capability setpcap,
# Used by libvirt to create device special files.
capability mknod,

# Allow libvirt to apply policy to spawned VM processes.
change_profile -> libvirt-[0-9a-f]*-[0-9a-f]*-[0-9a-f]*-[0-9a-f]*-[0-9a-f]*,

# Allow sending signals to the spawned VM processes.
signal (read, send) peer=libvirt-*,

# Allow reading certain proc entries, see ptrace(2) "Ptrace access mode checking".
# For ourselves:
ptrace (read, trace) peer=@{profile_name},
# For VM processes libvirt spawns:
ptrace (read, trace) peer=libvirt-*,

# Used by neutron-ovn-agent.
unmount /run/netns/ovnmeta-*,
`

const microStackSupportConnectedPlugSecComp = `
# Description: allow MicroStack to operate by allowing the necessary system calls to be used by various services.
# (libvirt, qemu, qemu-img, Nova, Neutron, Keystone, Glance, Cinder)

# Note that this profile necessarily contains the union of all the syscalls each of the
# utilities requires. We rely on MicroStack to generate specific AppArmor profiles
# for each child process, to further restrict their abilities.
mknod - |S_IFBLK -
mknodat - - |S_IFBLK -
`

type microStackInterface struct {
	commonInterface
}

var microStackSupportConnectedPlugKmod = []string{
	`vhost`,           // Core vhost module.
	`vhost-net`,       // Used to offload virtio interface data plane into the kernel module.
	`vhost-scsi`,      // Used to offload virtio-scsi device data plane into the kernel module.
	`vhost-vsock`,     // virtio-vsock device support.
	`pci-stub`,        // May be used for binding a PCI device driver to a stub driver.
	`vfio`,            // The core VFIO driver for secure device assignment https://www.kernel.org/doc/html/latest/driver-api/vfio.html
	`vfio-pci`,        // PCI-specific VFIO functionality.
	`nbd`,             // The Network Block Device driver used by Nova (e.g. for block live migration).
	`dm-mod`,          // Device mapper.
	`dm-thin-pool`,    // DM thin pools used by the LVM driver in Cinder.
	`dm-snapshot`,     // DM snapshots used by the LVM driver in Cinder.
	`iscsi-tcp`,       // A module providing iscsi initiator functionality used by Nova via os-brick.
	`target-core-mod`, // A module providing ConfigFS infrastructure utilized in LIO (which is used by Cinder for iSCSI targets).
}

func init() {
	registerIface(&microStackInterface{commonInterface{
		name:                     "microstack-support",
		summary:                  microStackSupportSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		controlsDeviceCgroup:     true,
		baseDeclarationSlots:     microStackSupportBaseDeclarationSlots,
		baseDeclarationPlugs:     microStackSupportBaseDeclarationPlugs,
		connectedPlugAppArmor:    microStackSupportConnectedPlugAppArmor,
		connectedPlugSecComp:     microStackSupportConnectedPlugSecComp,
		connectedPlugKModModules: microStackSupportConnectedPlugKmod,
		serviceSnippets:          []string{`Delegate=true`},
	}})
}
