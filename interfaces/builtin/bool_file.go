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
	"github.com/snapcore/snapd/interfaces/apparmor"
)

const boolFileSummary = `allows access to specific file with bool semantics`

const boolFileBaseDeclarationSlots = `
  bool-file:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

// boolFileInterface is the type of all the bool-file interfaces.
type boolFileInterface struct{}

// String returns the same value as Name().
func (iface *boolFileInterface) String() string {
	return iface.Name()
}

// Name returns the name of the bool-file interface.
func (iface *boolFileInterface) Name() string {
	return "bool-file"
}

func (iface *boolFileInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              boolFileSummary,
		BaseDeclarationSlots: boolFileBaseDeclarationSlots,
	}
}

var boolFileGPIOValuePattern = regexp.MustCompile(
	"^/sys/class/gpio/gpio[0-9]+/value$")
var boolFileAllowedPathPatterns = []*regexp.Regexp{
	// The brightness of standard LED class device
	regexp.MustCompile("^/sys/class/leds/[^/]+/brightness$"),
	// The value of standard exported GPIO
	boolFileGPIOValuePattern,
}

// BeforePrepareSlot checks and possibly modifies a slot.
// Valid "bool-file" slots must contain the attribute "path".
func (iface *boolFileInterface) BeforePrepareSlot(slot *interfaces.SlotData) error {
	path, err := slot.Attr("path")
	var pathstr string
	if err == nil {
		pathstr, _ = path.(string)
	}
	if err != nil || pathstr == "" {
		return fmt.Errorf("bool-file must contain the path attribute")
	}

	pathstr = filepath.Clean(pathstr)
	for _, pattern := range boolFileAllowedPathPatterns {
		if pattern.MatchString(pathstr) {
			return nil
		}
	}
	return fmt.Errorf("bool-file can only point at LED brightness or GPIO value")
}

func (iface *boolFileInterface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *interfaces.SlotData) error {
	gpioSnippet := `
/sys/class/gpio/export rw,
/sys/class/gpio/unexport rw,
/sys/class/gpio/gpio[0-9]+/direction rw,
`

	if iface.isGPIO(slot) {
		spec.AddSnippet(gpioSnippet)
	}
	return nil
}

func (iface *boolFileInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.PlugData, slot *interfaces.SlotData) error {
	// Allow write and lock on the file designated by the path.
	// Dereference symbolic links to file path handed out to apparmor since
	// sysfs is full of symlinks and apparmor requires uses real path for
	// filtering.
	path, err := iface.dereferencedPath(slot)
	if err != nil {
		return fmt.Errorf("cannot compute plug security snippet: %v", err)
	}
	spec.AddSnippet(fmt.Sprintf("%s rwk,", path))
	return nil
}

func (iface *boolFileInterface) dereferencedPath(slot *interfaces.SlotData) (string, error) {
	if path, err := slot.Attr("path"); err == nil {
		if pathstr, ok := path.(string); ok {
			pathstr, err := evalSymlinks(pathstr)
			if err != nil {
				return "", err
			}
			return filepath.Clean(pathstr), nil
		}
	}
	panic("slot is not sanitized")
}

// isGPIO checks if a given bool-file slot refers to a GPIO pin.
func (iface *boolFileInterface) isGPIO(slot *interfaces.SlotData) bool {
	if path, err := slot.Attr("path"); err == nil {
		if pathstr, ok := path.(string); ok {
			pathstr = filepath.Clean(pathstr)
			return boolFileGPIOValuePattern.MatchString(pathstr)
		}
	}
	panic("slot is not sanitized")
}

// AutoConnect returns whether plug and slot should be implicitly
// auto-connected assuming they will be an unambiguous connection
// candidate and declaration-based checks allow.
//
// By default we allow what declarations allowed.
func (iface *boolFileInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}

func init() {
	registerIface(&boolFileInterface{})
}
