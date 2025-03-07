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
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
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

func (iface *cudaDriverLibsInterface) validateVersion(version string) error {
	// Only latin-1 expected. We expect numbers and dots, see
	// https://developer.nvidia.com/cuda-toolkit-archive.
	numChars := len(version)
	for i := 0; i < numChars; i++ {
		if version[i] >= '0' && version[i] <= '9' {
			continue
		}
		if version[i] == '.' && i > 0 && i < numChars-1 {
			continue
		}
		return fmt.Errorf("invalid CUDA version: %q", version)
	}
	return nil
}

// validateVersionsRange makes sure that the "api-version" attribute
// has the right format, which can be either a version or a version
// range, with valid formats:
// <version>
// <version_1> .. <version_1>
// The versions are dot separated digits like 1.2 or 2.3.1.
func (iface *cudaDriverLibsInterface) validateVersionsRange(versRange string) error {
	fields := strings.Fields(versRange)
	if len(fields) != 1 && len(fields) != 3 {
		return fmt.Errorf("wrong format for api-version: %q", versRange)
	}
	switch len(fields) {
	case 1:
		if err := iface.validateVersion(fields[0]); err != nil {
			return err
		}
	case 3:
		if fields[1] != ".." {
			return fmt.Errorf("invalid separator in api-version: %q", fields[1])
		}
		for _, f := range []int{0, 2} {
			if err := iface.validateVersion(fields[f]); err != nil {
				return err
			}
		}
		// Reuse version comparison for debian packages, it should work
		// fine for our subset of characters.
		cmpRes, err := strutil.VersionCompare(fields[0], fields[2])
		if err != nil {
			return fmt.Errorf("while comparing range in api-version: %v", err)
		}
		if cmpRes > 0 {
			return fmt.Errorf("%q should not be bigger than %q", fields[0], fields[2])
		}
	default:
		return fmt.Errorf("wrong format for api-version: %q", versRange)
	}
	return nil
}

func (iface *cudaDriverLibsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// Validate format of API version - we don't actually need to do
	// anything else with it until we start to support regular snaps.
	var versions string
	if err := slot.Attr("api-version", &versions); err == nil {
		if err := iface.validateVersionsRange(versions); err != nil {
			return err
		}
	}
	// Validate directories
	return validateLdconfigLibDirs(slot)
}

func (iface *cudaDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being
	return addLdconfigLibDirs(spec, slot)
}

func (iface *cudaDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

func init() {
	registerIface(&cudaDriverLibsInterface{
		commonInterface: commonInterface{
			name:                 "cuda-driver-libs",
			summary:              cudaDriverLibsSummary,
			baseDeclarationPlugs: cudaDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots: cudaDriverLibsBaseDeclarationSlots,
			// Not supported on core yet
			implicitPlugOnCore:    false,
			implicitPlugOnClassic: true,
		},
	})
}
