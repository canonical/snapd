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

	// extract number suffix if present
	pname := []rune(proposedName)
	end := len(pname) - 1
	suffixIndex := end
	for suffixIndex >= 0 && unicode.IsDigit(pname[suffixIndex]) {
		suffixIndex--
	}

	var suffixNumValue uint64
	// if numeric suffix hasn't been found, append "-" before the number
	if suffixIndex == end {
		pname = append(pname, '-')
		suffixIndex++
	} else {
		var err error
		suffixNumValue, err = strconv.ParseUint(string(pname[suffixIndex+1:]), 10, 32)
		if err != nil {
			suffixIndex = end
		}
	}

	// increase suffix value until we have a unique name
	for {
		suffixNumValue++
		proposedName = fmt.Sprintf("%s%d", string(pname[:suffixIndex+1]), suffixNumValue)
		if isUnique(proposedName) {
			return proposedName
		}
	}
}

const maxLen = 20

// cleanupSlotName sanitizes proposedName to make it a valid slot name that
// passes validation rules implemented by ValidateSlotName (see snap/validate.go):
// - only lowercase letter, digits and dashes are allowed
// - must start with a letter
// - no double dashes, cannot end with a dash.
// In addition names are truncated not to exceed maxLen characters.
func cleanupSlotName(proposedName string) string {
	var out []rune
	var charCount int
	// the dash flag is used to prevent consecutive dashes, and the dash in the front
	dash := true
Loop:
	for _, c := range proposedName {
		switch {
		case c == '-' && !dash:
			dash = true
			out = append(out, '-')
		case unicode.IsLetter(c):
			out = append(out, unicode.ToLower(c))
			dash = false
			charCount++
			if charCount >= maxLen {
				break Loop
			}
		case unicode.IsDigit(c) && charCount > 0:
			out = append(out, c)
			dash = false
			charCount++
			if charCount >= maxLen {
				break Loop
			}
		default:
			// any other character is ignored
		}
	}
	// make sure the name doesn't end with a dash
	if len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

var nameAttrs = []string{"NAME", "ID_MODEL_FROM_DATABASE", "ID_MODEL"}

// suggestedSlotName returns the shortest name derived from attributes defined by nameAttrs, or
// the fallbackName if there is no known attribute to derive name from. The name created from
// attributes is cleaned up by cleanupSlotName function.
// The fallbackName is typically the name of the interface.
func suggestedSlotName(devinfo *hotplug.HotplugDeviceInfo, fallbackName string) string {
	var candidates []string
	for _, attr := range nameAttrs {
		name, ok := devinfo.Attribute(attr)
		if ok {
			name = cleanupSlotName(name)
			if name != "" {
				candidates = append(candidates, name)
			}
		}
	}
	if len(candidates) == 0 {
		return fallbackName
	}
	shortestName := candidates[0]
	for _, cand := range candidates[1:] {
		if len(cand) < len(shortestName) {
			shortestName = cand
		}
	}
	return shortestName
}
