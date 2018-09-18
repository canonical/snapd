// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/testutil"
)

type freezerSuite struct{}

var _ = Suite(&freezerSuite{})

func (s *freezerSuite) TestFreezeSnapProcesses(c *C) {
	restore := update.MockFreezerCgroupDir(c)
	defer restore()

	n := "foo"                                                               // snap name
	p := filepath.Join(update.FreezerCgroupDir(), fmt.Sprintf("snap.%s", n)) // snap freezer cgroup
	f := filepath.Join(p, "freezer.state")                                   // freezer.state file of the cgroup

	// When the freezer cgroup filesystem doesn't exist we do nothing at all.
	c.Assert(update.FreezeSnapProcesses(n), IsNil)
	_, err := os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the freezer cgroup filesystem exists but the particular cgroup
	// doesn't exist we don nothing at all.
	c.Assert(os.MkdirAll(update.FreezerCgroupDir(), 0755), IsNil)
	c.Assert(update.FreezeSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the cgroup exists we write FROZEN the freezer.state file.
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(update.FreezeSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	c.Assert(f, testutil.FileEquals, `FROZEN`)
}

func (s *freezerSuite) TestThawSnapProcesses(c *C) {
	restore := update.MockFreezerCgroupDir(c)
	defer restore()

	n := "foo"                                                               // snap name
	p := filepath.Join(update.FreezerCgroupDir(), fmt.Sprintf("snap.%s", n)) // snap freezer cgroup
	f := filepath.Join(p, "freezer.state")                                   // freezer.state file of the cgroup

	// When the freezer cgroup filesystem doesn't exist we do nothing at all.
	c.Assert(update.ThawSnapProcesses(n), IsNil)
	_, err := os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the freezer cgroup filesystem exists but the particular cgroup
	// doesn't exist we don nothing at all.
	c.Assert(os.MkdirAll(update.FreezerCgroupDir(), 0755), IsNil)
	c.Assert(update.ThawSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the cgroup exists we write THAWED the freezer.state file.
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(update.ThawSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	c.Assert(f, testutil.FileEquals, `THAWED`)
}
