// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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
	"math"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const cudaDriverLibsSummary = `allows exposing CUDA driver libraries to the system`

// Plugs only supported for the system on classic for the moment (note this is
// checked on "system" snap installation even though this is an implicit plug
// in that case) - in the future we will allow snaps having this as plug and
// this declaration will have to change.
const cudaDriverLibsBaseDeclarationPlugs = `
  cuda-driver-libs:
    allow-installation:
      plug-snap-type:
        - core
    allow-connection:
      slots-per-plug: *
    deny-auto-connection: true
`

// Installation only allowed if permitted by the snap declaration (for asserted snaps)
const cudaDriverLibsBaseDeclarationSlots = `
  cuda-driver-libs:
    allow-installation: false
    deny-auto-connection: true
`

// cudaDriverLibsInterface allows exposing CUDA driver libraries to the system or snaps.
type cudaDriverLibsInterface struct {
	commonInterface
}

func (iface *cudaDriverLibsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	var compatField string
	if err := slot.Attr("compatibility", &compatField); err != nil {
		return err
	}
	// Validate format of compatibility field - we don't actually need to do
	// anything else with it until we start to support regular snaps.
	if err := compatibility.IsValidExpression(compatField,
		&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
			{Tag: "cuda", Values: []compatibility.CompatRange{{Min: 0, Max: math.MaxUint}}},
			{Tag: "ubuntu", Values: []compatibility.CompatRange{{Min: 2404, Max: math.MaxUint}}},
		}}); err != nil {
		return err
	}
	// Validate directories
	return validateLdconfigLibDirs(slot)
}

func (iface *cudaDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being
	return addLdconfigLibDirs(spec, slot)
}

var _ = interfaces.ConfigfilesUser(&cudaDriverLibsInterface{})

const cudaDriverLibs = "cuda-driver-libs"

func (t *cudaDriverLibsInterface) PathPatterns() []string {
	return []string{systemLibrarySourcePath("*", "*", cudaDriverLibs)}
}

func (iface *cudaDriverLibsInterface) ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being

	// Files used by snap-confine on classic
	if release.OnClassic {
		if err := addConfigfilesForSystemLibrarySourcePaths(cudaDriverLibs, spec, slot); err != nil {
			return err
		}
	}

	return nil
}

func (iface *cudaDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// TODO This might need changes when we support plugs in non-system
	// snaps for this interface.
	return true
}

func init() {
	registerIface(&cudaDriverLibsInterface{
		commonInterface: commonInterface{
			name:                 cudaDriverLibs,
			summary:              cudaDriverLibsSummary,
			baseDeclarationPlugs: cudaDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots: cudaDriverLibsBaseDeclarationSlots,
			// Not supported on core yet
			implicitPlugOnCore:    false,
			implicitPlugOnClassic: true,
		},
	})
}
