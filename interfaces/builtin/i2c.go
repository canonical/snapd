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
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const i2cSummary = `allows access to specific I2C controller`

const i2cBaseDeclarationSlots = `
  i2c:
    allow-installation:
      slot-snap-type:
        - gadget
        - core
    deny-auto-connection: true
`

const i2cConnectedPlugAppArmor = `
# Description: Can access I2C controller

%s rw,
/sys/devices/platform/{*,**.i2c}/%s/** rw,
`

// The type for i2c interface
type i2cInterface struct{}

// Getter for the name of the i2c interface
func (iface *i2cInterface) Name() string {
	return "i2c"
}

func (iface *i2cInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              i2cSummary,
		BaseDeclarationSlots: i2cBaseDeclarationSlots,
	}
}

func (iface *i2cInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed i2c device nodes. It is gonna be used to check the
// validity of the path attributes in case the udev is not used for
// identification
var i2cControlDeviceNodePattern = regexp.MustCompile("^/dev/i2c-[0-9]+$")

// Check validity of the defined slot
func (iface *i2cInterface) SanitizeSlot(slot *snap.SlotInfo) error {
	if err := sanitizeSlotReservedForOSOrGadget(iface, slot); err != nil {
		return err
	}

	// Validate the path
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("%s slot must have a path attribute", iface.Name())
	}

	path = filepath.Clean(path)

	if !i2cControlDeviceNodePattern.MatchString(path) {
		return fmt.Errorf("%s path attribute must be a valid device node", iface.Name())
	}

	return nil
}

func (iface *i2cInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil
	}

	cleanedPath := filepath.Clean(path)
	spec.AddSnippet(fmt.Sprintf(i2cConnectedPlugAppArmor, cleanedPath, strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *i2cInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil
	}
	spec.TagDevice(fmt.Sprintf(`KERNEL=="%s"`, strings.TrimPrefix(path, "/dev/")))
	return nil
}

func (iface *i2cInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&i2cInterface{})
}
