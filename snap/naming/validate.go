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

// ValidateProvenance checks if the given string is valid non-empty provenance value.
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

// validateAssumedSnapdVersion checks if the snapd version requirement is valid
// and satisfied by the current snapd version.
func validateAssumedSnapdVersion(assumedVersion, currentVersion string) (bool, error) {
	// double check that the input looks like a snapd version
	reqVersionNumMatch := snapdVersionExp.FindStringSubmatch(assumedVersion)
	if reqVersionNumMatch == nil {
		return false, nil
	}

	if currentVersion == "" {
		// Skip checking the assumed version against the current snapd version
		return true, nil
	}

	// this check ensures that no one can use an assumes like snapd2.48.3~pre2
	// or snapd2.48.5+20.10, as modifiers past the version number are not meant
	// to be relied on for snaps via assumes, however the check against the real
	// snapd version number below allows such non-numeric modifiers since real
	// snapds do have versions like that (for example debian pkg of snapd)
	if reqVersionNumMatch[0] != assumedVersion {
		return false, nil
	}

	req := strings.Split(reqVersionNumMatch[0], ".")

	if currentVersion == "unknown" {
		return true, nil // Development tree.
	}

	// We could (should?) use strutil.VersionCompare here and simplify
	// this code (see PR#7344). However this would change current
	// behavior, i.e. "2.41~pre1" would *not* match [snapd2.41] anymore
	// (which the code below does).
	curVersionNumMatch := snapdVersionExp.FindStringSubmatch(currentVersion)
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

// validateAssumedISAArch checks that, when a snap requires an ISA to be supported:
//  1. compares the specified <arch> with the device's one. If they differ, it exits
//     without error signaling that they flag is valid
//  2. if the specified <arch> matches the device's one, the support for specifying ISAs
//     for that architecture is verified. If it's absent, an error is returned
//  3. if ISA specification is supported for that architecture, the arch-specific function,
//     defined in the arch-specific file, is called. If no error is returned then the key
//     is considered valid.
func validateAssumedISAArch(flag string, currentArchitecture string) error {
	if currentArchitecture == "" {
		// Skip checking the assumed ISA against the currently running architecture
		return nil
	}

	// we allow keys like isa-<arch>-<isa_val>, so the result of the split will
	// always be {"isa", "<arch>", "<isa_val>"}
	tokens := strings.SplitN(flag, "-", 3)
	if len(tokens) != 3 {
		return fmt.Errorf("%s: must be in the format isa-<arch>-<isa_val>", flag)
	}

	if currentArchitecture != tokens[1] {
		// Skip, it doesn't make sense to verify the ISA for architectures we
		// are not running on
		return nil
	}

	// Run architecture-dependent compatibility checks
	var err error
	switch tokens[1] {
	case "riscv64":
		err = validateAssumesRiscvISA(tokens[2])

	default:
		return fmt.Errorf("%s: ISA specification is not supported for arch: %s", flag, tokens[1])
	}

	if err != nil {
		return fmt.Errorf("%s: validation failed: %s", flag, err)
	}
	return nil
}

// assumeFormat matches the expected string format for assume flags.
var assumeFormat = regexp.MustCompile("^[a-z0-9]+(?:-[a-z0-9]+)*$")

// ValidateAssumes checks if `assumes` lists features that are all supported.
// Pass empty currentSnapdVersion to skip checking the assumed version against the current snap version.
// Pass nil/empty featureSet to only validate assumes format & not feature support.
// Pass empty currentArchitecture to skip checking the assumed ISAs against the current device architecture
func ValidateAssumes(assumes []string, currentSnapdVersion string, featureSet map[string]bool, currentArchitecture string) error {
	var failed []string
	for _, flag := range assumes {
		if strings.HasPrefix(flag, "snapd") {
			validVersion, err := validateAssumedSnapdVersion(flag[5:], currentSnapdVersion)
			if err != nil {
				// error not possible unless someone has messed up the regex
				return err
			}

			if validVersion {
				continue
			}
		}

		if strings.HasPrefix(flag, "isa-") {
			err := validateAssumedISAArch(flag, currentArchitecture)
			if err != nil {
				return err
			}
			continue
		}

		// if featureSet is provided, check feature support;
		// otherwise only validate format
		isValid := false
		if len(featureSet) > 0 {
			isValid = featureSet[flag]
		} else {
			isValid = assumeFormat.MatchString(flag)
		}

		if !isValid {
			failed = append(failed, flag)
		}
	}

	if len(failed) > 0 {
		if currentSnapdVersion == "" && len(featureSet) == 0 {
			return fmt.Errorf("invalid features: %s", strings.Join(failed, ", "))
		}

		return fmt.Errorf("unsupported features: %s", strings.Join(failed, ", "))
	}

	return nil
}
