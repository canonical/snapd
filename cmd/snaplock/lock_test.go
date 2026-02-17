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

package snaplock_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type lockSuite struct{}

var _ = Suite(&lockSuite{})

func (s *lockSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *lockSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *lockSuite) TestOpenLock(c *C) {
	lock, err := snaplock.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()

	_, err = os.Stat(lock.Path())
	c.Assert(err, IsNil)

	comment := Commentf("wrong prefix for %q, want %q", lock.Path(), dirs.SnapRunLockDir)
	c.Check(strings.HasPrefix(lock.Path(), dirs.SnapRunLockDir), Equals, true, comment)
}

func (s *lockSuite) TestWithLock(c *C) {
	lock, err := snaplock.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()
	c.Assert(lock.TryLock(), IsNil) // lock is not held
	lock.Unlock()

	err = snaplock.WithLock("name", func() error {
		c.Assert(lock.TryLock(), Equals, osutil.ErrAlreadyLocked) // lock is held
		return errors.New("error-is-propagated")
	})
	c.Check(err, ErrorMatches, "error-is-propagated")

	c.Assert(lock.TryLock(), IsNil) // lock was not held and we took it
	lock.Unlock()
}

func (s *lockSuite) TestWithTryLock(c *C) {
	lock, err := snaplock.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()
	c.Assert(lock.TryLock(), IsNil) // lock is not held
	lock.Unlock()

	err = snaplock.WithTryLock("name", func() error {
		c.Assert(lock.TryLock(), Equals, osutil.ErrAlreadyLocked) // lock is held
		return errors.New("error-is-propagated")
	})
	c.Check(err, ErrorMatches, "error-is-propagated")

	c.Assert(lock.TryLock(), IsNil) // lock was not held and we took it
	lock.Unlock()

	// so far this was identical to snaplock.WithLock(), now check the Try part

	called := false
	err = snaplock.WithTryLock("name", func() error {
		called = true
		// try nesting the lock
		internalErr := snaplock.WithTryLock("name", func() error {
			panic("unexpected call")
		})
		c.Assert(internalErr, testutil.ErrorIs, osutil.ErrAlreadyLocked)
		return nil
	})
	c.Assert(called, Equals, true)

	// take the lock
	c.Check(lock.TryLock(), IsNil)
	err = snaplock.WithTryLock("name", func() error {
		panic("unexpected call")
	})
	c.Assert(err, testutil.ErrorIs, osutil.ErrAlreadyLocked)
	lock.Unlock()
}
