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
)

// The type for i2c interface
type I2cInterface struct{}

// Getter for the name of the i2c interface
func (iface *I2cInterface) Name() string {
	return "i2c"
}

func (iface *I2cInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed i2c device nodes. It is gonna be used to check the
// validity of the path attributes in case the udev is not used for
// identification
var i2cControlDeviceNodePattern = regexp.MustCompile("^/dev/i2c-[0-9]+$")

// Check validity of the defined slot
func (iface *I2cInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
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

	if !i2cControlDeviceNodePattern.MatchString(path) {
		return fmt.Errorf("%s path attribute must be a valid device node", iface.Name())
	}

	return nil
}

// Checks and possibly modifies a plug
func (iface *I2cInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *I2cInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil
	}

	cleanedPath := filepath.Clean(path)
	spec.AddSnippet(fmt.Sprintf("%s rw,", cleanedPath))
	return nil
}

func (iface *I2cInterface) UdevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
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

func (iface *I2cInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *I2cInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func (iface *I2cInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *I2cInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}
