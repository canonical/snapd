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

package cgroup_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

type freezerSuite struct{}

var _ = Suite(&freezerSuite{})

func (s *freezerSuite) TestFreezeSnapProcesses(c *C) {
	restore := cgroup.MockFreezerCgroupDir(c)
	defer restore()

	n := "foo"                                                               // snap name
	p := filepath.Join(cgroup.FreezerCgroupDir(), fmt.Sprintf("snap.%s", n)) // snap freezer cgroup
	f := filepath.Join(p, "freezer.state")                                   // freezer.state file of the cgroup

	// When the freezer cgroup filesystem doesn't exist we do nothing at all.
	c.Assert(cgroup.FreezeSnapProcesses(n), IsNil)
	_, err := os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the freezer cgroup filesystem exists but the particular cgroup
	// doesn't exist we don nothing at all.
	c.Assert(os.MkdirAll(cgroup.FreezerCgroupDir(), 0755), IsNil)
	c.Assert(cgroup.FreezeSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the cgroup exists we write FROZEN the freezer.state file.
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(cgroup.FreezeSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	c.Assert(f, testutil.FileEquals, `FROZEN`)
}

func (s *freezerSuite) TestThawSnapProcesses(c *C) {
	restore := cgroup.MockFreezerCgroupDir(c)
	defer restore()

	n := "foo"                                                               // snap name
	p := filepath.Join(cgroup.FreezerCgroupDir(), fmt.Sprintf("snap.%s", n)) // snap freezer cgroup
	f := filepath.Join(p, "freezer.state")                                   // freezer.state file of the cgroup

	// When the freezer cgroup filesystem doesn't exist we do nothing at all.
	c.Assert(cgroup.ThawSnapProcesses(n), IsNil)
	_, err := os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the freezer cgroup filesystem exists but the particular cgroup
	// doesn't exist we don nothing at all.
	c.Assert(os.MkdirAll(cgroup.FreezerCgroupDir(), 0755), IsNil)
	c.Assert(cgroup.ThawSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(os.IsNotExist(err), Equals, true)

	// When the cgroup exists we write THAWED the freezer.state file.
	c.Assert(os.MkdirAll(p, 0755), IsNil)
	c.Assert(cgroup.ThawSnapProcesses(n), IsNil)
	_, err = os.Stat(f)
	c.Assert(err, IsNil)
	c.Assert(f, testutil.FileEquals, `THAWED`)
}
