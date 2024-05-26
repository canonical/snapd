// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"fmt"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const kubernetesSupportSummary = `allows operating as the Kubernetes service`

const kubernetesSupportBaseDeclarationPlugs = `
  kubernetes-support:
    allow-installation: false
    deny-auto-connection: true
`

const kubernetesSupportBaseDeclarationSlots = `
  kubernetes-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const kubernetesSupportConnectedPlugAppArmorCommon = `
# Common rules for running as a kubernetes node

# reading cgroups
capability sys_resource,
/sys/fs/cgroup/{,**} r,

# Allow adjusting the OOM score for containers. Note, this allows adjusting for
# all processes, not just containers.
@{PROC}/@{pid}/oom_score_adj rw,
@{PROC}/sys/vm/overcommit_memory rw,
/sys/kernel/mm/hugepages/{,**} r,
/sys/kernel/mm/transparent_hugepage/{,**} r,

capability dac_override,

# Lock file used by Calico's IPAM plugin. This is configurable via the
# (undocumented) "ipam_lock_file" configuration key:
# https://github.com/projectcalico/cni-plugin/blob/master/pkg/types/types.go
/{,var/}run/calico/ipam.lock rwk,

# manually add java certs here
# see also https://bugs.launchpad.net/apparmor/+bug/1816372
/etc/ssl/certs/java/{,*} r,
#include <abstractions/ssl_certs>


# some workloads like cilium may attempt to use tc to set up complex
# network traffic control, which in turn uses seqpacket
network alg seqpacket,

/{,usr/}bin/systemd-run Cxr -> systemd_run,
/run/systemd/private r,
profile systemd_run (attach_disconnected,mediate_deleted) {
  # Common rules for kubernetes use of systemd_run
  #include <abstractions/base>

  /{,usr/}bin/systemd-run rm,
  owner @{PROC}/@{pid}/stat r,
  owner @{PROC}/@{pid}/environ r,
  @{PROC}/cmdline r,  # proc_cmdline()

  # setsockopt()
  capability net_admin,

  # systemd-run's detect_container() looks at several files to determine if it
  # is running in a container.
  @{PROC}/sys/kernel/osrelease r,
  @{PROC}/1/sched r,
  /run/systemd/container r,

  # kubelet calls 'systemd-run --scope true' to determine if systemd is
  # available and usable for calling certain mount commands under transient
  # units as part of its lifecycle management. This requires ptrace 'read' on
  # unconfined since systemd-run will call its detect_container() which will
  # try to read /proc/1/environ. This is mediated via PTRACE_MODE_READ when
  # run within kubelet's namespace.
  ptrace (read) peer=unconfined,
  /run/systemd/private rw,

  # kubelet calling 'systemd-run --scope true' triggers this when kubelet is
  # run in a nested container (eg, under lxd).
  @{PROC}/1/cmdline r,

  # Ubuntu's ptrace patchset before (at least) 20.04 did not correctly evaluate
  # PTRACE_MODE_READ and policy required 'trace' instead of 'read'.
  # (LP: #1890848). This child profile doesn't have 'capability sys_ptrace', so
  # continue to allow this historic 'trace' rule on unconfined (which systemd
  # runs as) since systemd-run won't be able to ptrace this snap's processes.
  # This can be dropped once LP: #1890848 is fixed.
  ptrace (trace) peer=unconfined,

  /{,usr/}bin/true ixr,
  @{INSTALL_DIR}/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/@{SNAP_REVISION}/{,usr/}bin/true ixr,
###KUBERNETES_SUPPORT_SYSTEMD_RUN###
}
`

const kubernetesSupportConnectedPlugAppArmorKubelet = `
# Allow running as the kubelet service

# Ideally this would be snap-specific
/run/dockershim.sock rw,

# Ideally this would be snap-specific (it could if the control plane was a
# snap), but in deployments where the control plane is not a snap, it will tell
# flannel to use this path.
/run/flannel/{,**} rw,
/run/flannel/** k,

# allow managing pods' cgroups
/sys/fs/cgroup/*/kubepods/{,**} rw,

# kubelet can be configured to use the systemd cgroup driver which moves
# container processes into systemd-managed cgroups. This is now the recommended
# configuration since it provides a single cgroup manager (systemd) in an
# effort to achieve consistent views of resources.
/sys/fs/cgroup/*/systemd/{,system.slice/} rw,          # create missing dirs
/sys/fs/cgroup/*/systemd/system.slice/** r,
/sys/fs/cgroup/*/systemd/system.slice/cgroup.procs w,

# Allow tracing our own processes. Note, this allows seccomp sandbox escape on
# kernels < 4.8
capability sys_ptrace,
ptrace (trace) peer=snap.@{SNAP_INSTANCE_NAME}.*,

# Allow ptracing other processes (as part of ps-style process lookups). Note,
# the peer needs a corresponding tracedby rule. As a special case, disallow
# ptracing unconfined.
ptrace (trace),
deny ptrace (trace) peer=unconfined,

@{PROC}/[0-9]*/attr/ r,
@{PROC}/[0-9]*/fdinfo/ r,
@{PROC}/[0-9]*/map_files/ r,
@{PROC}/[0-9]*/ns/{,*} r,
# dac_read_search needed for lstat'ing non-root owned ns/* files
capability dac_read_search,

# kubernetes will verify and set panic and panic_on_oops to values it considers
# sane
@{PROC}/sys/kernel/panic w,
@{PROC}/sys/kernel/panic_on_oops w,
@{PROC}/sys/kernel/keys/root_maxbytes r,
@{PROC}/sys/kernel/keys/root_maxkeys r,

/dev/kmsg r,

# kubelet calls out to systemd-run for some mounts, but not all of them and not
# unmounts...
capability sys_admin,
mount /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**} -> /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**},
mount options=(rw, rshared) -> /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**},

/{,usr/}bin/mount ixr,
/{,usr/}bin/umount ixr,
deny /run/mount/utab{,.lock} rw,
umount /var/snap/@{SNAP_INSTANCE_NAME}/common/**,

# When fsGroup is set, the pod's volume will be recursively chowned with the
# setgid bit set on directories so new files will be owned by the fsGroup. See
# kubernetes pkg/volume/volume_linux.go:changeFilePermission()
capability fsetid,
`

const kubernetesSupportConnectedPlugAppArmorKubeletSystemdRun = `
  # kubelet mount rules
  capability sys_admin,
  /{,usr/}bin/mount ixr,
  mount fstype="tmpfs" tmpfs -> /var/snap/@{SNAP_INSTANCE_NAME}/common/**,
  deny /run/mount/utab{,.lock} rw,

  # For mounting volume subPaths
  mount /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**} -> /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**},
  mount options=(rw, remount, bind) -> /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**},
  # nvme0-99, 1-63 partitions with 1-63 optional namespaces
  mount /dev/nvme{[0-9],[1-9][0-9]}n{[1-9],[1-5][0-9],6[0-3]}{,p{[1-9],[1-5][0-9],6[0-3]}} -> /var/snap/@{SNAP_INSTANCE_NAME}/common/**,
  # SCSI sda-sdiv, 1-15 partitions
  mount /dev/sd{[a-z],[a-h][a-z],i[a-v]}{[1-9],1[0-5]} -> /var/snap/@{SNAP_INSTANCE_NAME}/common/**,
  # virtio vda-vdz, 1-63 partitions
  mount /dev/vd[a-z]{[1-9],[1-5][0-9],6[0-3]} -> /var/snap/@{SNAP_INSTANCE_NAME}/common/**,
  umount /var/snap/@{SNAP_INSTANCE_NAME}/common/**,

  # When mounting a volume subPath, kubelet binds mounts on an open fd (eg,
  # /proc/.../fd/N) which triggers a ptrace 'read' denial on the parent
  # kubelet peer process from this child profile due to PTRACE_MODE_READ (man
  # ptrace) checks.
  ptrace (read) peer=snap.@{SNAP_INSTANCE_NAME}.@{SNAP_COMMAND_NAME},

  # Ubuntu's ptrace patchset before (at least) 20.04 did not correctly evaluate
  # PTRACE_MODE_READ and policy required 'trace' instead of 'read'.
  # (LP: #1890848). This child profile doesn't have 'capability sys_ptrace', so
  # continue to allow this historic 'trace' rule on kubelet (our parent peer)
  # since systemd-run won't be able to ptrace this snap's processes (kubelet
  # would also need a corresponding tracedby rule). This can be dropped once
  # LP: #1890848 is fixed.
  ptrace (trace) peer=snap.@{SNAP_INSTANCE_NAME}.@{SNAP_COMMAND_NAME},
`

// k8s.io/apiserver/pkg/storage/etcd3/logger.go pulls in go-systemd via
// go.etcd.io/etcd/clientv3. See:
// https://github.com/coreos/go-systemd/blob/master/journal/journal.go#L211
const kubernetesSupportConnectedPlugAppArmorAutobindUnix = `
# Allow using the 'autobind' feature of bind() (eg, for journald via go-systemd)
# unix (bind) type=dgram addr=auto,
# TODO: when snapd vendors in AppArmor userspace, then enable the new syntax
# above which allows only "empty"/automatic addresses, for now we simply permit
# all addresses with SOCK_DGRAM type, which leaks info for other addresses than
# what docker tries to use
# see https://bugs.launchpad.net/snapd/+bug/1867216
unix (bind) type=dgram,
`

const kubernetesSupportConnectedPlugSeccompAutobindUnix = `
# Allow using the 'autobind' feature of bind() (eg, for journald).
bind
`

const kubernetesSupportConnectedPlugSeccompKubelet = `
# Allow running as the kubelet service
mount
umount
umount2

unshare
setns - CLONE_NEWNET

# When fsGroup is set, the pod's volume will be recursively chowned with the
# setgid bit set on directories so new files will be owned by the fsGroup. See
# kubernetes pkg/volume/volume_linux.go:changeFilePermission()
fchownat
`

var kubernetesSupportConnectedPlugUDevKubelet = []string{
	`KERNEL=="kmsg"`,
}

const kubernetesSupportConnectedPlugAppArmorKubeproxy = `
# Allow running as the kubeproxy service

# managing our own cgroup
/sys/fs/cgroup/*/kube-proxy/{,**} rw,

# Allow reading the state of modules kubernetes needs
/sys/module/libcrc32c/initstate r,
/sys/module/llc/initstate r,
/sys/module/stp/initstate r,
/sys/module/ip_vs/initstate r,
/sys/module/ip_vs_rr/initstate r,
/sys/module/ip_vs_sh/initstate r,
/sys/module/ip_vs_wrr/initstate r,
`

var kubernetesSupportConnectedPlugKmodKubeProxy = []string{
	`ip_vs_rr`,
	`ip_vs_sh`,
	`ip_vs_wrr`,
	`libcrc32c`,
	`llc`,
	`stp`,
}

type kubernetesSupportInterface struct {
	commonInterface
}

func (iface *kubernetesSupportInterface) ServicePermanentPlug(plug *snap.PlugInfo) []string {
	// only autobind-unix flavor does not get Delegate=true, all other flavors
	// are usable to manage control groups of processes/containers, and thus
	// need Delegate=true
	flavor := k8sFlavor(plug)
	if flavor == "autobind-unix" {
		return nil
	}

	return []string{"Delegate=true"}
}

func k8sFlavor(plug interfaces.Attrer) string {
	var flavor string
	_ = plug.Attr("flavor", &flavor)
	return flavor
}

func (iface *kubernetesSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := kubernetesSupportConnectedPlugAppArmorCommon
	systemd_run_extra := ""

	// All flavors should include the autobind-unix rules, but we break it
	// out so other k8s daemons can use this flavor without getting the
	// privileged rules.
	switch k8sFlavor(plug) {
	case "kubelet":
		systemd_run_extra = kubernetesSupportConnectedPlugAppArmorKubeletSystemdRun
		snippet += kubernetesSupportConnectedPlugAppArmorKubelet
		snippet += kubernetesSupportConnectedPlugAppArmorAutobindUnix
		spec.SetUsesPtraceTrace()
	case "kubeproxy":
		snippet += kubernetesSupportConnectedPlugAppArmorKubeproxy
		snippet += kubernetesSupportConnectedPlugAppArmorAutobindUnix
	case "autobind-unix":
		snippet = kubernetesSupportConnectedPlugAppArmorAutobindUnix
	default:
		systemd_run_extra = kubernetesSupportConnectedPlugAppArmorKubeletSystemdRun
		snippet += kubernetesSupportConnectedPlugAppArmorKubelet
		snippet += kubernetesSupportConnectedPlugAppArmorKubeproxy
		snippet += kubernetesSupportConnectedPlugAppArmorAutobindUnix
		spec.SetUsesPtraceTrace()
	}

	old := "###KUBERNETES_SUPPORT_SYSTEMD_RUN###"
	spec.AddSnippet(strings.Replace(snippet, old, systemd_run_extra, -1))
	return nil
}

func (iface *kubernetesSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// All flavors should include the autobind-unix rules, but we add the
	// privileged kubelet rules conditionally.
	snippet := kubernetesSupportConnectedPlugSeccompAutobindUnix
	flavor := k8sFlavor(plug)
	if flavor == "kubelet" || flavor == "" {
		snippet += kubernetesSupportConnectedPlugSeccompKubelet
	}
	spec.AddSnippet(snippet)
	return nil
}

func (iface *kubernetesSupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	flavor := k8sFlavor(plug)
	if flavor == "kubelet" || flavor == "" {
		for _, rule := range kubernetesSupportConnectedPlugUDevKubelet {
			spec.TagDevice(rule)
		}
	}
	return nil
}

func (iface *kubernetesSupportInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	flavor := k8sFlavor(plug)
	if flavor == "kubeproxy" || flavor == "" {
		for _, m := range kubernetesSupportConnectedPlugKmodKubeProxy {
			mylog.Check(spec.AddModule(m))
		}
	}
	return nil
}

func (iface *kubernetesSupportInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// It's fine if flavor isn't specified, but if it is, it needs to be
	// either "kubelet", "kubeproxy" or "autobind-unix"
	if t, ok := plug.Attrs["flavor"]; ok && t != "kubelet" && t != "kubeproxy" && t != "autobind-unix" {
		return fmt.Errorf(`kubernetes-support plug requires "flavor" to be either "kubelet", "kubeproxy" or "autobind-unix"`)
	}

	return nil
}

func init() {
	registerIface(&kubernetesSupportInterface{commonInterface{
		name:                 "kubernetes-support",
		summary:              kubernetesSupportSummary,
		implicitOnClassic:    true,
		implicitOnCore:       true,
		baseDeclarationPlugs: kubernetesSupportBaseDeclarationPlugs,
		baseDeclarationSlots: kubernetesSupportBaseDeclarationSlots,
	}})
}
