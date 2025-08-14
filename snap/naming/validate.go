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
	"strconv"
	"strings"

	"github.com/snapcore/snapd/snapdtool"
)

// almostValidNameRegexString is part of snap and socket name validation. The
// full regexp we could use, "^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$", is O(2ⁿ)
// on the length of the string in python. An equivalent regexp that doesn't have
// the nested quantifiers that trip up Python's re would be
// "^(?:[a-z0-9]|(?<=[a-z0-9])-)*[a-z](?:[a-z0-9]|-(?=[a-z0-9]))*$", but Go's
// regexp package doesn't support look-aheads nor look-behinds, so in order to
// have a unified implementation in the Go and Python bits of the project we're
// doing it this way instead. Check the length (if applicable), check this
// regexp, then check the dashes. This still leaves sc_snap_name_validate (in
// cmd/snap-confine/snap.c) and snap_validate (cmd/snap-update-ns/bootstrap.c)
// with their own handcrafted validators.
const almostValidNameRegexString = "[a-z0-9-]*[a-z][a-z0-9-]*"

var almostValidName = regexp.MustCompile(fmt.Sprintf("^%s$", almostValidNameRegexString))

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
	if err := ValidateSnap(storeName); err != nil {
		return err
	}
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
	_, err := ParseSecurityTag(tag)
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

// regular expression which matches a version expressed as groups of digits
// separated with dots, with optional non-numbers afterwards
var snapdVersionExp = regexp.MustCompile(`^(?:[1-9][0-9]*)(?:\.(?:[0-9]+))*`)

func validateSnapdVersion(version string) (bool, error) {
	// double check that the input looks like a snapd version
	reqVersionNumMatch := snapdVersionExp.FindStringSubmatch(version)
	if reqVersionNumMatch == nil {
		return false, nil
	}
	// this check ensures that no one can use an assumes like snapd2.48.3~pre2
	// or snapd2.48.5+20.10, as modifiers past the version number are not meant
	// to be relied on for snaps via assumes, however the check against the real
	// snapd version number below allows such non-numeric modifiers since real
	// snapds do have versions like that (for example debian pkg of snapd)
	if reqVersionNumMatch[0] != version {
		return false, nil
	}

	req := strings.Split(reqVersionNumMatch[0], ".")

	if snapdtool.Version == "unknown" {
		return true, nil // Development tree.
	}

	// We could (should?) use strutil.VersionCompare here and simplify
	// this code (see PR#7344). However this would change current
	// behavior, i.e. "2.41~pre1" would *not* match [snapd2.41] anymore
	// (which the code below does).
	curVersionNumMatch := snapdVersionExp.FindStringSubmatch(snapdtool.Version)
	if curVersionNumMatch == nil {
		return false, nil
	}
	cur := strings.Split(curVersionNumMatch[0], ".")

	for i := range req {
		if i == len(cur) {
			// we hit the end of the elements of the current version number and have
			// more required version numbers left, so this doesn't match, if the
			// previous element was higher we would have broken out already, so the
			// only case left here is where we have version requirements that are
			// not met
			return false, nil
		}
		reqN, err1 := strconv.Atoi(req[i])
		curN, err2 := strconv.Atoi(cur[i])
		if err1 != nil || err2 != nil {
			// error not possible unless someone has messed up the regex
			return false, fmt.Errorf("version regexp is broken")
		}
		if curN != reqN {
			return curN > reqN, nil
		}
	}

	return true, nil
}

// assumesFeatureSet contains the flag values that can be listed in assumes entries
// that this ubuntu-core actually provides.
var assumesFeatureSet = map[string]bool{
	// Support for common data directory across revisions of a snap.
	"common-data-dir": true,
	// Support for the "Environment:" feature in snap.yaml
	"snap-env": true,
	// Support for the "command-chain" feature for apps and hooks in snap.yaml
	"command-chain": true,
	// Support for "kernel-assets" in gadget.yaml. I.e. having volume
	// content of the style $kernel:ref`
	"kernel-assets": true,
	// Support for "refresh-mode: ignore-running" in snap.yaml
	"app-refresh-mode": true,
	// Support for "SNAP_UID" and "SNAP_EUID" environment variables
	"snap-uid-envvars": true,
}

func ValidateAssumes(assumes []string) error {
	missing := ([]string)(nil)
	for _, flag := range assumes {
		if strings.HasPrefix(flag, "snapd") {
			validVersion, err := validateSnapdVersion(flag[5:])
			if err != nil {
				// error not possible unless someone has messed up the regex
				return err
			}

			if validVersion {
				continue
			}
		}

		if !assumesFeatureSet[flag] {
			missing = append(missing, flag)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("unsupported features: %s", strings.Join(missing, ", "))
	}

	return nil
}
