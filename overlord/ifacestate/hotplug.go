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
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/snapcore/snapd/interfaces/hotplug"
)

// List of attributes that determine the computation of default device key.
// Attributes are grouped by similarity, the first non-empty attribute within the group goes into the key.
// The final key is composed of 4 attributes (some of which may be empty), separated by "/".
// Warning, any future changes to these definitions requrie a new key version.
var attrGroups = [][][]string{
	// key version 0
	{
		// Name
		{"ID_V4L_PRODUCT", "NAME", "ID_NET_NAME", "PCI_SLOT_NAME"},
		// Vendor
		{"ID_VENDOR_ID", "ID_VENDOR", "ID_WWN", "ID_WWN_WITH_EXTENSION", "ID_VENDOR_FROM_DATABASE", "ID_VENDOR_ENC", "ID_OUI_FROM_DATABASE"},
		// Model
		{"ID_MODEL_ID", "ID_MODEL_ENC"},
		// Identifier
		{"ID_SERIAL", "ID_SERIAL_SHORT", "ID_NET_NAME_MAC", "ID_REVISION"},
	},
}

// deviceKeyVersion is the current version number for the default keys computed by hotplug subsystem.
// Fresh device keys always use current version format
var deviceKeyVersion = len(attrGroups) - 1

// defaultDeviceKey computes device key from the attributes of
// HotplugDeviceInfo. Empty string is returned if too few attributes are present
// to compute a good key. Attributes used to compute device key are defined in
// attrGroups list above and they depend on the keyVersion passed to the
// function.
// The resulting key returned by the function has the following format:
// <version><checksum> where checksum is the sha256 checksum computed over
// select attributes of the device.
func defaultDeviceKey(devinfo *hotplug.HotplugDeviceInfo, keyVersion int) (string, error) {
	found := 0
	key := sha256.New()
	if keyVersion >= 16 || keyVersion >= len(attrGroups) {
		return "", fmt.Errorf("internal error: invalid key version %d", keyVersion)
	}
	for _, group := range attrGroups[keyVersion] {
		for _, attr := range group {
			if val, ok := devinfo.Attribute(attr); ok && val != "" {
				key.Write([]byte(attr))
				key.Write([]byte{0})
				key.Write([]byte(val))
				key.Write([]byte{0})
				found++
				break
			}
		}
	}
	if found < 2 {
		return "", nil
	}
	return fmt.Sprintf("%x%x", keyVersion, key.Sum(nil)), nil
}

// hotplugDeviceAdded gets called when a device is added to the system.
func (m *InterfaceManager) hotplugDeviceAdded(devinfo *hotplug.HotplugDeviceInfo) {
}

// hotplugDeviceRemoved gets called when a device is removed from the system.
func (m *InterfaceManager) hotplugDeviceRemoved(devinfo *hotplug.HotplugDeviceInfo) {
}

// hotplugEnumerationDone gets called when initial enumeration on startup is finished.
func (m *InterfaceManager) hotplugEnumerationDone() {
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
