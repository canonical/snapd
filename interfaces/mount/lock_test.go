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

package mount_test

import (
	"os"
	"os/exec"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/mount"
)

type lockSuite struct{}

var _ = Suite(&lockSuite{})

func (s *lockSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *lockSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

// Test that opening and closing a lock works as expected.
func (s *lockSuite) TestOpenLock(c *C) {
	lock, err := mount.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()

	_, err = os.Stat(lock.Path())
	c.Assert(err, IsNil)
}

// Test that Lock and Unlock work as expected.
func (s *lockSuite) TestLockUnlockWorks(c *C) {
	lock, err := mount.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()

	// Run a flock command in another process, it should succeed because it can
	// lock the lock as we didn't do it yet.
	cmd := exec.Command("flock", "--exclusive", "--nonblock", lock.Path(), "true")
	c.Assert(cmd.Run(), IsNil)

	// Lock the lock.
	c.Assert(lock.Lock(), IsNil)

	// Run a flock command in another process, it should fail with the distinct
	// error code because we hold the lock already and we asked it not to block.
	cmd = exec.Command("flock", "--exclusive", "--nonblock",
		"--conflict-exit-code", "2", lock.Path(), "true")
	c.Assert(cmd.Run(), ErrorMatches, "exit status 2")

	// Unlock the lock.
	c.Assert(lock.Unlock(), IsNil)

	// Run a flock command in another process, it should succeed because it can
	// grab the lock again now.
	cmd = exec.Command("flock", "--exclusive", "--nonblock", lock.Path(), "true")
	c.Assert(cmd.Run(), IsNil)
}

// Test that locking a locked lock does nothing.
func (s *lockSuite) TestLockLocked(c *C) {
	lock, err := mount.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()

	// NOTE: technically this replaces the lock type but we only use LOCK_EX.
	c.Assert(lock.Lock(), IsNil)
	c.Assert(lock.Lock(), IsNil)
}

// Test that unlocking an unlocked lock does nothing.
func (s *lockSuite) TestUnlockUnlocked(c *C) {
	lock, err := mount.OpenLock("name")
	c.Assert(err, IsNil)
	defer lock.Close()

	c.Assert(lock.Unlock(), IsNil)
}

// Test that locking or unlocking a closed lock fails.
func (s *lockSuite) TestUsingClosedLock(c *C) {
	lock, err := mount.OpenLock("name")
	c.Assert(err, IsNil)
	lock.Close()

	c.Assert(lock.Lock(), ErrorMatches, "bad file descriptor")
	c.Assert(lock.Unlock(), ErrorMatches, "bad file descriptor")
}
