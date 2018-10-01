// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ifacestate

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/snapcore/snapd/interfaces/hotplug"
)

// HotplugDeviceAdded gets called when a device is added to the system.
func (m *InterfaceManager) HotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
}

// HotplugDeviceRemoved gets called when a device is removed from the system.
func (m *InterfaceManager) HotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
}

// ensureUniqueName modifies proposedName so that it's unique according to isUnique predicate.
// Uniqueness is achieved by appending a numeric suffix, or increasing existing suffix.
func ensureUniqueName(proposedName string, isUnique func(string) bool) string {
	// if the name is unique right away, do nothing
	if isUnique(proposedName) {
		return proposedName
	}

	suffixNumValue := 0
	prefix := strings.TrimRightFunc(proposedName, unicode.IsDigit)
	if prefix != proposedName {
		suffixNumValue, _ = strconv.Atoi(proposedName[len(prefix):])
	}
	prefix = strings.TrimRight(prefix, "-")

	// increase suffix value until we have a unique name
	for {
		suffixNumValue++
		proposedName = fmt.Sprintf("%s%d", prefix, suffixNumValue)
		if isUnique(proposedName) {
			return proposedName
		}
	}
}

const maxGenerateSlotNameLen = 20

// makeSlotName sanitizes a string to make it a valid slot name that
// passes validation rules implemented by ValidateSlotName (see snap/validate.go):
// - only lowercase letter, digits and dashes are allowed
// - must start with a letter
// - no double dashes, cannot end with a dash.
// In addition names are truncated not to exceed maxGenerateSlotNameLen characters.
func makeSlotName(s string) string {
	var out []rune
	// the dash flag is used to prevent consecutive dashes, and the dash in the front
	dash := true
	for _, c := range s {
		switch {
		case c == '-' && !dash:
			dash = true
			out = append(out, '-')
		case unicode.IsLetter(c):
			out = append(out, unicode.ToLower(c))
			dash = false
		case unicode.IsDigit(c) && len(out) > 0:
			out = append(out, c)
			dash = false
		default:
			// any other character is ignored
		}
		if len(out) >= maxGenerateSlotNameLen {
			break
		}
	}
	// make sure the name doesn't end with a dash
	return strings.TrimRight(string(out), "-")
}

var nameAttrs = []string{"NAME", "ID_MODEL_FROM_DATABASE", "ID_MODEL"}

// suggestedSlotName returns the shortest name derived from attributes defined
// by nameAttrs, or the fallbackName if there is no known attribute to derive
// name from. The name created from attributes is sanitized to ensure it's a
// valid slot name. The fallbackName is typically the name of the interface.
func suggestedSlotName(devinfo *hotplug.HotplugDeviceInfo, fallbackName string) string {
	var shortestName string
	for _, attr := range nameAttrs {
		name, ok := devinfo.Attribute(attr)
		if ok {
			if name := makeSlotName(name); name != "" {
				if shortestName == "" || len(name) < len(shortestName) {
					shortestName = name
				}
			}
		}
	}
	if len(shortestName) == 0 {
		return fallbackName
	}
	return shortestName
}
