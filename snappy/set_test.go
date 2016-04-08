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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
)

func (s *SnapTestSuite) TestParseSetPropertyCmdlineEmpty(c *C) {
	err := SetProperty("ubuntu-core", &MockProgressMeter{}, []string{}...)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSetProperty(c *C) {
	var ratingPkg, ratingVal string
	mockSetRating := func(k, v string, p progress.Meter) error {
		ratingPkg = k
		ratingVal = v
		return nil
	}
	setFuncs = map[string]func(k, v string, p progress.Meter) error{
		"rating": mockSetRating,
		"null":   func(k, v string, pb progress.Meter) error { return nil },
	}
	meter := &MockProgressMeter{}

	// the "null" property in this test does nothing, its just
	// there to ensure that setFunc works with multiple entries
	err := SetProperty("hello-world", meter, "null=1.61")
	c.Assert(err, IsNil)

	// simple-case for set
	err = SetProperty("hello-world", meter, "rating=4")
	c.Assert(err, IsNil)
	c.Assert(ratingPkg, Equals, "hello-world")
	c.Assert(ratingVal, Equals, "4")

	// ensure unknown property raises a error
	err = SetProperty("ubuntu-core", meter, "no-such-property=foo")
	c.Assert(err, NotNil)

	// ensure incorrect format raises a error
	err = SetProperty("hello-world", meter, "rating")
	c.Assert(err, NotNil)

	// ensure additional "=" in SetProperty are ok (even though this is
	// not a valid rating of course)
	err = SetProperty("hello-world", meter, "rating=1=2")
	c.Assert(err, IsNil)
	c.Assert(ratingPkg, Equals, "hello-world")
	c.Assert(ratingVal, Equals, "1=2")
}

func (s *SnapTestSuite) TestSetActive(c *C) {
	makeTwoTestSnaps(c, snap.TypeApp)

	path, err := filepath.EvalSymlinks(filepath.Join(dirs.SnapSnapsDir, fooComposedName, "current"))
	c.Assert(err, IsNil)
	c.Check(path, Equals, filepath.Join(dirs.SnapSnapsDir, fooComposedName, "200"))

	path, err = filepath.EvalSymlinks(filepath.Join(dirs.SnapDataDir, fooComposedName, "current"))
	c.Assert(err, IsNil)
	c.Check(path, Equals, filepath.Join(dirs.SnapDataDir, fooComposedName, "200"))

	meter := &MockProgressMeter{}

	// setActive has some ugly print
	devnull, err := os.Open(os.DevNull)
	c.Assert(err, IsNil)
	oldStdout := os.Stdout
	os.Stdout = devnull
	defer func() {
		os.Stdout = oldStdout
	}()

	err = makeSnapActiveByNameAndVersion("foo", "1.0", meter)
	c.Assert(err, IsNil)
	path, _ = filepath.EvalSymlinks(filepath.Join(dirs.SnapSnapsDir, fooComposedName, "current"))
	c.Check(path, Equals, filepath.Join(dirs.SnapSnapsDir, fooComposedName, "100"))
	path, _ = filepath.EvalSymlinks(filepath.Join(dirs.SnapDataDir, fooComposedName, "current"))
	c.Check(path, Equals, filepath.Join(dirs.SnapDataDir, fooComposedName, "100"))
}
