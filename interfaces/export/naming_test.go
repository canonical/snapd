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

package export_test

import (
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/export"
	"github.com/snapcore/snapd/snap"
)

type namingSuite struct{}

var _ = Suite(&namingSuite{})

func (s *namingSuite) TestUnitNameSnapOnly(c *C) {
	name := export.UnitName("foo", "egl-slot", snap.R(34), "", snap.Revision{})
	c.Check(name, Equals, "foo_egl-slot_34")
}

func (s *namingSuite) TestUnitNameComponent(c *C) {
	name := export.UnitName("foo", "egl-slot", snap.R(34), "comp1", snap.R(12))
	c.Check(name, Equals, "foo_egl-slot_34+comp1_12")
}

func (s *namingSuite) TestUnitNameMultipleComponentsAreDistinctUnits(c *C) {
	// Each component gets its own unit - the design explicitly rejected
	// aggregating all of a snap's components into a single unit name (see
	// UnitName doc comment), so two different components must never
	// collapse onto the same unit name.
	n1 := export.UnitName("foo", "egl-slot", snap.R(34), "comp1", snap.R(12))
	n2 := export.UnitName("foo", "egl-slot", snap.R(34), "comp2", snap.R(22))
	n0 := export.UnitName("foo", "egl-slot", snap.R(34), "", snap.Revision{})

	c.Check(n1, Not(Equals), n2)
	c.Check(n0, Not(Equals), n1)
	c.Check(n0, Not(Equals), n2)
}

func (s *namingSuite) TestUnitNameDeterministic(c *C) {
	n1 := export.UnitName("foo", "egl-slot", snap.R(34), "comp1", snap.R(12))
	n2 := export.UnitName("foo", "egl-slot", snap.R(34), "comp1", snap.R(12))
	c.Check(n1, Equals, n2)
}

func (s *namingSuite) TestUnitNameSnapRevisionAlwaysSignificant(c *C) {
	// Same component and component revision, different snap revision:
	// must produce a different unit, because the snap revision decides
	// which subdirectory of the component is scanned and what priority
	// its files get (see interfaces.SnapAppSet.ExpandSliceSnapVariablesWithOrder).
	n1 := export.UnitName("foo", "egl-slot", snap.R(33), "comp1", snap.R(12))
	n2 := export.UnitName("foo", "egl-slot", snap.R(34), "comp1", snap.R(12))
	c.Check(n1, Not(Equals), n2)
}

func (s *namingSuite) TestUnitNameDifferentSlotsDoNotCollide(c *C) {
	// A snap exposing two slots of the same interface must not collide,
	// even with otherwise identical inputs.
	n1 := export.UnitName("foo", "egl-slot-a", snap.R(34), "", snap.Revision{})
	n2 := export.UnitName("foo", "egl-slot-b", snap.R(34), "", snap.Revision{})
	c.Check(n1, Not(Equals), n2)
}

func (s *namingSuite) TestUnitNameParallelInstancesDoNotCollide(c *C) {
	n1 := export.UnitName("foo", "egl-slot", snap.R(34), "", snap.Revision{})
	n2 := export.UnitName("foo_instance", "egl-slot", snap.R(34), "", snap.Revision{})
	c.Check(n1, Not(Equals), n2)
}

func (s *namingSuite) TestUnitNameLengthBound(c *C) {
	// Worst-case realistic name: a 40 character snap name (the maximum
	// allowed by snap/naming.ValidateSnap) with a 10 character instance
	// key, a 40 character slot name, and a 40 character component name,
	// each paired with a plausible revision. This must stay comfortably
	// under NAME_MAX (255 bytes on Linux), since the unit name becomes a
	// directory entry on disk.
	longSnapName := strings.Repeat("a", 40)
	longInstanceKey := strings.Repeat("b", 10)
	instance := longSnapName + "_" + longInstanceKey
	longSlot := strings.Repeat("c", 40)
	longComp := strings.Repeat("d", 40)
	rev := snap.R(9999999)

	name := export.UnitName(instance, longSlot, rev, longComp, rev)
	c.Check(len(name) <= 255, Equals, true, Commentf("unit name %q is %d bytes long", name, len(name)))
}
