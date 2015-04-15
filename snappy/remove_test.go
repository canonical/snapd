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
	. "launchpad.net/gocheck"
	"launchpad.net/snappy/progress"
)

func (s *SnapTestSuite) TestRemoveNonExistingRaisesError(c *C) {
	pkgName := "some-random-non-existing-stuff"
	err := Remove(pkgName, 0, &progress.NullProgress{})
	c.Assert(err, NotNil)
	c.Assert(err, Equals, ErrPackageNotFound)
}

func (s *SnapTestSuite) TestSnapRemoveByVersion(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)

	err := Remove("foo=1.0", 0, &progress.NullProgress{})

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Version(), Equals, "2.0")
}

func (s *SnapTestSuite) TestSnapRemoveActive(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)

	err := Remove("foo", 0, &progress.NullProgress{})

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Version(), Equals, "1.0")
}

func (s *SnapTestSuite) TestSnapRemoveActiveOemFails(c *C) {
	makeTwoTestSnaps(c, SnapTypeOem)

	err := Remove("foo", 0, &progress.NullProgress{})
	c.Assert(err, DeepEquals, ErrPackageNotRemovable)

	err = Remove("foo=1.0", 0, &progress.NullProgress{})
	c.Assert(err, IsNil)

	err = Remove("foo", 0, &progress.NullProgress{})
	c.Assert(err, DeepEquals, ErrPackageNotRemovable)

	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Assert(installed[0].Name(), Equals, "foo")
	c.Assert(installed[0].Type(), Equals, SnapTypeOem)
	c.Assert(installed[0].Version(), Equals, "2.0")
	c.Assert(installed, HasLen, 1)
}

func (s *SnapTestSuite) TestSnapRemoveGC(c *C) {
	makeTwoTestSnaps(c, SnapTypeApp)
	err := Remove("foo", DoRemoveGC, &progress.NullProgress{})
	c.Assert(err, IsNil)
	m := NewMetaRepository()
	installed, err := m.Installed()
	c.Assert(err, IsNil)
	c.Check(installed, HasLen, 0)
}
