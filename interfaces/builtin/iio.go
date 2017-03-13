// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const iioConnectedPlugAppArmor = `
# Description: Give access to a specific IIO device on the system.

###IIO_DEVICE_PATH### rw,
/sys/bus/iio/devices/###IIO_DEVICE_NAME###/ r,
/sys/bus/iio/devices/###IIO_DEVICE_NAME###/** rwk,
`

// The type for iio interface
type IioInterface struct{}

// Getter for the name of the iio interface
func (iface *IioInterface) Name() string {
	return "iio"
}

func (iface *IioInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed iio device nodes. It is going to be used to check the
// validity of the path attributes in case the udev is not used for
// identification
var iioControlDeviceNodePattern = regexp.MustCompile("^/dev/iio:device[0-9]+$")

// Check validity of the defined slot
func (iface *IioInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Creation of the slot of this type
	// is allowed only by a gadget or os snap
	if !(slot.Snap.Type == "gadget" || slot.Snap.Type == "os") {
		return fmt.Errorf("%s slots only allowed on gadget or core snaps", iface.Name())
	}

	// Validate the path
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("%s slot must have a path attribute", iface.Name())
	}

	path = filepath.Clean(path)

	if !iioControlDeviceNodePattern.MatchString(path) {
		return fmt.Errorf("%s path attribute must be a valid device node", iface.Name())
	}

	return nil
}

// Checks and possibly modifies a plug
func (iface *IioInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

// Returns snippet granted on install
func (iface *IioInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *IioInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil
	}

	cleanedPath := filepath.Clean(path)
	snippet := strings.Replace(iioConnectedPlugAppArmor, "###IIO_DEVICE_PATH###", cleanedPath, -1)

	// The path is already verified against a regular expression
	// in SanitizeSlot so we can rely on its structure here and
	// safely strip the '/dev/' prefix to get the actual name of
	// the IIO device.
	deviceName := strings.TrimPrefix(path, "/dev/")
	snippet = strings.Replace(snippet, "###IIO_DEVICE_NAME###", deviceName, -1)

	return spec.AddSnippet(snippet)
}

// Getter for the security snippet specific to the plug
func (iface *IioInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil, nil
	}
	switch securitySystem {
	case interfaces.SecurityUDev:
		var tagSnippet bytes.Buffer
		const pathPrefix = "/dev/"
		const udevRule = `KERNEL=="%s", TAG+="%s"`
		for appName := range plug.Apps {
			tag := udevSnapSecurityName(plug.Snap.Name(), appName)
			tagSnippet.WriteString(fmt.Sprintf(udevRule, strings.TrimPrefix(path, pathPrefix), tag))
			tagSnippet.WriteString("\n")
		}
		return tagSnippet.Bytes(), nil
	}
	return nil, nil
}

// No extra permissions granted on connection
func (iface *IioInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// No permissions granted to plug permanently
func (iface *IioInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *IioInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}
