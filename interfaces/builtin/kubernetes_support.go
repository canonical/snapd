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

const kubernetesSupportConnectedPlugAppArmor = `
# Description: can use kubernetes to control Docker containers. This interface
# is restricted because it gives wide ranging access to the host and other
# processes.

# what is this for?
#include <abstractions/dbus-strict>

# Allow reading all information regarding cgroups for all processes
capability sys_resource,
@{PROC}/@{pid}/cgroup r,
/sys/fs/cgroup/{,**} r,

# Allow adjusting the OOM score for Docker containers. Note, this allows
# adjusting for all processes, not just containers.
@{PROC}/@{pid}/oom_score_adj rw,
/sys/kernel/mm/hugepages/ r,

@{PROC}/sys/kernel/panic rw,
@{PROC}/sys/kernel/panic_on_oops rw,
@{PROC}/sys/kernel/keys/root_maxbytes r,
@{PROC}/sys/kernel/keys/root_maxkeys r,
@{PROC}/sys/vm/overcommit_memory rw,
@{PROC}/sys/vm/panic_on_oom r,

@{PROC}/diskstats r,
@{PROC}/@{pid}/cmdline r,

# Allow reading the state of modules kubernetes needs
/sys/module/llc/initstate r,
/sys/module/stp/initstate r,

# Allow listing kernel modules. Note, seccomp blocks module loading syscalls
/sys/module/apparmor/parameters/enabled r,
/bin/kmod ixr,
/etc/modprobe.d/{,**} r,

# Allow ptracing Docker containers
ptrace (read, trace) peer=docker-default,
ptrace (read, trace) peer=snap.docker.dockerd,

# Should we have a 'privileged' mode for kubernetes like we do with
# docker-support? Right now kubernetes needs this which allows it to control
# all processes on the system and on <4.8 kernels, escape confinement.
ptrace (read, trace) peer=unconfined,
`

var kubernetesSupportConnectedPlugKmod = []string{`llc`, `stp`}

func init() {
	registerIface(&commonInterface{
		name:                     "kubernetes-support",
		summary:                  kubernetesSupportSummary,
		implicitOnCore:           true,
		implicitOnClassic:        true,
		baseDeclarationPlugs:     kubernetesSupportBaseDeclarationPlugs,
		baseDeclarationSlots:     kubernetesSupportBaseDeclarationSlots,
		connectedPlugAppArmor:    kubernetesSupportConnectedPlugAppArmor,
		connectedPlugKModModules: kubernetesSupportConnectedPlugKmod,
		reservedForOS:            true,
	})
}
