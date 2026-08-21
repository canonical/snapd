// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package export

import (
	"fmt"

	"github.com/snapcore/snapd/snap"
)

// UnitName returns the name of the on-disk unit directory that groups
// together the files exported for a single connection, contributed by a
// single container: either the snap itself (component == "") or exactly
// one of its components.
//
// The snap revision is always part of the name, even when component is
// set, because the snap revision determines which subdirectory of the
// component is scanned by the interface, and the priority/index assigned
// to its files - so changing the snap revision can change the resulting
// content even if the component itself is unchanged (see
// interfaces.SnapAppSet.ExpandSliceSnapVariablesWithOrder).
//
// Exactly one container is referenced per unit - never an aggregate of all
// of a snap's components - so that the name stays bounded regardless of how
// many components a snap has. Component names are capped at 40 characters
// (snap/naming.ValidateSnap, used by snap/naming.ComponentRef.Validate),
// but there is no limit on the number of components a snap may have;
// aggregating all of them into one name would make the result unbounded
// (a driver snap with one component per GPU family could plausibly have
// five to ten).
//
// The result is deterministic given its inputs, which gives two useful
// properties: reverting a snap or a component reuses the unit already on
// disk (if not yet garbage-collected) with no writes needed, and two
// concurrent writers computing the unit for the same content produce
// identical, idempotent output rather than colliding.
func UnitName(instanceName, slotName string, snapRev snap.Revision, component string, componentRev snap.Revision) string {
	base := fmt.Sprintf("%s_%s_%s", instanceName, slotName, snapRev)
	if component == "" {
		return base
	}
	return fmt.Sprintf("%s+%s_%s", base, component, componentRev)
}
