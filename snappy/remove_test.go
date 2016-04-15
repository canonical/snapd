// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

func (s *SnapTestSuite) TestUnlinkSnapActiveVsNotActive(c *C) {
	foo1, foo2 := makeTwoTestSnaps(c, snap.TypeApp)

	err := UnlinkSnap(foo2, &progress.NullProgress{})
	c.Assert(err, IsNil)

	err = UnlinkSnap(foo1, &progress.NullProgress{})
	c.Assert(err, Equals, ErrSnapNotActive)
}

func (s *SnapTestSuite) TestCanRemoveGadget(c *C) {
	foo1, foo2 := makeTwoTestSnaps(c, snap.TypeGadget)

	c.Check(CanRemove(foo2, true), Equals, false)

	c.Check(CanRemove(foo1, false), Equals, true)
}
