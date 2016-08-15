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

package builtin

import (
	"github.com/snapcore/snapd/interfaces"
)

const dockerPermanentSlotAppArmor = `
# Description: Allow operating as the Docker daemon. Reserved because this
#  gives privileged access to the system.
# Usage: reserved

# Allow sockets
/{,var/}run/docker.sock rw,
/{,var/}run/docker/     rw,
/{,var/}run/docker/**   mrwklix,
/{,var/}run/runc/       rw,
/{,var/}run/runc/**     mrwklix,

# Wide read access to /proc, but somewhat limited writes for now
@{PROC}/ r,
@{PROC}/** r,
@{PROC}/[0-9]*/attr/exec w,
@{PROC}/sys/net/** w,
@{PROC}/[0-9]*/cmdline r,
@{PROC}/[0-9]*/oom_score_adj w,

# Wide read access to /sys
/sys/** r,
# Limit cgroup writes a bit
/sys/fs/cgroup/*/docker/   rw,
/sys/fs/cgroup/*/docker/** rw,

# Allow tracing ourself (especially the "runc" process we create)
ptrace (trace) peer=@{profile_name},

# Docker needs a lot of caps, but limits them in the app container
capability,

# Docker does all kinds of mounts all over the filesystem
/dev/mapper/control rw,
/dev/mapper/docker* rw,
/dev/loop* r,
/dev/loop[0-9]* w,
mount,
umount,

# After doing a pivot_root using <graph-dir>/<container-fs>/.pivot_rootNNNNNN,
# Docker removes the leftover /.pivot_rootNNNNNN directory (which is now
# relative to "/" instead of "<graph-dir>/<container-fs>" thanks to pivot_root)
pivot_root,
/.pivot_root[0-9]*/ rw,

# file descriptors (/proc/NNN/fd/X)
/[0-9]* rw,
# file descriptors in the container show up here due to attach_disconnected

# Docker needs to be able to create and load the profile it applies to containers ("docker-default")
# XXX We might be able to get rid of this if we generate and load docker-default ourselves and make docker not do it.
/sbin/apparmor_parser ixr,
/etc/apparmor.d/cache/ r,
/etc/apparmor.d/cache/.features r,
/etc/apparmor.d/cache/docker* rw,
/etc/apparmor/parser.conf r,
/etc/apparmor/subdomain.conf r,
/sys/kernel/security/apparmor/.replace rw,

# We'll want to adjust this to support --security-opts...
change_profile -> docker-default,
signal (send) peer=docker-default,
ptrace (read, trace) peer=docker-default,

# Graph (storage) driver bits
/dev/shm/aufs.xino rw,

#cf bug 1502785
/ r,
`

const dockerConnectedPlugAppArmor = `
# Description: Allow using Docker. Reserved because this gives
#  privileged access to the service/host.
# Usage: reserved

# Obviously need to be able to talk to the daemon
/{,var/}run/docker.sock rw,

@{PROC}/sys/net/core/somaxconn r,
`

const dockerPermanentSlotSecComp = `
# The Docker daemon needs to be able to launch arbitrary processes within containers (whose syscall needs are unknown beforehand)
@unrestricted
`

const dockerConnectedPlugSecComp = `
setsockopt
bind
`

type DockerInterface struct{}

func (iface *DockerInterface) Name() string {
	return "docker"
}

func (iface *DockerInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(dockerConnectedPlugAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(dockerConnectedPlugSecComp), nil
	case interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return []byte(dockerPermanentSlotAppArmor), nil
	case interfaces.SecuritySecComp:
		return []byte(dockerPermanentSlotSecComp), nil
	case interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	// The docker socket is a named socket and therefore mediated by AppArmor file rules and we can't currently limit connecting clients by their security label
	switch securitySystem {
	case interfaces.SecurityAppArmor, interfaces.SecurityDBus, interfaces.SecurityMount, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *DockerInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *DockerInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *DockerInterface) AutoConnect() bool {
	return false
}
