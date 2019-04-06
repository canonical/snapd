// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
)

const systemBackupSummary = `allows read-only access to the entire system for backups`

const systemBackupBaseDeclarationSlots = `
  system-backup:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const systemBackupConnectedPlugAppArmor = `
# Description: Allow read-only access to system data
capability dac_read_search,
/writable/{,**} r,
`

const systemBackupConnectedPlugAppArmorClassic = `
# Description: Allow read-only access to system data on classic

# read access to everything except items under /dev, /sys and /proc
/[^dsp]** r,
/{d[^e],s[^y],p[^r]}** r,
/{de[^v],sy[^s],pr[^o]}** r,
/{dev[^/],sys[^/],pro[^c]}** r,
/proc[^/]** r,

# Allow a few not caught in the above
/{d,de,p,pr,pro,s,sy}/** r,
/{d,de,dev,p,pr,pro,proc,s,sy,sys}{,/} r,
`

type systemBackupInterface struct {
	commonInterface
}

func (iface *systemBackupInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(systemBackupConnectedPlugAppArmor)
	if release.OnClassic {
		spec.AddSnippet(systemBackupConnectedPlugAppArmorClassic)
	}
	return nil
}

func init() {
	registerIface(&systemBackupInterface{commonInterface{
		name:                 "system-backup",
		summary:              systemBackupSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: systemBackupBaseDeclarationSlots,
		reservedForOS:        true,
	}})
}
