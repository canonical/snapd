// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package snap_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
)

type processInfoSuite struct{}

var _ = Suite(&processInfoSuite{})

func (s *processInfoSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *processInfoSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *processInfoSuite) TestNameFromPidHappy(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "hello-world", nil
	})
	defer restore()
	restore = snap.MockApparmorSnapNameFromPid(func(pid int) (string, string, string, error) {
		c.Assert(pid, Equals, 333)
		return "hello-world", "app", "", nil
	})
	defer restore()
	info, err := snap.NameFromPid(333)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName, Equals, "hello-world")
	c.Check(info.AppName, Equals, "app")
	c.Check(info.HookName, Equals, "")
}

func (s *processInfoSuite) TestNameFromPidNoAppArmor(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "hello-world", nil
	})
	defer restore()
	restore = snap.MockApparmorSnapNameFromPid(func(pid int) (string, string, string, error) {
		c.Assert(pid, Equals, 333)
		return "", "", "", errors.New("no label")
	})
	defer restore()
	info, err := snap.NameFromPid(333)
	c.Assert(err, IsNil)
	c.Check(info.InstanceName, Equals, "hello-world")
	c.Check(info.AppName, Equals, "")
	c.Check(info.HookName, Equals, "")
}

func (s *processInfoSuite) TestNameFromPidUnhappy(c *C) {
	restore := snap.MockCgroupSnapNameFromPid(func(pid int) (string, error) {
		c.Assert(pid, Equals, 333)
		return "", errors.New("nada")
	})
	defer restore()
	restore = snap.MockApparmorSnapNameFromPid(func(pid int) (string, string, string, error) {
		c.Error("unexpected appArmorLabelForPid call")
		return "", "", "", errors.New("no label")
	})
	defer restore()
	info, err := snap.NameFromPid(333)
	c.Assert(err, ErrorMatches, "nada")
	c.Check(info, DeepEquals, snap.ProcessInfo{})
}
