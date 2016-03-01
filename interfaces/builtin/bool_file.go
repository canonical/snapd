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

	"github.com/ubuntu-core/snappy/interfaces"
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

// SanitizePlug checks and possibly modifies a plug.
// Valid "bool-file" plugs must contain the attribute "path".
func (iface *BoolFileInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	path, ok := plug.Attrs["path"].(string)
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

// SanitizeSlot checks and possibly modifies a slot.
func (iface *BoolFileInterface) SanitizeSlot(plug *interfaces.Slot) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}
	// NOTE: currently we don't check anything on the slot side.
	return nil
}

// PlugSecuritySnippet returns the configuration snippet required to provide a bool-file interface.
// Producers gain control over exporting, importing GPIOs as well as
// controlling the direction of particular pins.
func (iface *BoolFileInterface) PlugSecuritySnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	gpioSnippet := []byte(`
/sys/class/gpio/export rw,
/sys/class/gpio/unexport rw,
/sys/class/gpio/gpio[0-9]+/direction rw,
`)
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// To provide GPIOs we need extra permissions to export/unexport and to
		// set the direction of each pin.
		if iface.isGPIO(plug) {
			return gpioSnippet, nil
		}
		return nil, nil
	case interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

// SlotSecuritySnippet returns the configuration snippet required to use a bool-file interface.
// Consumers gain permission to read, write and lock the designated file.
func (iface *BoolFileInterface) SlotSecuritySnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		// Allow write and lock on the file designated by the path.
		// Dereference symbolic links to file path handed out to apparmor since
		// sysfs is full of symlinks and apparmor requires uses real path for
		// filtering.
		path, err := iface.dereferencedPath(plug)
		if err != nil {
			return nil, fmt.Errorf("cannot compute slot security snippet: %v", err)
		}
		return []byte(fmt.Sprintf("%s rwk,\n", path)), nil
	case interfaces.SecuritySecComp, interfaces.SecurityDBus, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *BoolFileInterface) dereferencedPath(plug *interfaces.Plug) (string, error) {
	if path, ok := plug.Attrs["path"].(string); ok {
		path, err := evalSymlinks(path)
		if err != nil {
			return "", err
		}
		return filepath.Clean(path), nil
	}
	panic("plug is not sanitized")
}

// isGPIO checks if a given bool-file plug refers to a GPIO pin.
func (iface *BoolFileInterface) isGPIO(plug *interfaces.Plug) bool {
	if path, ok := plug.Attrs["path"].(string); ok {
		path = filepath.Clean(path)
		return boolFileGPIOValuePattern.MatchString(path)
	}
	panic("plug is not sanitized")
}
