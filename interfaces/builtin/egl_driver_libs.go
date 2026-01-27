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
	"encoding/json"
	"fmt"
	"math"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const eglDriverLibsSummary = `allows exposing EGL driver libraries to the system`

// Plugs only supported for the system on classic for the moment (note this is
// checked on "system" snap installation even though this is an implicit plug
// in that case) - in the future we will allow snaps having this as plug and
// this declaration will have to change.
const eglDriverLibsBaseDeclarationPlugs = `
  egl-driver-libs:
    allow-installation:
      plug-snap-type:
        - core
    allow-connection:
      slots-per-plug: *
    deny-auto-connection: true
`

// Installation only allowed if permitted by the snap declaration (for asserted snaps)
const eglDriverLibsBaseDeclarationSlots = `
  egl-driver-libs:
    allow-installation: false
    deny-auto-connection: true
`

// eglDriverLibsInterface allows exposing EGL driver libraries to the system or snaps.
type eglDriverLibsInterface struct {
	commonInterface
}

func (iface *eglDriverLibsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// Validate attributes
	var priority int64
	if err := slot.Attr("priority", &priority); err != nil {
		return fmt.Errorf("invalid priority: %w", err)
	}
	if priority <= 0 {
		return fmt.Errorf("priority must be a positive integer")
	}

	var compatField string
	if err := slot.Attr("compatibility", &compatField); err != nil {
		return err
	}
	// Validate format of compatibility field - we don't actually need to
	// do anything else with it until we start to support regular snaps.
	if err := compatibility.IsValidExpression(compatField,
		&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
			{Tag: "egl", Values: []compatibility.CompatRange{{Min: 1, Max: 1}, {Min: 5, Max: 5}}},
			{Tag: "ubuntu", Values: []compatibility.CompatRange{{Min: 0, Max: math.MaxUint}}},
		}}); err != nil {
		return err
	}

	// Validate *-source directories
	if err := validateSourceDirs(slot, sourceDirAttr{attrName: "icd-source", isOptional: false}); err != nil {
		return err
	}
	return validateSourceDirs(slot, sourceDirAttr{attrName: "library-source", isOptional: false})
}

func (iface *eglDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being
	return addLdconfigLibDirs(spec, slot)
}

var _ = symlinks.ConnectedPlugCallback(&eglDriverLibsInterface{})
var _ = interfaces.ConfigfilesUser(&eglDriverLibsInterface{})

const (
	eglDriverLibs = "egl-driver-libs"
	eglVendorPath = "/etc/glvnd/egl_vendor.d"
)

func (iface *eglDriverLibsInterface) TrackedDirectories() []string {
	return []string{eglVendorPath}
}

func checkEglIcdFile(slot *interfaces.ConnectedSlot, icdContent []byte) error {
	var icdJson struct {
		Icd struct {
			LibraryPath string `json:"library_path"`
		} `json:"ICD"`
	}
	err := json.Unmarshal(icdContent, &icdJson)
	if err != nil {
		return fmt.Errorf("while unmarshalling: %w", err)
	}
	if icdJson.Icd.LibraryPath == "" {
		return fmt.Errorf("no library_path value found")
	}
	// Here we are implicitly limiting library_path to be a file
	// name instead of a full path.
	_, err = filePathInLibDirs(slot, icdJson.Icd.LibraryPath)
	if err != nil {
		return err
	}
	return nil
}

func (iface *eglDriverLibsInterface) SymlinksConnectedPlug(spec *symlinks.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	const withPriority = true
	return symlinksForSourceDir(spec, slot,
		sourceDirAttr{attrName: "icd-source", isOptional: false}, eglVendorPath,
		checkEglIcdFile, withPriority)
}

func (t *eglDriverLibsInterface) PathPatterns() []string {
	return []string{systemLibrarySourcePath("*", "*", eglDriverLibs)}
}

func (iface *eglDriverLibsInterface) ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Files used by snap-confine on classic
	if release.OnClassic {
		if err := addConfigfilesForSystemLibrarySourcePaths(eglDriverLibs, spec, slot); err != nil {
			return err
		}
	}
	return nil
}

func (iface *eglDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// TODO This might need changes when we support plugs in non-system
	// snaps for this interface.
	return true
}

func init() {
	registerIface(&eglDriverLibsInterface{
		commonInterface: commonInterface{
			name:                 eglDriverLibs,
			summary:              eglDriverLibsSummary,
			baseDeclarationPlugs: eglDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots: eglDriverLibsBaseDeclarationSlots,
			// Not supported on core yet
			implicitPlugOnCore:    false,
			implicitPlugOnClassic: true,
		},
	})
}
