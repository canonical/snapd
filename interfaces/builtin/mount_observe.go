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

const mountObserveSummary = `allows reading mount table and quota information`

const mountObserveBaseDeclarationSlots = `
  mount-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/mount-observe
const mountObserveConnectedPlugAppArmor = `
# Description: Can query system mount and disk quota information. This is
# restricted because it gives privileged read access to mount arguments and
# should only be used with trusted apps.

/{,usr/}bin/df ixr,

# Needed by 'df'. This is an information leak
@{PROC}/mounts r,
owner @{PROC}/@{pid}/mounts r,
owner @{PROC}/@{pid}/mountinfo r,
owner @{PROC}/@{pid}/mountstats r,
/sys/devices/*/block/{,**} r,

@{PROC}/swaps r,

# This is often out of date but some apps insist on using it
/etc/mtab r,
/etc/fstab r,
`

const mountObserveConnectedPlugSecComp = `
# Description: Can query system mount and disk quota information. This is
# restricted because it gives privileged read access to mount arguments and
# should only be used with trusted apps.

quotactl Q_GETQUOTA - - -
quotactl Q_GETINFO - - -
quotactl Q_GETFMT - - -
quotactl Q_XGETQUOTA - - -
quotactl Q_XGETQSTAT - - -
`

func init() {
	registerIface(&commonInterface{
		name:                  "mount-observe",
		summary:               mountObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  mountObserveBaseDeclarationSlots,
		connectedPlugAppArmor: mountObserveConnectedPlugAppArmor,
		connectedPlugSecComp:  mountObserveConnectedPlugSecComp,
		reservedForOS:         true,
	})
}
