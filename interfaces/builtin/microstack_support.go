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
 */

const microStackSupportSummary = `allows operating as the MicroStack service`

const microStackSupportBaseDeclarationPlugs = `
  microstack-support:
    allow-installation: false
    allow-auto-connection: true
`

const microStackSupportBaseDeclarationSlots = `
  microstack-support:
    allow-installation:
      slot-snap-type:
        - core
    allow-auto-connection: true
`

const microStackSupportConnectedPlugAppArmor = `
/dev/vhost-net rw,
/dev/vhost-scsi rw,
/dev/vhost-vsock rw,
/dev/vfio/vfio rw,
/dev/nbd* rw,

# Description: this policy intentionally allows Microstack services to configure AppArmor
# as libvirt generates AppArmor profiles for the utility processes it spawns.
/sys/kernel/security/apparmor/{,**} r,
/sys/kernel/security/apparmor/.remove w,
/sys/kernel/security/apparmor/.replace w,

# promote_secondaries is used in every namespace created by Neutron agents (including neutron-ovn-agent)
# to enable promotion of secondary IPs to primary IPs on an interface (only for ipv4).
/proc/sys/net/ipv4/conf/all/promote_secondaries rw,

# Read sysctls used by ovs-vswitchd.
/proc/sys/kernel/osrelease r,
/proc/sys/net/core/netdev_max_backlog r,
# Used by libcap-ng loaded by Libvirt.
/proc/sys/kernel/cap_last_cap r,

# Used by libvirt to read information about a NUMA topology of a host.
/sys/devices/system/node/** r,
/sys/kernel/mm/hugepages/hugepages-*/** r,
/sys/kernel/iommu_groups/ r,
/sys/kernel/iommu_groups/** r,
/sys/bus/pci/devices/**/iommu_group/** r,

# Used by libvirt's QEMU driver state initialization code path.
# The path used is hard-coded in libvirt to <huge-page-mnt-dir>/libvirt/qemu.
/dev/hugepages/libvirt/ rw,
/dev/hugepages/libvirt/** mrwklix,

# Used by libvirt to read information about available CPU and memory resources of a host.
/sys/devices/system/cpu/ r,
/sys/devices/system/node/ r,
/sys/devices/system/node/node[0-9]*/meminfo r,
/sys/devices/system/node/node[0-9]*/hugepages/** r,

/sys/module/vhost/parameters/max_mem_regions r,

# Allow nested virtualization checks for different CPU models and architectures (where it is supported).
/sys/module/kvm_intel/parameters/nested r,
/sys/module/kvm_amd/parameters/nested r,
/sys/module/kvm_hv/parameters/nested r, # PPC64.
/sys/module/kvm/parameters/nested r, # S390.

# Used by libvirt (cgroup-related):
/sys/fs/cgroup/** rw,
/sys/fs/cgroup/*/machine/qemu-*/** rw,
/sys/fs/cgroup/unified/cgroup.controllers r,
/proc/self/cgroup r,
/proc/cgroups r,

# Used by libvirt.
/proc/cmdline r,
/proc/filesystems r,
/proc/mtrr w,
/proc/@{pids}/environ r,
/proc/@{pids}/mountinfo r,
/proc/@{pids}/mounts r,
/proc/@{pids}/sched r,
/proc/@{pids}/stat r,

# Per man(5) proc, the kernel enforces that a thread may
# only modify its comm value or those in its thread group.
owner /proc/@{pid}/task/@{tid}/comm rw,

/proc/*/cmdline r,
/proc/sys/net/** r,
/proc/version r,
/proc/*/stat r,
/proc/*/mounts r,
/proc/*/status r,
/proc/*/ns/net r,


# Used by libvirt to work with network devices created for VMs.
# E.g. getting operational state and speed of tap devices.
/sys/class/net/tap*/* rw,

# for usb access
/dev/bus/usb/ r,
/sys/bus/ r,
/sys/class/ r,
# For hostdev access. The actual devices will be added dynamically
/sys/bus/usb/devices/ r,
/sys/devices/**/usb[0-9]*/** r,
# libusb needs udev data about usb devices (~equal to content of lsusb -v)
/run/udev/data/+usb* r,
/run/udev/data/c16[6,7]* r,
/run/udev/data/c18[0,8,9]* r,

# Libvirt needs access to the PCI config space in order to be able to reset devices.
/sys/devices/pci*/**/config rw,

# for vfio hotplug on systems without static vfio (LP: #1775777)
/dev/vfio/vfio rw,
/dev/vfio/* rw,

# Allow querying the max number of segments for a block device by Libvirt
# (see QEMU commit 9103f1ce and Linux/block/queue-sysfs.txt).
/sys/devices/**/block/*/queue/max_segments r,

# required by libpmem init to fts_open()/fts_read() the symlinks in
# /sys/bus/nd/devices
/ r, # harmless on any lsb compliant system
/sys/bus/nd/devices/{,**/} r,

# For ppc device-tree access by Libvirt.
/proc/device-tree/ r,
/proc/device-tree/** r,
/sys/firmware/devicetree/** r,

# "virsh console" support
/dev/pts/* rw,
# Used by libvirt.
/dev/ptmx rw,
# spice
owner /{dev,run}/shm/spice.* rw,

# Used by libvirt to create lock files for /dev/pts/<num> devices
# when handling virsh console access requests.
/run/lock/ r,
/run/lock/snap.@{SNAP_INSTANCE_NAME}/ rw,
/run/lock/snap.@{SNAP_INSTANCE_NAME}/** mrwklix,

# allow connect with openGraphicsFD to work
unix (send, receive) type=stream addr=none peer=(label=libvirtd),
unix (send, receive) type=stream addr=none peer=(label=/snap/microstack/current/usr/sbin/libvirtd),

# Allow running utility processes under the specialized AppArmor profiles.
# These profiles will prevent utility processes escaping confinement.
capability mac_admin,

# MicroStack services such as libvirt use has a server/client design where
# unix sockets are used for IPC.
capability chown,

# MicroStack will also use privilege separation when running utility processes
capability setuid,
capability setgid,

# Required by Nova and Neutron (e.g. to create network namespaces), see oslo.privsep docs.
capability sys_admin,
# Required by Neutron and OpenvSwitch.
capability net_admin,
capability net_broadcast,
# Required by Nova.
capability dac_override,
capability dac_read_search,
capability fowner,

# Used by libvirt to alter process capabilities via prctl.
capability setpcap,
# Used by libvirt to create device special files.
capability mknod,
# Used by libvirt to write log messages to the kernel audit log.
capability audit_write,
# Used by QEMU for memory locking.
capability ipc_lock,

# Used my mysql.
capability sys_nice,

# ptrace
capability sys_ptrace,

# Profiles Microstack generates have a naming scheme, restrict any profile changes to
# those matching that scheme. Need unsafe to prevent env scrubbing.
change_profile unsafe /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/usr/bin/* -> microstack.*,
signal (send) peer=microstack.*,
ptrace (read, trace) peer=microstack.*,

network inet stream,
network inet6 stream,

# Used by neutron-ovn-agent.
/run/netns/ r,
unmount /run/netns/ovnmeta-*,
`

const microStackSupportConnectedPlugSecComp = `
# Description: allow MicroStack to operate by allowing the necessary system calls to be used by various services.
# (libvirt, qemu, qemu-img, dnsmasq, Nova, Neutron, Keystone, Glance, Cinder)

# Note that this profile necessarily contains the union of all the syscalls each of the
# utilities requires. We rely on MicroStack to generate specific AppArmor profiles
# for each child process, to further restrict their abilities.

chown
chown32
fchown
fchown32
fchownat
lchown
lchownat
`
var microStackConnectedPlugUDev = []string{
	`KERNEL=="vhost-net"`,
	`KERNEL=="vhost-scsi"`,
	`KERNEL=="vhost-vsock"`,
	`KERNEL=="tun"`,
}

type microStackInterface struct {
	commonInterface
}

var microStackSupportConnectedPlugKmod = []string{`vhost`, `vhost-net`, `vhost-scsi`, `vhost-vsock`, `pci-stub`, `vfio`, `nbd`}

func init() {
	registerIface(&microStackInterface{commonInterface{
		name:                  "microstack-support",
		summary:               microStackSupportSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  microStackSupportBaseDeclarationSlots,
		baseDeclarationPlugs:  microStackSupportBaseDeclarationPlugs,
		connectedPlugAppArmor: microStackSupportConnectedPlugAppArmor,
		connectedPlugSecComp:  microStackSupportConnectedPlugSecComp,
		connectedPlugUDev:     microStackConnectedPlugUDev,
		connectedPlugKModModules: microStackSupportConnectedPlugKmod,
	}})
}
