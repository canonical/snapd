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
	"fmt"
	"math"

	"github.com/snapcore/snapd/interfaces"

	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const openglesDriverLibsSummary = `allows exposing OpenGLES driver libraries to the system`

// Plug on classic may only be declared by the system snap (implicit plug); on
// Ubuntu Core any snap may declare it (see allow-installation alternatives).
const openglesDriverLibsBaseDeclarationPlugs = `
  opengles-driver-libs:
    allow-installation:
      -
        on-classic: true
        plug-snap-type:
          - core
      -
        on-classic: false
    allow-connection:
      slots-per-plug: *
    deny-auto-connection: true
`

// Installation only allowed if permitted by the snap declaration (for asserted snaps)
const openglesDriverLibsBaseDeclarationSlots = `
  opengles-driver-libs:
    allow-installation: false
    deny-auto-connection: true
`

// openglesDriverLibsInterface allows exposing OpenGLES driver libraries to the system or snaps.
type openglesDriverLibsInterface struct {
	commonInterface
}

func (iface *openglesDriverLibsInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	if !driverLibsSupported(plug.Snap.Base) {
		return fmt.Errorf("%s interface is not supported on base %q", openglesDriverLibs, plug.Snap.Base)
	}
	return nil
}

func (iface *openglesDriverLibsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	var compatField string
	if err := slot.Attr("compatibility", &compatField); err != nil {
		return err
	}
	// Validate format of compatibility field - we don't actually need to
	// do anything else with it until we start to support regular snaps.
	if err := compatibility.IsValidExpression(compatField,
		&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
			{Tag: "opengles", Values: []compatibility.CompatRange{
				{Min: 0, Max: math.MaxUint}, {Min: 0, Max: math.MaxUint}}},
			{Tag: "ubuntu", Values: []compatibility.CompatRange{
				{Min: 0, Max: math.MaxUint}}},
		}}); err != nil {
		return err
	}
	// Validate directories
	return validateSourceDirs(slot, sourceDirAttr{attrName: "library-source", isOptional: false})
}

func (iface *openglesDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being
	return addLdconfigLibDirs(spec, slot)
}

func (iface *openglesDriverLibsInterface) MountConnectedPlug(spec *mount.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// On Ubuntu Core the provider content is bound into the assembly tree under
	// the /opt/snapd/interfaces directory (see mountAssemblyLibDirs).
	return mountAssemblyLibDirs(spec, slot, openglesDriverLibs)
}

func (iface *openglesDriverLibsInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Authorize snap-update-ns to construct (and eventually tear down) the
	// assembly tree under /opt/snapd/interfaces. The default base template
	// already grants /opt/** mrklix to the app itself, no extra snippet needed.
	return addAppArmorAssemblyLibDirs(spec, slot, openglesDriverLibs)
}

const openglesDriverLibs = "opengles-driver-libs"

func (t *openglesDriverLibsInterface) PathPatterns() []string {
	return []string{systemLibrarySourcePath("*", "*", openglesDriverLibs)}
}

func (iface *openglesDriverLibsInterface) ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being

	// Files used by snap-confine on classic
	if release.OnClassic {
		if err := addConfigfilesForSystemLibrarySourcePaths(openglesDriverLibs, spec, slot); err != nil {
			return err
		}
	}

	return nil
}

func (iface *openglesDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// TODO This might need changes when we support plugs in non-system
	// snaps for this interface.
	return true
}

func init() {
	registerIface(&openglesDriverLibsInterface{
		commonInterface: commonInterface{
			name:                 "opengles-driver-libs",
			summary:              openglesDriverLibsSummary,
			baseDeclarationPlugs: openglesDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots: openglesDriverLibsBaseDeclarationSlots,
			// Not supported on core yet
			implicitPlugOnCore:    false,
			implicitPlugOnClassic: true,
		},
	})
}
