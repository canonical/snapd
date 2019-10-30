// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package naming_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
)

type snapRefSuite struct{}

var _ = Suite(&snapRefSuite{})

func (s *snapRefSuite) TestNewSnapRef(c *C) {
	fooRef := naming.NewSnapRef("foo", "foo-id")
	c.Check(fooRef.SnapName(), Equals, "foo")
	c.Check(fooRef.ID(), Equals, "foo-id")

	fooNameOnlyRef := naming.NewSnapRef("foo", "")
	c.Check(fooNameOnlyRef.SnapName(), Equals, "foo")
	c.Check(fooNameOnlyRef.ID(), Equals, "")
}

func (s *snapRefSuite) TestSnap(c *C) {
	fooNameOnlyRef := naming.Snap("foo")
	c.Check(fooNameOnlyRef.SnapName(), Equals, "foo")
	c.Check(fooNameOnlyRef.ID(), Equals, "")
}

func (s *snapRefSuite) TestSameSnap(c *C) {
	fooRef := naming.NewSnapRef("foo", "foo-id")
	fooNameOnlyRef := naming.NewSnapRef("foo", "")
	altFooRef := naming.NewSnapRef("foo-proj", "foo-id")
	barNameOnylRef := naming.NewSnapRef("bar", "")
	unrelFooRef := naming.NewSnapRef("foo", "unrel-id")

	c.Check(naming.SameSnap(fooRef, altFooRef), Equals, true)
	c.Check(naming.SameSnap(fooRef, fooNameOnlyRef), Equals, true)
	c.Check(naming.SameSnap(altFooRef, fooNameOnlyRef), Equals, false)
	c.Check(naming.SameSnap(unrelFooRef, fooRef), Equals, false)
	c.Check(naming.SameSnap(fooRef, barNameOnylRef), Equals, false)
	// weakness but expected
	c.Check(naming.SameSnap(unrelFooRef, fooNameOnlyRef), Equals, true)
}

func (s *snapRefSuite) TestSnapSet(c *C) {
	ss := naming.NewSnapSet(nil)
	c.Check(ss.Empty(), Equals, true)

	fooRef := naming.NewSnapRef("foo", "foo-id")
	fooNameOnlyRef := naming.Snap("foo")

	ss.Add(fooRef)
	c.Check(ss.Empty(), Equals, false)
	ss.Add(fooNameOnlyRef)

	altFooRef := naming.NewSnapRef("foo-proj", "foo-id")
	c.Check(ss.Lookup(fooRef), Equals, fooRef)
	c.Check(ss.Lookup(fooNameOnlyRef), Equals, fooRef)
	c.Check(ss.Lookup(altFooRef), Equals, fooRef)

	barNameOnylRef := naming.NewSnapRef("bar", "")
	unrelFooRef := naming.NewSnapRef("foo", "unrel-id")
	c.Check(ss.Lookup(barNameOnylRef), Equals, nil)
	c.Check(ss.Lookup(unrelFooRef), Equals, nil)

	// weaker behavior but expected
	ss1 := naming.NewSnapSet([]naming.SnapRef{fooNameOnlyRef})
	c.Check(ss.Empty(), Equals, false)
	ss1.Add(fooRef)
	c.Check(ss1.Lookup(fooRef), Equals, fooNameOnlyRef)
	c.Check(ss1.Lookup(altFooRef), Equals, nil)
	c.Check(ss1.Lookup(barNameOnylRef), Equals, nil)
	c.Check(ss1.Lookup(unrelFooRef), Equals, fooNameOnlyRef)
}
