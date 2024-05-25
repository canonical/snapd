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

	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/strutil"
)

var (
	ErrPermissionNotInList        = errors.New("permission not found in permissions list")
	ErrPermissionsListEmpty       = errors.New("permissions list empty")
	ErrUnrecognizedFilePermission = errors.New("file permissions mask contains unrecognized permission")
)

type Constraints struct {
	PathPattern *PathPattern `json:"path-pattern,omitempty"`
	Permissions []string     `json:"permissions,omitempty"`
}

// ValidateForInterface returns nil if the constraints are valid for the given
// interface, otherwise returns an error.
func (c *Constraints) ValidateForInterface(iface string) error {
	prefix := "invalid constraints"
	if c.PathPattern == nil {
		return fmt.Errorf("%s: no path pattern", prefix)
	}
	if err := c.validatePermissions(iface); err != nil {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return nil
}

// validatePermissions checks that the permissions for the given constraints
// are valid for the given interface. If not, returns an error, otherwise
// ensures that the permissions are in the order in which they occur in the
// list of available permissions for that interface.
func (c *Constraints) validatePermissions(iface string) error {
	availablePerms, ok := interfacePermissionsAvailable[iface]
	if !ok {
		return fmt.Errorf("unsupported interface: %s", iface)
	}
	permsSet := make(map[string]bool, len(c.Permissions))
	for _, perm := range c.Permissions {
		if !strutil.ListContains(availablePerms, perm) {
			return fmt.Errorf("unsupported permission for %s interface: %q", iface, perm)
		}
		permsSet[perm] = true
	}
	if len(permsSet) == 0 {
		return ErrPermissionsListEmpty
	}
	newPermissions := make([]string, 0, len(permsSet))
	for _, perm := range availablePerms {
		if exists := permsSet[perm]; exists {
			newPermissions = append(newPermissions, perm)
		}
	}
	c.Permissions = newPermissions
	return nil
}

// Match returns true if the constraints match the given path, otherwise false.
//
// If the constraints or path are invalid, returns an error.
func (c *Constraints) Match(path string) (bool, error) {
	if c.PathPattern == nil {
		return false, fmt.Errorf("invalid constraints: no path pattern")
	}
	return PathPatternMatch(c.PathPattern.String(), path)
}

// RemovePermission removes every instance of the given permission from the
// permissions list associated with the constraints. If the permission does
// not exist in the list, returns ErrPermissionNotInList.
func (c *Constraints) RemovePermission(permission string) error {
	newPermissions := make([]string, 0, len(c.Permissions))
	for _, perm := range c.Permissions {
		if perm != permission {
			newPermissions = append(newPermissions, perm)
		}
	}
	if len(newPermissions) == len(c.Permissions) {
		return ErrPermissionNotInList
	}
	c.Permissions = newPermissions
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
		return nil, fmt.Errorf("cannot get available permissions: unsupported interface: %s", iface)
	}
	return available, nil
}

// AbstractPermissionsFromAppArmorPermissions returns the list of permissions
// corresponding to the given AppArmor permissions for the given interface.
func AbstractPermissionsFromAppArmorPermissions(iface string, permissions interface{}) ([]string, error) {
	filePerms, ok := permissions.(notify.FilePermission)
	if !ok {
		return nil, fmt.Errorf("cannot parse the given permissions as file permissions: %v", permissions)
	}
	if filePerms == notify.FilePermission(0) {
		return nil, fmt.Errorf("cannot get abstract permissions from empty AppArmor permissions: %q", filePerms)
	}
	abstractPermsAvailable, exists := interfacePermissionsAvailable[iface]
	if !exists {
		return nil, fmt.Errorf("cannot map the given interface to list of available permissions: %s", iface)
	}
	abstractPermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		// This should never happen, since we just found a permissions list
		// for the given interface and thus a map should exist for it as well.
		return nil, fmt.Errorf("cannot map the given interface to map from abstract permissions to AppArmor permissions: %s", iface)
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
			return nil, fmt.Errorf("internal error: cannot map abstract permission to AppArmor permissions for the %s interface: %q", iface, abstractPerm)
		}
		if filePerms&aaPermMapping != 0 {
			abstractPerms = append(abstractPerms, abstractPerm)
			filePerms &= ^aaPermMapping
		}
	}
	if filePerms != notify.FilePermission(0) {
		return nil, fmt.Errorf("cannot map AppArmor permission to abstract permission for the %s interface: %q", iface, filePerms)
	}
	return abstractPerms, nil
}

// AbstractPermissionsToAppArmorPermissions returns AppArmor permissions
// corresponding to the given permissions for the given interface.
func AbstractPermissionsToAppArmorPermissions(iface string, permissions []string) (interface{}, error) {
	if len(permissions) == 0 {
		return notify.FilePermission(0), ErrPermissionsListEmpty
	}
	filePermsMap, exists := interfaceFilePermissionsMaps[iface]
	if !exists {
		return notify.FilePermission(0), fmt.Errorf("cannot map the given interface to map from abstract permissions to AppArmor permissions: %s", iface)
	}
	filePerms := notify.FilePermission(0)
	for _, perm := range permissions {
		permMask, exists := filePermsMap[perm]
		if !exists {
			// Should not occur, since stored permissions list should have been validated
			return notify.FilePermission(0), fmt.Errorf("cannot map abstract permission to AppArmor permissions for the %s interface: %q", iface, perm)
		}
		filePerms |= permMask
	}
	if filePerms&(notify.AA_MAY_EXEC|notify.AA_MAY_WRITE|notify.AA_MAY_READ|notify.AA_MAY_APPEND|notify.AA_MAY_CREATE) != 0 {
		filePerms |= notify.AA_MAY_OPEN
	}
	return filePerms, nil
}
