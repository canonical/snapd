// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package backend_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type lockingSuite struct {
	be backend.Backend
}

var _ = Suite(&lockingSuite{})

func (s *lockingSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *lockingSuite) TestRunInhibitSnapForUnlinkPositiveDecision(c *C) {
	const yaml = `name: snap-name
version: 1
`
	info := snaptest.MockInfo(c, yaml, &snap.SideInfo{Revision: snap.R(1)})
	lock, err := s.be.RunInhibitSnapForUnlink(info, "hint", func() error {
		// This decision function returns nil so the lock is established and
		// the inhibition hint is set.
		return nil
	})
	c.Assert(err, IsNil)
	c.Assert(lock, NotNil)
	lock.Close()
	hint, inhibitInfo, err := runinhibit.IsLocked(info.InstanceName())
	c.Assert(err, IsNil)
	c.Check(string(hint), Equals, "hint")
	c.Check(inhibitInfo, Equals, runinhibit.InhibitInfo{Previous: snap.R(1)})
}

func (s *lockingSuite) TestRunInhibitSnapForUnlinkNegativeDecision(c *C) {
	const yaml = `name: snap-name
version: 1
`
	info := snaptest.MockInfo(c, yaml, nil)
	lock, err := s.be.RunInhibitSnapForUnlink(info, "hint", func() error {
		// This decision function returns an error so the lock is not
		// established and the inhibition hint is not set.
		return errors.New("propagated")
	})
	c.Assert(err, ErrorMatches, "propagated")
	c.Assert(lock, IsNil)
	hint, inhibitInfo, err := runinhibit.IsLocked(info.InstanceName())
	c.Assert(err, IsNil)
	c.Check(string(hint), Equals, "")
	c.Check(inhibitInfo, Equals, runinhibit.InhibitInfo{})
}

func (s *lockingSuite) TestWithSnapLock(c *C) {
	const yaml = `name: snap-name
version: 1
`
	info := snaptest.MockInfo(c, yaml, nil)

	lock, err := snaplock.OpenLock(info.InstanceName())
	c.Assert(err, IsNil)
	defer lock.Close()
	c.Assert(lock.TryLock(), IsNil) // Lock is not held
	lock.Unlock()

	err = backend.WithSnapLock(info, func() error {
		c.Assert(lock.TryLock(), Equals, osutil.ErrAlreadyLocked) // Lock is held
		return errors.New("error-is-propagated")
	})
	c.Check(err, ErrorMatches, "error-is-propagated")

	c.Assert(lock.TryLock(), IsNil) // Lock is not held
}
