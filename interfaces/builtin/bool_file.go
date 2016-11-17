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

	"github.com/snapcore/snapd/interfaces"
)

// BoolFileInterface is the type of all the bool-file interfaces.
type BoolFileInterface struct{}

// String returns the same value as Name().
func (iface *BoolFileInterface) String() string {
	return iface.Name()
}

// Name returns the name of the bool-file interface.
func (iface *BoolFileInterface) Name() string {
	return "bool-file"
}

var boolFileGPIOValuePattern = regexp.MustCompile(
	"^/sys/class/gpio/gpio[0-9]+/value$")
var boolFileAllowedPathPatterns = []*regexp.Regexp{
	// The brightness of standard LED class device
	regexp.MustCompile("^/sys/class/leds/[^/]+/brightness$"),
	// The value of standard exported GPIO
	boolFileGPIOValuePattern,
}

// SanitizeSlot checks and possibly modifies a slot.
// Valid "bool-file" slots must contain the attribute "path".
func (iface *BoolFileInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	path, ok := slot.Attrs["path"].(string)
	if !ok || path == "" {
		return fmt.Errorf("bool-file must contain the path attribute")
	}
	path = filepath.Clean(path)
	for _, pattern := range boolFileAllowedPathPatterns {
		if pattern.MatchString(path) {
			return nil
		}
	}
	return fmt.Errorf("bool-file can only point at LED brightness or GPIO value")
}

// SanitizePlug checks and possibly modifies a plug.
func (iface *BoolFileInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// NOTE: currently we don't check anything on the plug side.
	return nil
}

// ConnectedSlotSnippet returns security snippet specific to a given connection between the bool-file slot and some plug.
// Applications associated with the slot don't gain any extra permissions.
func (iface *BoolFileInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

// PermanentSlotSnippet returns security snippet permanently granted to bool-file slots.
// Applications associated with the slot, if the slot is a GPIO, gain permission to export, unexport and set direction of any GPIO pin.
func (iface *BoolFileInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	gpioSnippet := []byte(`
/sys/class/gpio/export rw,
/sys/class/gpio/unexport rw,
/sys/class/gpio/gpio[0-9]+/direction rw,
`)
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// To provide GPIOs we need extra permissions to export/unexport and to
		// set the direction of each pin.
		if iface.isGPIO(slot) {
			return gpioSnippet, nil
		}
		return nil, nil
	}
	return nil, nil
}

// ConnectedPlugSnippet returns security snippet specific to a given connection between the bool-file plug and some slot.
// Applications associated with the plug gain permission to read, write and lock the designated file.
func (iface *BoolFileInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// Allow write and lock on the file designated by the path.
		// Dereference symbolic links to file path handed out to apparmor since
		// sysfs is full of symlinks and apparmor requires uses real path for
		// filtering.
		path, err := iface.dereferencedPath(slot)
		if err != nil {
			return nil, fmt.Errorf("cannot compute plug security snippet: %v", err)
		}
		return []byte(fmt.Sprintf("%s rwk,\n", path)), nil
	}
	return nil, nil
}

// PermanentPlugSnippet returns the configuration snippet required to use a bool-file interface.
// Applications associated with the plug don't gain any extra permissions.
func (iface *BoolFileInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *BoolFileInterface) dereferencedPath(slot *interfaces.Slot) (string, error) {
	if path, ok := slot.Attrs["path"].(string); ok {
		path, err := evalSymlinks(path)
		if err != nil {
			return "", err
		}
		return filepath.Clean(path), nil
	}
	panic("slot is not sanitized")
}

// isGPIO checks if a given bool-file slot refers to a GPIO pin.
func (iface *BoolFileInterface) isGPIO(slot *interfaces.Slot) bool {
	if path, ok := slot.Attrs["path"].(string); ok {
		path = filepath.Clean(path)
		return boolFileGPIOValuePattern.MatchString(path)
	}
	panic("slot is not sanitized")
}

// AutoConnect returns whether plug and slot should be implicitly
// auto-connected assuming they will be an unambiguous connection
// candidate and declaration-based checks allow.
//
// By default we allow what declarations allowed.
func (iface *BoolFileInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}
