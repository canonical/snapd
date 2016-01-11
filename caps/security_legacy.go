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

package caps

import (
	"fmt"

	"github.com/ubuntu-core/snappy/snappy"
)

// legacySecurtySystem aids in the ongoing work to transition capability system
// from hwaccess API to a more direct approach. It allows particular capability
// types to define a common interface that doesn't expose hwaccess API anymore.
type legacySecurtySystem struct {
	AttrName string
}

func (sec *legacySecurtySystem) GrantPermissions(snapName string, cap *Capability) error {
	const errPrefix = "cannot grant permissions"
	// Find the snap
	snap := snappy.ActiveSnapByName(snapName)
	if snap == nil {
		return fmt.Errorf("%s, no such package", errPrefix)
	}
	// Fetch the attribute where the path is stored
	path, err := cap.GetAttr(sec.AttrName)
	if err != nil {
		return fmt.Errorf("%s, cannot get required attribute: %q", errPrefix, err)
	}
	// Use hw-access layer to grant the permissions to access
	qualName := snappy.QualifiedName(snap)
	if err := snappy.AddHWAccess(qualName, path.(string)); err != nil {
		return fmt.Errorf("%s, hw-access failed: %q", errPrefix, err)
	}
	return nil
}

func (sec *legacySecurtySystem) RevokePermissions(snapName string, cap *Capability) error {
	const errPrefix = "cannot revoke permissions"
	// Find the snap
	snap := snappy.ActiveSnapByName(snapName)
	if snap == nil {
		return fmt.Errorf("%s, no such package", errPrefix)
	}
	// Fetch the attribute where the path is stored
	path, err := cap.GetAttr(sec.AttrName)
	if err != nil {
		return fmt.Errorf("%s, cannot get required attribute: %q", errPrefix, err)
	}
	// Use hw-access layer to grant the permissions to access
	qualName := snappy.QualifiedName(snap)
	if err := snappy.RemoveHWAccess(qualName, path.(string)); err != nil {
		return fmt.Errorf("%s, hw-access failed: %q", errPrefix, err)
	}
	return nil
}
