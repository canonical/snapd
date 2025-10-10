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
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

const gbmDriverLibsSummary = `allows exposing GBM driver libraries to the system`

// Plugs only supported for the system on classic for the moment (note this is
// checked on "system" snap installation even though this is an implicit plug
// in that case) - in the future we will allow snaps having this as plug and
// this declaration will have to change.
const gbmDriverLibsBaseDeclarationPlugs = `
  gbm-driver-libs:
    allow-installation:
      plug-snap-type:
        - core
    allow-connection:
      slots-per-plug: *
    deny-auto-connection: true
`

// Installation only allowed if permitted by the snap declaration (for asserted snaps)
const gbmDriverLibsBaseDeclarationSlots = `
  gbm-driver-libs:
    allow-installation: false
    deny-auto-connection: true
`

// gbmDriverLibsInterface allows exposing GBM driver libraries to the system or snaps.
type gbmDriverLibsInterface struct {
	commonInterface
}

var reClientDriver = regexp.MustCompile("^[-0-9a-zA-Z_.]+$").Match

func (iface *gbmDriverLibsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// Validate attributes
	var clientDriver string
	if err := slot.Attr("client-driver", &clientDriver); err != nil {
		return fmt.Errorf("invalid client-driver: %w", err)
	}
	// We want a file name in client-driver, without directories
	if strings.ContainsRune(clientDriver, os.PathSeparator) {
		return fmt.Errorf("client-driver value %q should be a file", clientDriver)
	}
	if !reClientDriver([]byte(clientDriver)) {
		return fmt.Errorf("invalid client-driver name: %s", clientDriver)
	}
	var compatField string
	if err := slot.Attr("compatibility", &compatField); err != nil {
		return err
	}
	// Validate format of compatibility field - we don't actually need to
	// do anything else with it until we start to support regular snaps.
	var compatErr error
	var validCompat bool
	// TODO maybe we should support regular expressions in the CompatSpec tags
	for _, arch := range []string{"arch32", "arch64"} {
		err := compatibility.IsValidExpression(compatField,
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "gbmbackend", Values: []compatibility.CompatRange{{Min: 0, Max: math.MaxUint}}},
				{Tag: arch, Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				{Tag: "ubuntu", Values: []compatibility.CompatRange{{Min: 0, Max: math.MaxUint}}},
			}})
		if err == nil {
			validCompat = true
			break
		}
		compatErr = err
	}
	if !validCompat {
		return compatErr
	}
	// Validate directories
	return validateLdconfigLibDirs(slot)
}

func (iface *gbmDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being
	return addLdconfigLibDirs(spec, slot)
}

var _ = interfaces.SymlinksUser(&gbmDriverLibsInterface{})
var _ = symlinks.ConnectedPlugCallback(&gbmDriverLibsInterface{})
var _ = interfaces.ConfigfilesUser(&gbmDriverLibsInterface{})

func gbmVendorPath() string {
	// TODO consider alternative architectures?
	return fmt.Sprintf("/usr/lib/%s-linux-gnu/gbm", osutil.MachineName())
}

func (iface *gbmDriverLibsInterface) TrackedDirectories() []string {
	return []string{gbmVendorPath()}
}

func (iface *gbmDriverLibsInterface) SymlinksConnectedPlug(spec *symlinks.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var clientDriver string
	if err := slot.Attr("client-driver", &clientDriver); err != nil {
		return fmt.Errorf("invalid client-driver: %w", err)
	}
	// Look for the driver library
	path, err := filePathInLibDirs(slot, clientDriver)
	if err != nil {
		return err
	}

	return spec.AddSymlink(path, filepath.Join(gbmVendorPath(), clientDriver))
}

const gbmDriverLibs = "gbm-driver-libs"

func (t *gbmDriverLibsInterface) PathPatterns() []string {
	return []string{librarySourcePath("*", "*", gbmDriverLibs)}
}

func (iface *gbmDriverLibsInterface) ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being

	// Files used by snap-confine on classic
	if release.OnClassic {
		if err := addConfigfilesForLibrarySourcePaths(gbmDriverLibs, spec, slot); err != nil {
			return err
		}
	}

	return nil
}

func (iface *gbmDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// TODO This might need changes when we support plugs in non-system
	// snaps for this interface.
	return true
}

func init() {
	registerIface(&gbmDriverLibsInterface{
		commonInterface: commonInterface{
			name:                 gbmDriverLibs,
			summary:              gbmDriverLibsSummary,
			baseDeclarationPlugs: gbmDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots: gbmDriverLibsBaseDeclarationSlots,
			// Not supported on core yet
			implicitPlugOnCore:    false,
			implicitPlugOnClassic: true,
		},
	})
}
