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

import (
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
)

const hostDocsSummary = `allows access to documentation stored on the host`

const hostDocsBaseDeclarationSlots = `
  host-docs:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const hostDocsConnectedPlugAppArmor = `
# Description: Can access documentation stored on the host in /usr/share/doc

/usr/share/doc/{,**} r,
`

type hostDocsInterface struct {
	commonInterface
}

func (iface *hostDocsInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(hostDocsConnectedPlugAppArmor)
	emit := spec.AddUpdateNSf
	emit("  # Mount host documentation\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/doc/ -> /usr/share/doc/,\n")
	emit("  remount options=(bind, ro) /usr/share/doc/,\n")
	emit("  umount /usr/share/doc/,\n")
	return nil
}

func (iface *hostDocsInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	return spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/doc",
		Dir:     "/usr/share/doc",
		Options: []string{"bind", "ro"},
	})
}

func init() {
	registerIface(&hostDocsInterface{
		commonInterface: commonInterface{
			name:                 "host-docs",
			summary:              hostDocsSummary,
			implicitOnClassic:    true,
			baseDeclarationSlots: hostDocsBaseDeclarationSlots,
		},
	})
}
