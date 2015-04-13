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

package helpers

import (
	"os"
	"path/filepath"
	"time"

	. "launchpad.net/gocheck"
)

func (ts *HTestSuite) TestUpdateTimestamp(c *C) {
	// empirical
	minRes := 25 * time.Millisecond

	d := c.MkDir()
	foo := filepath.Join(d, "foo")
	bar := filepath.Join(d, "bar")
	c.Assert(os.Symlink(foo, bar), IsNil)
	time.Sleep(minRes)
	c.Assert(os.Mkdir(foo, 0755), IsNil)
	// so now foo (the dir) has a timestamp after bar's (the symlink)
	// let's check that:
	fifoo, err := os.Lstat(foo)
	c.Assert(err, IsNil)
	fibar, err := os.Lstat(bar)
	c.Assert(err, IsNil)
	c.Assert(fifoo.ModTime().After(fibar.ModTime()), Equals, true)
	// and bar is a symlink to foo
	fifoox, err := os.Stat(bar)
	c.Assert(err, IsNil)
	c.Assert(fifoo.ModTime(), Equals, fifoox.ModTime())
	// ok.
	time.Sleep(minRes)

	// this should update bar's timestamp to be newer than foo's
	// (even though bar is a symlink to foo)
	c.Assert(UpdateTimestamp(bar), IsNil)

	fifoo, err = os.Lstat(foo)
	c.Assert(err, IsNil)
	fibar, err = os.Lstat(bar)
	c.Assert(err, IsNil)
	c.Assert(fifoo.ModTime().Before(fibar.ModTime()), Equals, true)
}

func (ts *HTestSuite) TestUpdateTimestampDoesNotCreate(c *C) {
	d := c.MkDir()
	foo := filepath.Join(d, "foo")

	c.Check(UpdateTimestamp(foo), NotNil)
	_, err := os.Stat(foo)
	c.Check(os.IsNotExist(err), Equals, true)
}

func (ts *HTestSuite) TestUpdateTimestampBailsOnRelative(c *C) {
	c.Check(UpdateTimestamp("./foo"), Equals, ErrNotAbsPath)
}
