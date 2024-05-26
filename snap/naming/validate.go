// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2021 Canonical Ltd
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

// Package naming implements naming constraints and concepts for snaps and their elements.
package naming

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

// almostValidName is part of snap and socket name validation. The full regexp
// we could use, "^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$", is O(2ⁿ) on the
// length of the string in python. An equivalent regexp that doesn't have the
// nested quantifiers that trip up Python's re would be
// "^(?:[a-z0-9]|(?<=[a-z0-9])-)*[a-z](?:[a-z0-9]|-(?=[a-z0-9]))*$", but Go's
// regexp package doesn't support look-aheads nor look-behinds, so in order to
// have a unified implementation in the Go and Python bits of the project we're
// doing it this way instead. Check the length (if applicable), check this
// regexp, then check the dashes. This still leaves sc_snap_name_validate
// (in cmd/snap-confine/snap.c) and snap_validate
// (cmd/snap-update-ns/bootstrap.c) with their own handcrafted validators.
var almostValidName = regexp.MustCompile("^[a-z0-9-]*[a-z][a-z0-9-]*$")

// validInstanceKey is a regular expression describing a valid snap instance key
var validInstanceKey = regexp.MustCompile("^[a-z0-9]{1,10}$")

// isValidName checks snap and socket identifiers.
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
	mylog.Check(ValidateSnap(storeName))

	if !validInstanceKey.MatchString(instanceKey) {
		return fmt.Errorf("invalid instance key: %q", instanceKey)
	}
	return nil
}

// ValidateSnap checks if a string can be used as a snap name.
func ValidateSnap(name string) error {
	// NOTE: This function should be synchronized with the two other
	// implementations: sc_snap_name_validate and validate_snap_name .
	if len(name) < 2 || len(name) > 40 || !isValidName(name) {
		return fmt.Errorf("invalid snap name: %q", name)
	}
	return nil
}

// Regular expression describing correct plug, slot and interface names.
var validPlugSlotIface = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidatePlug checks if a string can be used as a slot name.
//
// Slot names and plug names within one snap must have unique names.
// This is not enforced by this function but is enforced by snap-level
// validation.
func ValidatePlug(name string) error {
	if !validPlugSlotIface.MatchString(name) {
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
	if !validPlugSlotIface.MatchString(name) {
		return fmt.Errorf("invalid slot name: %q", name)
	}
	return nil
}

// ValidateInterface checks if a string can be used as an interface name.
func ValidateInterface(name string) error {
	if !validPlugSlotIface.MatchString(name) {
		return fmt.Errorf("invalid interface name: %q", name)
	}
	return nil
}

// Regular expressions describing correct identifiers.
var validHook = regexp.MustCompile("^[a-z](?:-?[a-z0-9])*$")

// ValidateHook checks if a string can be used as a hook name.
func ValidateHook(name string) error {
	valid := validHook.MatchString(name)
	if !valid {
		return fmt.Errorf("invalid hook name: %q", name)
	}
	return nil
}

// ValidAlias is a regular expression describing a valid alias
var ValidAlias = regexp.MustCompile("^[a-zA-Z0-9][-_.a-zA-Z0-9]*$")

// ValidateAlias checks if a string can be used as an alias name.
func ValidateAlias(alias string) error {
	valid := ValidAlias.MatchString(alias)
	if !valid {
		return fmt.Errorf("invalid alias name: %q", alias)
	}
	return nil
}

// ValidApp is a regular expression describing a valid application name
var ValidApp = regexp.MustCompile("^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$")

// ValidateApp tells whether a string is a valid application name.
func ValidateApp(n string) error {
	if !ValidApp.MatchString(n) {
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

// ValidateIfaceTag can be used to check valid tags in interfaces.
// These tags are used to match plugs with slots, and although they
// could be arbitrary strings it is nice to keep naming consistent
// with what we do for snap names.
func ValidateIfaceTag(name string) error {
	if !isValidName(name) {
		return fmt.Errorf("invalid tag name: %q", name)
	}
	return nil
}

// ValidSnapID is a regular expression describing a valid snapd-id
var ValidSnapID = regexp.MustCompile("^[a-z0-9A-Z]{32}$")

// ValidateSnapID checks whether the string is a valid snap-id.
func ValidateSnapID(id string) error {
	if !ValidSnapID.MatchString(id) {
		return fmt.Errorf("invalid snap-id: %q", id)
	}
	return nil
}

// ValidateSecurityTag validates known variants of snap security tag.
//
// Two forms are recognised, one for apps and one for hooks. Other forms
// are possible but are not handled here.
//
// TODO: handle the weird udev variant.
func ValidateSecurityTag(tag string) error {
	_ := mylog.Check2(ParseSecurityTag(tag))
	return err
}

// validQuotaGroupName is a regular expression describing a valid quota resource
// group name. It is the same regular expression as a snap name
var validQuotaGroupName = almostValidName

// ValidateQuotaGroup checks if a string can be used as a name for a quota
// resource group. Currently the rules are exactly the same as for snap names.
// Higher levels might also reserve some names, that is not taken into
// account by ValidateQuotaGroup itself.
func ValidateQuotaGroup(grp string) error {
	if grp == "" {
		return fmt.Errorf("invalid quota group name: must not be empty")
	}

	if len(grp) < 2 || len(grp) > 40 {
		return fmt.Errorf("invalid quota group name: must be between 2 and 40 characters long")
	}

	// check that the name matches the regexp
	if !validQuotaGroupName.MatchString(grp) {
		return fmt.Errorf("invalid quota group name: contains invalid characters (valid names start with a letter and are otherwise alphanumeric with dashes)")
	}

	if grp[0] == '-' || grp[len(grp)-1] == '-' || strings.Contains(grp, "--") {
		return fmt.Errorf("invalid quota group name: has invalid \"-\" sequences in it")
	}

	return nil
}

// ValidProvenance is a regular expression describing a valid provenance.
var ValidProvenance = regexp.MustCompile("^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$")

// DefaultProvenance is the default value for provenance, i.e the provenance for snaps uplodaded through the global store pipeline.
const DefaultProvenance = "global-upload"

// ValidateProvenance checks fi the given string is valid non-empty provenance value.
func ValidateProvenance(prov string) error {
	if prov == "" {
		return fmt.Errorf("invalid provenance: must not be empty")
	}
	if !ValidProvenance.MatchString(prov) {
		return fmt.Errorf("invalid provenance: %q", prov)
	}
	return nil
}
