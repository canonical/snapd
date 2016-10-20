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
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
)

// The type for i2c control interface
type I2cControlInterface struct{}

// Getter for the name of the i2c-control interface
func (iface *I2cControlInterface) Name() string {
	return "i2c-control"
}

func (iface *I2cControlInterface) String() string {
	return iface.Name()
}

// Pattern to match allowed i2c device nodes. It is gonna be used to check the
// validity of the path attributes in case the udev is not used for
// identification
var i2cControlDeviceNodePattern = regexp.MustCompile("^/dev/i2c-[0-9]+$")

// Checks for validity of the defined slot
func (iface *I2cControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// FIXME: please!
}

// Checks and possibly modifies a plug
func (iface *I2cControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	// FIXME please!
}

// Returns snippet granted to install
func (iface *I2cControlInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	// FIXME do I really need it?
}

// Getter for the security snippet specific to the plug
func (iface *I2cControlInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	path, pathOk := slot.Attrs["path"].(string)
	if !pathOk {
		return nil, nil
	}
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// No USB attributes for i2c so just go directly to
		// considering the fixed node
		cleanedPath := filepath.Clean(path)
		return []byte(fmt.Sprintf("%s rw,\n", cleanedPath)), nil

	case interfaces.SecurityUdev:
		const udevRule string = `KERNEL=="%s", TAG+=snap_%s_%s`
		var udevSnippet bytes.Buffer
		for appName := range plug.Apps {
			fName = filepath.Base(path)
			rule := fmt.Sprintf(udevRule, fName, plug.Snap.Name(), appName)
			udevSnippet.WriteString(fmt.Sprintf("%s\n", rule))
		}
		return udevSnippet.Bytes(), nil
	}
	return nil, nil
}

// No extra permissions granted on connection
func (iface *I2cControlInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// No permissions granted to plug permanently
func (iface *I2cControlInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *I2cControlInterface) LegacyAutoConnect() bool {
	return false
}

func (iface *I2cControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

// =======================

const i2cConnectedPlugAppArmor = `
/dev/i2c-[0-9]* rw,
/sys/class/i2c-dev/ r,
/sys/devices/i2c-dev/ rw,
`

// NewI2CInterface returns a new "i2c" interface.
func NewI2CInterface() interfaces.Interface {
	return &commonInterface{
		name: "i2c",
		connectedPlugAppArmor:  i2cConnectedPlugAppArmor,
		reservedForOS:          true,
		autoConnect:            false,
		rejectAutoConnectPairs: false,
	}
}
