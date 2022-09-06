// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"io/ioutil"
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const udisks2SupportSummary = `allows operating as a udisks2 daemon`

const udisks2SupportBaseDeclarationPlugs = `
  udisks2-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

type udisks2SupportInterface struct {
	commonInterface
}

func (iface *udisks2SupportInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var udevFile string
	plug.Attr("udev-file", &udevFile)
	mountDir := plug.Snap().MountDir()
	data, err := ioutil.ReadFile(filepath.Join(mountDir, udevFile))
	if err != nil {
		return err
	}
	spec.AddSnippet(string(data))
	return nil
}

func (iface *udisks2SupportInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

// FIXME: whatever the configuration, udisks2 will *always* use
// "noudev" and "nosuid". We should capture that if we can do it
// without restricting other options.

const udisks2SupportConnectedPlugAppArmor = `
/{run/,}media/{,**} rw,
mount /dev/{dm-*,nvme*,vd*,hd*,sd*,mmcblk*,fd*,sr*} -> /{run/,}media/**,
umount /{run/,}media/**,

/dev/{dm-*,nvme*,vd*,hd*,sd*,mmcblk*,fd*,sr*} r,

# /etc/libblockdev/conf.d/ ?

/run/ rw,
/run/cryptsetup/{,**} rwk,
/run/mount/utab.lock rwk,
/run/mount/utab.* rw,
/run/mount/utab rw,
/run/udisks2/{,**} rw,
/sys/devices/**/block/**/uevent w,

/{usr/,}{sbin,bin}/dumpe2fs ixr,
/etc/crypttab r,

dbus (send)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member=Inhibit,

/run/systemd/seats/* r,
`

const udisks2SupportConnectedPlugSecComp = `
mount
umount2
`

func init() {
	registerIface(&udisks2SupportInterface{
		commonInterface: commonInterface{
			name:                  "udisks2-support",
			summary:               udisks2SupportSummary,
			implicitOnCore:        true,
			implicitOnClassic:     true,
			baseDeclarationPlugs:  udisks2SupportBaseDeclarationPlugs,
			connectedPlugAppArmor: udisks2SupportConnectedPlugAppArmor,
			connectedPlugSecComp:  udisks2SupportConnectedPlugSecComp,
		},
	})
}
