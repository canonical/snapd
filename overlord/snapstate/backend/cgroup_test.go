// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

type cgroupSuite struct{}

var _ = Suite(&cgroupSuite{})

func (s *cgroupSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *cgroupSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *cgroupSuite) TestRemoveSnapFreezerCgroupLocks(c *C) {
	var removeErr error
	s.testRemoveSnapFreezerCgroupLocks(c, removeErr)
}

func (s *cgroupSuite) TestRemoveSnapFreezerCgroupLocksOnError(c *C) {
	removeErr := errors.New("busy")
	s.testRemoveSnapFreezerCgroupLocks(c, removeErr)
}

func (s *cgroupSuite) testRemoveSnapFreezerCgroupLocks(c *C, removeErr error) {
	defer cgroup.MockVersion(cgroup.V1, nil)()
	lock, err := snaplock.OpenLock("foo_instance")
	c.Assert(err, IsNil)
	defer lock.Close()

	calls := 0
	stateLocked := true
	unlocker := func() func() {
		c.Assert(stateLocked, Equals, true)
		stateLocked = false
		return func() {
			// Release the snap lock before reacquiring the state lock.
			c.Check(lock.TryLock(), IsNil)
			c.Check(lock.Unlock(), IsNil)
			stateLocked = true
		}
	}
	restore := backend.MockCgroupRemoveSnapFreezerCgroupV1(func(instanceName naming.InstanceName) error {
		calls++
		c.Check(instanceName, Equals, naming.InstanceName("foo_instance"))
		c.Check(stateLocked, Equals, false)
		c.Check(lock.TryLock(), Equals, osutil.ErrAlreadyLocked)
		return removeErr
	})
	defer restore()

	var be backend.Backend
	c.Check(be.RemoveSnapFreezerCgroup("foo_instance", unlocker), Equals, removeErr)
	c.Check(stateLocked, Equals, true)
	c.Check(calls, Equals, 1)
	c.Assert(lock.TryLock(), IsNil)
}

func (s *cgroupSuite) TestRemoveSnapFreezerCgroupV2DoesNotTakeSnapLock(c *C) {
	defer cgroup.MockVersion(cgroup.V2, nil)()
	restore := backend.MockCgroupRemoveSnapFreezerCgroupV1(func(instanceName naming.InstanceName) error {
		c.Error("unexpected cgroup-v1 freezer removal")
		return nil
	})
	defer restore()

	c.Assert(dirs.SnapRunLockDir, testutil.FileAbsent)
	var be backend.Backend
	c.Assert(be.RemoveSnapFreezerCgroup("foo_instance", nil), IsNil)
	// Taking the snap lock would create its parent directory.
	c.Check(dirs.SnapRunLockDir, testutil.FileAbsent)
}

func (s *cgroupSuite) TestRemoveSnapFreezerCgroupProbeError(c *C) {
	probeErr := errors.New("cannot determine cgroup version")
	defer cgroup.MockVersion(cgroup.Unknown, probeErr)()
	restore := backend.MockCgroupRemoveSnapFreezerCgroupV1(func(instanceName naming.InstanceName) error {
		c.Error("unexpected cgroup-v1 freezer removal")
		return nil
	})
	defer restore()

	c.Assert(dirs.SnapRunLockDir, testutil.FileAbsent)
	var be backend.Backend
	c.Check(be.RemoveSnapFreezerCgroup("foo_instance", nil), testutil.ErrorIs, probeErr)
	c.Check(dirs.SnapRunLockDir, testutil.FileAbsent)
}

func (s *cgroupSuite) TestRemoveSnapFreezerCgroupNilStateUnlocker(c *C) {
	defer cgroup.MockVersion(cgroup.V1, nil)()
	var be backend.Backend
	c.Check(be.RemoveSnapFreezerCgroup("foo_instance", nil), ErrorMatches, "internal error: stateUnlocker cannot be nil")
}
