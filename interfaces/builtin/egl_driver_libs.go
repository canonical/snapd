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
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/osutil"
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
	var clientDriver string
	if err := slot.Attr("client-driver", &clientDriver); err != nil {
		return fmt.Errorf("invalid client-driver: %w", err)
	}
	// We want a file name in client-driver, without directories
	if strings.ContainsRune(clientDriver, os.PathSeparator) {
		return fmt.Errorf("client-driver value %q should be a file", clientDriver)
	}
	var compatField string
	if err := slot.Attr("compatibility", &compatField); err != nil {
		return err
	}
	// Validate format of compatibility field - we don't actually need to
	// do anything else with it until we start to support regular snaps.
	_, err := compatibility.DecodeCompatField(compatField,
		&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
			{Tag: "egl", Values: []compatibility.CompatRange{{Min: 1, Max: 1}, {Min: 5, Max: 5}}},
			{Tag: "ubuntu", Values: []compatibility.CompatRange{{Min: 0, Max: math.MaxUint}}},
		}})
	if err != nil {
		return err
	}
	// Validate directories
	return validateLdconfigLibDirs(slot)
}

func (iface *eglDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being
	return addLdconfigLibDirs(spec, slot)
}

var _ = interfaces.ConfigfilesUser(&eglDriverLibsInterface{})

const eglVendorPath = "/usr/share/glvnd/egl_vendor.d"

func (t *eglDriverLibsInterface) PathPatterns() []string {
	return []string{filepath.Join(eglVendorPath, "*_snap_*_*.json")}
}

func (iface *eglDriverLibsInterface) ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being

	var priority int64
	if err := slot.Attr("priority", &priority); err != nil {
		return fmt.Errorf("invalid priority: %w", err)
	}
	var clientDriver string
	if err := slot.Attr("client-driver", &clientDriver); err != nil {
		return fmt.Errorf("invalid client-driver: %w", err)
	}

	var icd struct {
		FileFormatVersion string `json:"file_format_version"`
		ICD               struct {
			LibraryPath string `json:"library_path"`
		} `json:"ICD"`
	}
	icd.FileFormatVersion = "1.0.0"
	icd.ICD.LibraryPath = clientDriver
	icdContent, err := json.MarshalIndent(icd, "", "    ")
	if err != nil {
		return err
	}
	icdContent = append(icdContent, byte('\n'))

	icdPath := filepath.Join(eglVendorPath, fmt.Sprintf(
		"%d_snap_%s_%s.json", priority, slot.Snap().InstanceName(), slot.Name()))
	return spec.AddPathContent(icdPath, &osutil.MemoryFileState{Content: icdContent, Mode: 0644})
}

func (iface *eglDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// TODO This might need changes when we support plugs in non-system
	// snaps for this interface.
	return true
}

func init() {
	registerIface(&eglDriverLibsInterface{
		commonInterface: commonInterface{
			name:                 "egl-driver-libs",
			summary:              eglDriverLibsSummary,
			baseDeclarationPlugs: eglDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots: eglDriverLibsBaseDeclarationSlots,
			// Not supported on core yet
			implicitPlugOnCore:    false,
			implicitPlugOnClassic: true,
		},
	})
}
