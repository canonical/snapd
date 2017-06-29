// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
)

const spiSummary = `allows access to specific spi controller`

const spiBaseDeclarationSlots = `
  spi:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

// The type for spi interface
type spiInterface struct{}

// Getter for the name of the spi interface
func (iface *spiInterface) Name() string {
	return "spi"
}

func (iface *spiInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		Summary:              spiSummary,
		BaseDeclarationSlots: spiBaseDeclarationSlots,
	}
}

// Pattern to match allowed spi device nodes. It is gonna be used to check the
// validity of the path attributes in case the udev is not used for
// identification
var spiControlDeviceNodePattern = regexp.MustCompile("^/dev/spidev[0-9].[0-9]+$")

// Check validity of the defined slot
func (iface *spiInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}

	// Creation of the slot of this type
	// is allowed only by a gadget snap
	if !(slot.Snap.Type == "gadget" || slot.Snap.Type == "os") {
		return fmt.Errorf("%s slots only allowed on gadget or core snaps", iface.Name())
	}

	// Validate the path
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("%s slot must have a path attribute", iface.Name())
	}

	path = filepath.Clean(path)

	if !spiControlDeviceNodePattern.MatchString(path) {
		return fmt.Errorf("%s path attribute must be a valid device node", iface.Name())
	}

	return nil
}

// Checks and possibly modifies a plug
func (iface *spiInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *spiInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil
	}

	cleanedPath := filepath.Clean(path)
	spec.AddSnippet(fmt.Sprintf("%s rw,", cleanedPath))
	spec.AddSnippet(fmt.Sprintf("/sys/devices/platform/soc/**.spi/spi_master/spi0/%s/** rw,", strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *spiInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil
	}
	const pathPrefix = "/dev/"
	const udevRule string = `KERNEL=="%s", TAG+="%s"`
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		spec.AddSnippet(fmt.Sprintf(udevRule, strings.TrimPrefix(path, pathPrefix), tag))
	}
	return nil
}

func (iface *spiInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&spiInterface{})
}
