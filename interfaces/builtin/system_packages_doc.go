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

const systemPackagesDocSummary = `allows access to documentation of system packages`

const systemPackagesDocBaseDeclarationSlots = `
  system-packages-doc:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemPackagesDocConnectedPlugAppArmor = `
# Description: can access documentation of system packages.

/usr/share/doc/{,**} r,
/usr/share/libreoffice/help/{,**} r,
`

type systemPackagesDocInterface struct {
	commonInterface
}

func (iface *systemPackagesDocInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(systemPackagesDocConnectedPlugAppArmor)
	emit := spec.AddUpdateNSf
	emit("  # Mount documentation of system packages\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/doc/ -> /usr/share/doc/,\n")
	emit("  remount options=(bind, ro) /usr/share/doc/,\n")
	emit("  umount /usr/share/doc/,\n")
	emit("  mount options=(bind) /var/lib/snapd/hostfs/usr/share/libreoffice/help/ -> /usr/share/libreoffice/help/,\n")
	emit("  remount options=(bind, ro) /usr/share/libreoffice/help/,\n")
	emit("  umount /usr/share/libreoffice/help/,\n")
	// /usr/share/libreoffice may not exist, set up apparmor profile for
	// creating a mimic, assuming that at least /usr/share exists
	apparmor.GenWritableProfile(emit, "/usr/share/libreoffice/help", 3)
	return nil
}

func (iface *systemPackagesDocInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/doc",
		Dir:     "/usr/share/doc",
		Options: []string{"bind", "ro"},
	})
	spec.AddMountEntry(osutil.MountEntry{
		Name:    "/var/lib/snapd/hostfs/usr/share/libreoffice/help",
		Dir:     "/usr/share/libreoffice/help",
		Options: []string{"bind", "ro"},
	})
	return nil
}

func init() {
	registerIface(&systemPackagesDocInterface{
		commonInterface: commonInterface{
			name:                 "system-packages-doc",
			summary:              systemPackagesDocSummary,
			implicitOnClassic:    true,
			baseDeclarationSlots: systemPackagesDocBaseDeclarationSlots,
			// affects the plug snap because of mount backend
			affectsPlugOnRefresh: true,
		},
	})
}
