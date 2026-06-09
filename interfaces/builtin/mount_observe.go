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
	"github.com/snapcore/snapd/interfaces/apparmor"
)

type mountObserveInterface struct {
	commonInterface
}

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

# Support coreutils paths (LP: #2123870)
@{SNAP_COREUTIL_DIRS}df ixr,

# Needed by 'df'. This is an information leak
@{PROC}/mounts r,
# Needed by 'htop' to detect whether it's running under lxc/lxd/docker
@{PROC}/1/mounts r,

owner @{PROC}/@{pid}/mounts r,
owner @{PROC}/@{pid}/mountstats r,

# some processes might read mount* from /proc/thread-self/ instead
# and those resolve to the following: (no mountstats here)
owner @{PROC}/@{pid}/task/@{tid}/mounts r,
owner @{PROC}/@{pid}/task/@{tid}/mountinfo r,

/sys/devices/*/block/{,**} r,

# Needed by 'htop' to calculate RAM usage more accurately (and informational purposes, if enabled)
@{PROC}/spl/kstat/zfs/arcstats r,

@{PROC}/swaps r,

# This is often out of date but some apps insist on using it
/etc/mtab r,
/etc/fstab r,

# some apps also insist on consulting utab
/run/mount/utab r,
`

// this snippet replaces a base prioritized snipet which denies these rules
// by default. See basePrioritizedSnippets in interfaces/apparmor/template.go
// for an explanation. Any priority is enough to replace a base snippet.
const mountInfoPriority = uint(1)
const mountInfoSnippet = `
owner @{PROC}/@{pid}/mountinfo r,
owner @{PROC}/self/mountinfo r,
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

listmount
statmount
`

func init() {
	registerIface(&mountObserveInterface{
		commonInterface: commonInterface{
			name:                 "mount-observe",
			summary:              mountObserveSummary,
			implicitOnCore:       true,
			implicitOnClassic:    true,
			baseDeclarationSlots: mountObserveBaseDeclarationSlots,
			// handled by AppArmorConnectedPlug
			connectedPlugAppArmor: "",
			connectedPlugSecComp:  mountObserveConnectedPlugSecComp,
		},
	})
}

func (iface *mountObserveInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(mountObserveConnectedPlugAppArmor)
	spec.AddPrioritizedSnippet(mountInfoSnippet, apparmor.MountInfoKey, mountInfoPriority)
	return nil
}
