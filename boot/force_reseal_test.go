// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package boot_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
)

type forceResealSuite struct {
	restoreModeenvLock        func()
	restoreResealKeyToModeenv func()
}

var _ = Suite(&forceResealSuite{})

func (s *forceResealSuite) SetUpTest(c *C) {
	rootdir := c.MkDir()
	dirs.SetRootDir(rootdir)
}

func (s *forceResealSuite) TestForceResealHappy(c *C) {
	u := mockUnlocker{}

	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	restoreResealKeyToModeenv := boot.MockResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, options *boot.ResealToModeenvOptions, unlocker boot.Unlocker) error {
		c.Assert(rootdir, Equals, dirs.GlobalRootDir)
		c.Assert(options.Force, Equals, true)
		defer unlocker()()
		return nil
	})
	defer restoreResealKeyToModeenv()

	err = boot.ForceReseal(u.unlocker)
	c.Assert(err, IsNil)

	c.Assert(u.unlocked, Equals, 1)
}

func (s *forceResealSuite) TestForceResealError(c *C) {
	u := mockUnlocker{}

	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")

	restoreResealKeyToModeenv := boot.MockResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, options *boot.ResealToModeenvOptions, unlocker boot.Unlocker) error {
		c.Assert(rootdir, Equals, dirs.GlobalRootDir)
		c.Assert(options.Force, Equals, true)
		return fmt.Errorf(`CUSTOMERROR`)
	})
	defer restoreResealKeyToModeenv()

	err = boot.ForceReseal(u.unlocker)
	c.Assert(err, ErrorMatches, `CUSTOMERROR`)
}
