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

/usr/bin/systemd-run Cxr -> systemd_run,
profile systemd_run (attach_disconnected,mediate_deleted) {
  # Common rules for kubernetes use of systemd_run
  #include <abstractions/base>

  /{,usr/}bin/systemd-run rm,
  owner @{PROC}/@{pid}/stat r,
  owner @{PROC}/@{pid}/environ r,
  @{PROC}/cmdline r,
  @{PROC}/sys/kernel/osrelease r,
  @{PROC}/1/sched r,

  # setsockopt()
  capability net_admin,

  # ptrace 'trace' is coarse and not required for using the systemd private
  # socket, and while the child profile omits 'capability sys_ptrace', skip
  # for now since it isn't strictly required.
  ptrace read peer=unconfined,
  deny ptrace trace peer=unconfined,
  /run/systemd/private rw,

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
@{PROC}/[0-9]*/ns/ r,

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
deny /run/mount/utab rw,
umount /var/snap/@{SNAP_INSTANCE_NAME}/common/**,
`

const kubernetesSupportConnectedPlugAppArmorKubeletSystemdRun = `
  # kubelet mount rules
  capability sys_admin,
  /{,usr/}bin/mount ixr,
  mount fstype="tmpfs" tmpfs -> /var/snap/@{SNAP_INSTANCE_NAME}/common/**,
  deny /run/mount/utab rw,

  # For mounting volume subPaths
  mount /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**} -> /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**},
  mount options=(rw, remount, bind) -> /var/snap/@{SNAP_INSTANCE_NAME}/common/{,**},
  umount /var/snap/@{SNAP_INSTANCE_NAME}/common/**,
  # When mounting a volume subPath, kubelet binds mounts on an open fd (eg,
  # /proc/.../fd/N) which triggers a ptrace 'trace' denial on the parent
  # kubelet peer process from this child profile. Note, this child profile
  # doesn't have 'capability sys_ptrace', so systemd-run is still not able to
  # ptrace this snap's processes.
  ptrace (trace) peer=snap.@{SNAP_INSTANCE_NAME}.@{SNAP_COMMAND_NAME},
`

const kubernetesSupportConnectedPlugSeccompKubelet = `
# Allow running as the kubelet service
mount
umount
umount2

unshare
setns - CLONE_NEWNET
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

func k8sFlavor(plug *interfaces.ConnectedPlug) string {
	var flavor string
	_ = plug.Attr("flavor", &flavor)
	return flavor
}

func (iface *kubernetesSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	snippet := kubernetesSupportConnectedPlugAppArmorCommon
	systemd_run_extra := ""

	switch k8sFlavor(plug) {
	case "kubelet":
		systemd_run_extra = kubernetesSupportConnectedPlugAppArmorKubeletSystemdRun
		snippet += kubernetesSupportConnectedPlugAppArmorKubelet
		spec.SetUsesPtraceTrace()
	case "kubeproxy":
		snippet += kubernetesSupportConnectedPlugAppArmorKubeproxy
	default:
		systemd_run_extra = kubernetesSupportConnectedPlugAppArmorKubeletSystemdRun
		snippet += kubernetesSupportConnectedPlugAppArmorKubelet
		snippet += kubernetesSupportConnectedPlugAppArmorKubeproxy
		spec.SetUsesPtraceTrace()
	}

	old := "###KUBERNETES_SUPPORT_SYSTEMD_RUN###"
	spec.AddSnippet(strings.Replace(snippet, old, systemd_run_extra, -1))
	return nil
}

func (iface *kubernetesSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	flavor := k8sFlavor(plug)
	if flavor == "kubelet" || flavor == "" {
		snippet := kubernetesSupportConnectedPlugSeccompKubelet
		spec.AddSnippet(snippet)
	}
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
			if err := spec.AddModule(m); err != nil {
				return err
			}
		}
	}
	return nil
}

func (iface *kubernetesSupportInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// It's fine if flavor isn't specified, but if it is, it needs to be
	// either "kubelet" or "kubeproxy"
	if t, ok := plug.Attrs["flavor"]; ok && t != "kubelet" && t != "kubeproxy" {
		return fmt.Errorf(`kubernetes-support plug requires "flavor" to be either "kubelet" or "kubeproxy"`)
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
