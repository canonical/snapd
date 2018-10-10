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

package name

import (
	"fmt"
	"regexp"
	"strings"
)

// almostValidName is part of snap and socket name validation.
//   the full regexp we could use, "^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$", is
//   O(2ⁿ) on the length of the string in python. An equivalent regexp that
//   doesn't have the nested quantifiers that trip up Python's re would be
//   "^(?:[a-z0-9]|(?<=[a-z0-9])-)*[a-z](?:[a-z0-9]|-(?=[a-z0-9]))*$", but Go's
//   regexp package doesn't support look-aheads nor look-behinds, so in order to
//   have a unified implementation in the Go and Python bits of the project
//   we're doing it this way instead. Check the length (if applicable), check
//   this regexp, then check the dashes.
//   This still leaves sc_snap_name_validate (in cmd/snap-confine/snap.c) and
//   snap_validate (cmd/snap-update-ns/bootstrap.c) with their own handcrafted
//   validators.
var almostValidName = regexp.MustCompile("^[a-z0-9-]*[a-z][a-z0-9-]*$")

// validInstanceKey is a regular expression describing valid snap instance key
var validInstanceKey = regexp.MustCompile("^[a-z0-9]{1,10}$")

// isValidName checks snap and socket socket identifiers.
func isValidName(name string) bool {
	if !almostValidName.MatchString(name) {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' || strings.Contains(name, "--") {
		return false
	}
	return true
}

// ValidateInstance checks if a string can be used as a snap instance name.
func ValidateInstance(instanceName string) error {
	// NOTE: This function should be synchronized with the two other
	// implementations: sc_instance_name_validate and validate_instance_name .
	pos := strings.IndexByte(instanceName, '_')
	if pos == -1 {
		// just store name
		return ValidateSnap(instanceName)
	}

	storeName := instanceName[:pos]
	instanceKey := instanceName[pos+1:]
	if err := ValidateSnap(storeName); err != nil {
		return err
	}
	if !validInstanceKey.MatchString(instanceKey) {
		// TODO parallel-install: extend the error message once snap
		// install help has been updated
		return fmt.Errorf("invalid instance key: %q", instanceKey)
	}
	return nil
}

// ValidateSnap checks if a string can be used as a snap name.
func ValidateSnap(name string) error {
	// NOTE: This function should be synchronized with the two other
	// implementations: sc_snap_name_validate and validate_snap_name .
	if len(name) > 40 || !isValidName(name) {
		return fmt.Errorf("invalid snap name: %q", name)
	}
	return nil
}

// Regular expression describing correct plug, slot and interface names.
var validPlugSlotIfaceName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidatePlug checks if a string can be used as a slot name.
//
// Slot names and plug names within one snap must have unique names.
// This is not enforced by this function but is enforced by snap-level
// validation.
func ValidatePlug(name string) error {
	if !validPlugSlotIfaceName.MatchString(name) {
		return fmt.Errorf("invalid plug name: %q", name)
	}
	return nil
}

// ValidateSlot checks if a string can be used as a slot name.
//
// Slot names and plug names within one snap must have unique names.
// This is not enforced by this function but is enforced by snap-level
// validation.
func ValidateSlot(name string) error {
	if !validPlugSlotIfaceName.MatchString(name) {
		return fmt.Errorf("invalid slot name: %q", name)
	}
	return nil
}

// ValidateInterface checks if a string can be used as an interface name.
func ValidateInterface(name string) error {
	if !validPlugSlotIfaceName.MatchString(name) {
		return fmt.Errorf("invalid interface name: %q", name)
	}
	return nil
}

// Regular expressions describing correct identifiers.
var validHookName = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateHook checks if a string can be used as a hook name.
func ValidateHook(name string) error {
	valid := validHookName.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid hook name: %q", name)
	}
	return nil
}

var validAlias = regexp.MustCompile("^[a-zA-Z0-9][-_.a-zA-Z0-9]*$")

// ValidateAlias checks if a string can be used as an alias name.
func ValidateAlias(alias string) error {
	valid := validAlias.MatchString(alias)
	if !valid {
		return fmt.Errorf("invalid alias name: %q", alias)
	}
	return nil
}

// ValidateApp tells whether a string is a valid application name.
func ValidateApp(n string) error {
	var validAppName = regexp.MustCompile("^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$")

	if !validAppName.MatchString(n) {
		return fmt.Errorf("invalid app name: %q", n)
	}
	return nil
}

// ValidateSockeName checks if a string ca be used as a name for a socket (for
// socket activation).
func ValidateSocket(name string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid socket name: %q", name)
	}
	return nil
}
