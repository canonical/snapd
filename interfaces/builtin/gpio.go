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
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/systemd"
)

var gpioSysfsGpioBase = "/sys/class/gpio/gpio"
var gpioSysfsExport = "/sys/class/gpio/export"

// GpioInterface type
type GpioInterface struct{}

// String returns the same value as Name().
func (iface *GpioInterface) String() string {
	return iface.Name()
}

// Name of the GpioInterface
func (iface *GpioInterface) Name() string {
	return "gpio"
}

// SanitizeSlot checks the slot definition is valid
func (iface *GpioInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Paranoid check this right interface type
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// We will only allow creation of this type of slot by a gadget or OS snap
	if !(slot.Snap.Type == "gadget" || slot.Snap.Type == "os") {
		return fmt.Errorf("gpio slots only allowed on gadget or core snaps")
	}

	// Must have a GPIO number
	number, ok := slot.Attrs["number"]
	if !ok {
		return fmt.Errorf("gpio slot must have a number attribute")
	}

	// Valid values of number
	if _, ok := number.(int64); !ok {
		return fmt.Errorf("gpio slot number attribute must be an int")
	}

	// Slot is good
	return nil
}

// SanitizePlug checks the plug definition is valid
func (iface *GpioInterface) SanitizePlug(plug *interfaces.Plug) error {
	// Make sure right interface type
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}

	// Plug is good
	return nil
}

func (iface *GpioInterface) ValidatePlug(plug *interfaces.Plug, attrs map[string]interface{}) error {
	return nil
}

func (iface *GpioInterface) ValidateSlot(slot *interfaces.Slot, attrs map[string]interface{}) error {
	return nil
}

// PermanentPlugSnippet returns security snippets for plug at install
func (iface *GpioInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// ConnectedPlugSnippet returns security snippets for plug at connection
func (iface *GpioInterface) ConnectedPlugSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		path := fmt.Sprint(gpioSysfsGpioBase, slot.Attrs["number"])
		// Entries in /sys/class/gpio for single GPIO's are just symlinks
		// to their correct device part in the sysfs tree. Given AppArmor
		// requires symlinks to be dereferenced, evaluate the GPIO
		// path and add the correct absolute path to the AppArmor snippet.
		dereferencedPath, err := evalSymlinks(path)
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("%s/* rwk,\n", dereferencedPath)), nil
	}
	return nil, nil
}

// PermanentSlotSnippet - no slot snippets provided
func (iface *GpioInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *GpioInterface) ConnectedSlotRichSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) (*systemd.Snippet, error) {
	switch securitySystem {
	case interfaces.SecuritySystemd:
		gpioNum, ok := slot.Attrs["number"].(int64)
		if !ok {
			return nil, fmt.Errorf("gpio slot has invalid number attribute: %q", slot.Attrs["number"])
		}
		serviceName := interfaces.InterfaceServiceName(slot.Snap.Name(), fmt.Sprintf("gpio-%d", gpioNum))
		snippet := &systemd.Snippet{
			Services: map[string]systemd.Service{
				serviceName: {
					Type:            "oneshot",
					RemainAfterExit: true,
					ExecStart:       fmt.Sprintf("/bin/sh -c 'test -e /sys/class/gpio/gpio%d || echo %d > /sys/class/gpio/export'", gpioNum, gpioNum),
					ExecStop:        fmt.Sprintf("/bin/sh -c 'test ! -e /sys/class/gpio/gpio%d || echo %d > /sys/class/gpio/unexport'", gpioNum, gpioNum),
				},
			},
		}
		return snippet, nil
	}
	return nil, nil
}

func (iface *GpioInterface) ConnectedSlotSnippet(plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	richSnippet, err := iface.ConnectedSlotRichSnippet(plug, slot, securitySystem)
	if err != nil {
		return nil, err
	}
	if richSnippet == nil {
		return nil, nil
	}
	rawSnippet, err := json.Marshal(richSnippet)
	if err != nil {
		return nil, err
	}
	return rawSnippet, nil
}

func (iface *GpioInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
