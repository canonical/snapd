// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package prompting

import (
	"errors"
	"fmt"
	"time"

	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/strutil"
)

var (
	ErrPermissionNotInList        = errors.New("permission not found in permissions list")
	ErrPermissionsListEmpty       = errors.New("permissions list empty")
	ErrUnrecognizedFilePermission = errors.New("file permissions mask contains unrecognized permission")
)

type Constraints struct {
	PathPattern string   `json:"path-pattern,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// ValidateForInterface returns nil if the constraints are valid for the given
// interface, otherwise returns an error.
func (c *Constraints) ValidateForInterface(iface string) error {
	// TODO: change to this once PR #13730 is merged:
	// if err := ValidatePathPattern(c.PathPattern); err != nil {
	//	return err
	// }
	permissions, err := AbstractPermissionsFromList(iface, c.Permissions)
	if err != nil {
		return err
	}
	c.Permissions = permissions
	return nil
}

// Match returns true if the constraints match the given path, otherwise false.
//
// If the constraints or path are invalid, returns an error.
func (c *Constraints) Match(path string) (bool, error) {
	// TODO: change to this once PR #13730 is merged:
	// return PathPatternMatch(c.PathPattern, path)
	return true, nil
}

// RemovePermission removes every instance of the given permission from the
// permissions list associated with the constraints. If the permission does
// not exist in the list, returns ErrPermissionNotInList.
func (c *Constraints) RemovePermission(permission string) error {
	origLen := len(c.Permissions)
	for i := 0; i < len(c.Permissions); {
		perm := c.Permissions[i]
		if perm != permission {
			i++
			continue
		}
		copy(c.Permissions[i:], c.Permissions[i+1:])
		c.Permissions = c.Permissions[:len(c.Permissions)-1]
	}
	if origLen == len(c.Permissions) {
		return ErrPermissionNotInList
	}
	return nil
}

// ContainPermissions returns true if the constraints include every one of the
// given permissions.
func (c *Constraints) ContainPermissions(permissions []string) bool {
	for _, perm := range permissions {
		if !strutil.ListContains(c.Permissions, perm) {
			return false
		}
	}
	return true
}

var (
	// List of permissions available for each interface. This also defines the
	// order in which the permissions should be presented.
	interfacePermissionsAvailable = map[string][]string{
		"home": {"read", "write", "execute"},
	}

	// A mapping from interfaces which support AppArmor file permissions to
	// the map between abstract permissions and those file permissions.
	//
	// Never include AA_MAY_OPEN in the maps below; it should always come from
	// the kernel with another permission (e.g. AA_MAY_READ or AA_MAY_WRITE),
	// and if it does not, it should be interpreted as AA_MAY_READ.
	interfaceFilePermissionsMaps = map[string]map[string]notify.FilePermission{
		"home": {
			"read":    notify.AA_MAY_READ,
			"write":   notify.AA_MAY_WRITE | notify.AA_MAY_APPEND | notify.AA_MAY_CREATE | notify.AA_MAY_DELETE | notify.AA_MAY_RENAME | notify.AA_MAY_CHMOD | notify.AA_MAY_LOCK | notify.AA_MAY_LINK,
			"execute": notify.AA_MAY_EXEC | notify.AA_EXEC_MMAP,
		},
	}
)

// AvailablePermissions returns the list of available permissions for the given
// interface.
func AvailablePermissions(iface string) ([]string, error) {
	available, exist := interfacePermissionsAvailable[iface]
	if !exist {
		return nil, fmt.Errorf("cannot get available permissions: unsupported interface: %q", iface)
	}
	return available, nil
}

// AbstractPermissionsFromAppArmorPermissions returns the list of permissions
// corresponding to the given AppArmor permissions for the given interface.
func AbstractPermissionsFromAppArmorPermissions(iface string, permissions interface{}) ([]string, error) {
	return abstractPermissionsFromAppArmorFilePermissions(iface, permissions)
}

// abstractPermissionsFromAppArmorFilePermissions returns the list of permissions
// corresponding to the given AppArmor file permissions for the given interface.
func abstractPermissionsFromAppArmorFilePermissions(iface string, permissions interface{}) ([]string, error) {
	filePerms, ok := permissions.(notify.FilePermission)
	if !ok {
		return nil, fmt.Errorf("failed to parse the given permissions as file permissions")
	}
	abstractPermsAvailable, exists := interfacePermissionsAvailable[iface]
	if !exists {
		// This should never happen, since iface is checked in the calling function.
		return nil, fmt.Errorf("internal error: no permissions list defined for interface: %q", iface)
	}
	abstractPermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		// This should never happen, since iface is checked in the calling function.
		return nil, fmt.Errorf("internal error: no file permissions map defined for interface: %q", iface)
	}
	if filePerms == notify.AA_MAY_OPEN {
		// Should not occur, but if a request is received for only open, treat it as read.
		filePerms = notify.AA_MAY_READ
	}
	// Discard Open permission; re-add it to the permission mask later
	filePerms &= ^notify.AA_MAY_OPEN
	abstractPerms := make([]string, 0, 1) // most requests should only include one permission
	for _, abstractPerm := range abstractPermsAvailable {
		aaPermMapping, exists := abstractPermsMap[abstractPerm]
		if !exists {
			// This should never happen, since permission mappings are
			// predefined and should be checked for correctness.
			return nil, fmt.Errorf("internal error: no permission map defined for abstract permission %q for interface %q", abstractPerm, iface)
		}
		if filePerms&aaPermMapping != 0 {
			abstractPerms = append(abstractPerms, abstractPerm)
			filePerms &= ^aaPermMapping
		}
	}
	if filePerms != notify.FilePermission(0) {
		return nil, fmt.Errorf("received unexpected permission for interface %q in AppArmor permission mask: %q", iface, filePerms)
	}
	if len(abstractPerms) == 0 {
		origMask := permissions.(notify.FilePermission)
		return nil, fmt.Errorf("no abstract permissions after parsing AppArmor permissions for interface: %q; original file permissions: %v", iface, origMask)
	}
	return abstractPerms, nil
}

// AbstractPermissionsFromList validates the given permissions list for the
// given interface and returns a list containing those permissions in the order
// in which they occur in the list of available permissions for that interface.
func AbstractPermissionsFromList(iface string, permissions []string) ([]string, error) {
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return nil, fmt.Errorf("unsupported interface: %q", iface)
	}
	permsSet := make(map[string]bool, len(permissions))
	for _, perm := range permissions {
		if !strutil.ListContains(availablePerms, perm) {
			return nil, fmt.Errorf("unsupported permission for %q interface: %q", iface, perm)
		}
		permsSet[perm] = true
	}
	if len(permsSet) == 0 {
		return nil, ErrPermissionsListEmpty
	}
	permissionsList := make([]string, 0, len(permsSet))
	for _, perm := range availablePerms {
		if exists := permsSet[perm]; exists {
			permissionsList = append(permissionsList, perm)
		}
	}
	return permissionsList, nil
}

// AbstractPermissionsToAppArmorPermissions returns AppArmor permissions
// corresponding to the given permissions for the given interface.
func AbstractPermissionsToAppArmorPermissions(iface string, permissions []string) (interface{}, error) {
	return abstractPermissionsToAppArmorFilePermissions(iface, permissions)
}

// AbstractPermissionsToAppArmorFilePermissions returns AppArmor file
// permissions corresponding to the given permissions for the given interface.
func abstractPermissionsToAppArmorFilePermissions(iface string, permissions []string) (notify.FilePermission, error) {
	if len(permissions) == 0 {
		return notify.FilePermission(0), ErrPermissionsListEmpty
	}
	filePermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		// This should never happen, since iface is checked in the calling function
		return notify.FilePermission(0), fmt.Errorf("internal error: no AppArmor file permissions map defined for interface: %q", iface)
	}
	filePerms := notify.FilePermission(0)
	for _, perm := range permissions {
		permMask, exists := filePermsMap[perm]
		if !exists {
			// Should not occur, since stored permissions list should have been validated
			return notify.FilePermission(0), fmt.Errorf("no AppArmor file permission mapping for %q interface with abstract permission: %q", iface, perm)
		}
		filePerms |= permMask
	}
	if filePerms&(notify.AA_MAY_EXEC|notify.AA_MAY_WRITE|notify.AA_MAY_READ|notify.AA_MAY_APPEND|notify.AA_MAY_CREATE) != 0 {
		filePerms |= notify.AA_MAY_OPEN
	}
	return filePerms, nil
}

// ValidateConstraintsOutcomeLifespanExpiration returns an error if the given
// constraints, outcome, lifespan, or duration are invalid, else returns nil.
func ValidateConstraintsOutcomeLifespanExpiration(iface string, constraints *Constraints, outcome OutcomeType, lifespan LifespanType, expiration *time.Time, currTime time.Time) error {
	if err := constraints.ValidateForInterface(iface); err != nil {
		return err
	}
	if err := ValidateOutcome(outcome); err != nil {
		return err
	}
	return ValidateLifespanExpiration(lifespan, expiration, currTime)
}

// ValidateConstraintsOutcomeLifespanDuration returns an error if the given
// constraints, outcome, lifespan, or duration are invalid. Otherwise, converts
// the given duration to an expiration timestamp and returns it and nil error.
func ValidateConstraintsOutcomeLifespanDuration(iface string, constraints *Constraints, outcome OutcomeType, lifespan LifespanType, duration string) (*time.Time, error) {
	if err := constraints.ValidateForInterface(iface); err != nil {
		return nil, err
	}
	if err := ValidateOutcome(outcome); err != nil {
		return nil, err
	}
	return ValidateLifespanParseDuration(lifespan, duration)
}
