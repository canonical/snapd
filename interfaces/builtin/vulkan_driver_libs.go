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
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

const vulkanDriverLibsSummary = `allows exposing vulkan driver libraries to the system`

// Plugs only supported for the system on classic for the moment (note this is
// checked on "system" snap installation even though this is an implicit plug
// in that case) - in the future we will allow snaps having this as plug and
// this declaration will have to change.
const vulkanDriverLibsBaseDeclarationPlugs = `
  vulkan-driver-libs:
    allow-installation:
      plug-snap-type:
        - core
    allow-connection:
      slots-per-plug: *
    deny-auto-connection: true
`

// Installation only allowed if permitted by the snap declaration (for asserted snaps)
const vulkanDriverLibsBaseDeclarationSlots = `
  vulkan-driver-libs:
    allow-installation: false
    deny-auto-connection: true
`

// vulkanDriverLibsInterface allows exposing VULKAN driver libraries to the system or snaps.
type vulkanDriverLibsInterface struct {
	commonInterface
}

func (iface *vulkanDriverLibsInterface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	// Validate attributes

	var compatField string
	if err := slot.Attr("compatibility", &compatField); err != nil {
		return err
	}
	// Validate format of compatibility field - we don't actually need to
	// do anything else with it until we start to support regular snaps.
	if err := compatibility.IsValidExpression(compatField,
		&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
			{Tag: "vulkan", Values: []compatibility.CompatRange{{Min: 1, Max: 1}, {Min: 0, Max: 10}}},
			{Tag: "ubuntu", Values: []compatibility.CompatRange{{Min: 0, Max: math.MaxUint}}},
		}}); err != nil {
		return err
	}

	// Validate *-source directories
	for _, sda := range []sourceDirAttr{
		{attrName: "library-source", isOptional: false},
		{attrName: "icd-source", isOptional: false},
		{attrName: "implicit-layer-source", isOptional: true},
		{attrName: "explicit-layer-source", isOptional: true},
	} {
		if err := validateSourceDirs(slot, sda); err != nil {
			return err
		}
	}
	return nil
}

func (iface *vulkanDriverLibsInterface) LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// The plug can only be the system plug for the time being
	return addLdconfigLibDirs(spec, slot)
}

var _ = symlinks.ConnectedPlugCallback(&vulkanDriverLibsInterface{})
var _ = interfaces.ConfigfilesUser(&vulkanDriverLibsInterface{})

const (
	vulkanDriverLibs        = "vulkan-driver-libs"
	vulkanIcdPath           = "/etc/vulkan/icd.d"
	vulkanExplicitLayerPath = "/etc/vulkan/explicit_layer.d"
	vulkanImplicitLayerPath = "/etc/vulkan/implicit_layer.d"
)

// Implementation of the SymlinksUser interface
func (iface *vulkanDriverLibsInterface) TrackedDirectories() []string {
	return []string{vulkanIcdPath, vulkanExplicitLayerPath, vulkanImplicitLayerPath}
}

func checkVulkanIcdFile(slot *interfaces.ConnectedSlot, icdContent []byte) error {
	var icdJson struct {
		Icd struct {
			ApiVersion  string `json:"api_version"`
			LibraryPath string `json:"library_path"`
		} `json:"ICD"`
	}
	err := json.Unmarshal(icdContent, &icdJson)
	if err != nil {
		return fmt.Errorf("while unmarshalling: %w", err)
	}

	if icdJson.Icd.ApiVersion == "" {
		return errors.New("no api_version value found")
	}
	if err := checkVulkanApiVersion(slot, icdJson.Icd.ApiVersion); err != nil {
		return err
	}

	if icdJson.Icd.LibraryPath == "" {
		return errors.New("no library_path value found")
	}
	// Here we are implicitly limiting library_path to be a file
	// name instead of a full path.
	_, err = filePathInLibDirs(slot, icdJson.Icd.LibraryPath)
	if err != nil {
		return err
	}
	return nil
}

func checkVulkanApiVersion(slot *interfaces.ConnectedSlot, apiVersion string) error {
	// Build a compatibility label for this API version and check consistency
	// with the one specified by the interface.
	version := strings.Split(apiVersion, ".")
	if len(version) < 2 {
		return fmt.Errorf("api_version is not a version: %s", apiVersion)
	}
	major, err := strconv.ParseUint(version[0], 10, 64)
	if err != nil {
		return fmt.Errorf("while parsing major: api_version %s", apiVersion)
	}
	minor, err := strconv.ParseUint(version[1], 10, 64)
	if err != nil {
		return fmt.Errorf("while parsing minor: api_version %s", apiVersion)
	}
	// We don't care about the Ubuntu version here (note: we allow only 8 digits in version)
	apiCompat := fmt.Sprintf("vulkan-%d-%d-ubuntu-(0..99999999)", major, minor)
	var compatField string
	if err := slot.Attr("compatibility", &compatField); err != nil {
		return err
	}
	if !compatibility.CheckCompatibility(compatField, apiCompat) {
		return fmt.Errorf("api_version %s is not compatible with the interface compatibility label %s", apiVersion, compatField)
	}
	return nil
}

type vulkanLayer struct {
	ApiVersion string `json:"api_version"`
	// Only one of ApiVersion/ComponentLayers can be present
	LibraryPath     string   `json:"library_path"`
	ComponentLayers []string `json:"component_layers"`
}

func checkVulkanLayersFile(slot *interfaces.ConnectedSlot, fileContent []byte) error {
	var layerJson struct {
		// Only one of these can be present
		Layer  *vulkanLayer  `json:"layer"`
		Layers []vulkanLayer `json:"layers"`
	}
	err := json.Unmarshal(fileContent, &layerJson)
	if err != nil {
		return fmt.Errorf("while unmarshalling: %w", err)
	}

	if layerJson.Layer == nil && len(layerJson.Layers) == 0 {
		return errors.New("either layer or layers should be present in layers file")
	}
	if layerJson.Layer != nil && len(layerJson.Layers) > 0 {
		return errors.New("layer and layers cannot be both present in layers file")
	}
	if layerJson.Layer != nil {
		layerJson.Layers = append(layerJson.Layers, *layerJson.Layer)
	}
	for _, layer := range layerJson.Layers {
		if err := checkLayer(slot, layer); err != nil {
			return err
		}
	}

	return nil
}

func checkLayer(slot *interfaces.ConnectedSlot, layer vulkanLayer) error {
	if layer.ApiVersion == "" {
		return errors.New("no api_version value found")
	}
	if err := checkVulkanApiVersion(slot, layer.ApiVersion); err != nil {
		return err
	}

	if layer.LibraryPath == "" && len(layer.ComponentLayers) == 0 {
		return errors.New("either library_path or component_layers should be present in layer")
	}
	if layer.LibraryPath != "" && len(layer.ComponentLayers) > 0 {
		return errors.New("library_path and component_layers cannot be both present in layers file")
	}
	// No check is done for component_layers, as that is a meta-layer that
	// depends on other layers that should have defined library_path.
	if layer.LibraryPath != "" {
		// Here we are implicitly limiting library_path to be a file
		// name instead of a full path.
		_, err := filePathInLibDirs(slot, layer.LibraryPath)
		if err != nil {
			return err
		}
	}

	return nil
}

func (iface *vulkanDriverLibsInterface) SymlinksConnectedPlug(spec *symlinks.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	for _, sourceAttr := range []struct {
		sda       sourceDirAttr
		targetDir string
		checker   func(slot *interfaces.ConnectedSlot, icdContent []byte) error
	}{
		{sourceDirAttr{attrName: "icd-source", isOptional: false},
			vulkanIcdPath, checkVulkanIcdFile},
		{sourceDirAttr{attrName: "implicit-layer-source", isOptional: true},
			vulkanImplicitLayerPath, checkVulkanLayersFile},
		{sourceDirAttr{attrName: "explicit-layer-source", isOptional: true},
			vulkanExplicitLayerPath, checkVulkanLayersFile},
	} {
		if err := iface.symlinksForSourceDir(spec, slot,
			sourceAttr.sda, sourceAttr.targetDir, sourceAttr.checker); err != nil {
			return err
		}
	}
	return nil
}

func (iface *vulkanDriverLibsInterface) symlinksForSourceDir(spec *symlinks.Specification, slot *interfaces.ConnectedSlot, sda sourceDirAttr, targetDir string, checker func(slot *interfaces.ConnectedSlot, icdContent []byte) error) error {
	icdPaths, err := icdSourceDirsCheck(slot, sda, checker)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", sda.attrName, err)
	}

	// Create symlinks to snap content (which is fine as this is a super-privileged slot)
	for _, icdPath := range icdPaths {
		// Strip out mount dir and snap name and revision
		relIcdPath, err := filepath.Rel(dirs.SnapMountDir, icdPath)
		if err != nil {
			return err
		}
		dirs := strings.SplitN(relIcdPath, "/", 3)
		if len(dirs) < 3 {
			return fmt.Errorf("internal error: wrong file path: %s", relIcdPath)
		}
		// Make path an easier to handle name
		escapedRelPath := systemd.EscapeUnitNamePath(filepath.Join(dirs[2]))
		// Note that icdFilePathsCheck already ensures a .json suffix
		linkPath := filepath.Join(targetDir, fmt.Sprintf("snap_%s_%s_%s",
			slot.Snap().InstanceName(), slot.Name(), escapedRelPath))
		if err := spec.AddSymlink(icdPath, linkPath); err != nil {
			return err
		}
	}

	return nil
}

// Implementation of the ConfigfilesUser interface
func (t *vulkanDriverLibsInterface) PathPatterns() []string {
	return []string{systemLibrarySourcePath("*", "*", vulkanDriverLibs)}
}

func (iface *vulkanDriverLibsInterface) ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Files used by snap-confine on classic
	if release.OnClassic {
		if err := addConfigfilesForSystemLibrarySourcePaths(vulkanDriverLibs, spec, slot); err != nil {
			return err
		}
	}
	return nil
}

func (iface *vulkanDriverLibsInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// TODO This might need changes when we support plugs in non-system
	// snaps for this interface.
	return true
}

func init() {
	registerIface(&vulkanDriverLibsInterface{
		commonInterface: commonInterface{
			name:                 vulkanDriverLibs,
			summary:              vulkanDriverLibsSummary,
			baseDeclarationPlugs: vulkanDriverLibsBaseDeclarationPlugs,
			baseDeclarationSlots: vulkanDriverLibsBaseDeclarationSlots,
			// Not supported on core yet
			implicitPlugOnCore:    false,
			implicitPlugOnClassic: true,
		},
	})
}
