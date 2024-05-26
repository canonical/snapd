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

package osutil_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type flockSuite struct{}

var _ = Suite(&flockSuite{})

// Test that an existing lock file can be opened.
func (s *flockSuite) TestOpenExistingLockForReading(c *C) {
	fname := filepath.Join(c.MkDir(), "name")
	lock := mylog.Check2(osutil.OpenExistingLockForReading(fname))
	c.Assert(err, ErrorMatches, ".* no such file or directory")
	c.Assert(lock, IsNil)

	lock = mylog.Check2(osutil.NewFileLockWithMode(fname, 0644))

	lock.Close()

	// Having created the lock above, we can now open it correctly.
	lock = mylog.Check2(osutil.OpenExistingLockForReading(fname))

	defer lock.Close()

	// The lock file is read-only though.
	file := lock.File()
	defer file.Close()
	n := mylog.Check2(file.Write([]byte{1, 2, 3}))
	// write(2) returns EBADF if the file descriptor is read only.
	c.Assert(err, ErrorMatches, ".* bad file descriptor")
	c.Assert(n, Equals, 0)
}

// Test that opening and closing a lock works as expected, and that the mode is right.
func (s *flockSuite) TestNewFileLockWithMode(c *C) {
	lock := mylog.Check2(osutil.NewFileLockWithMode(filepath.Join(c.MkDir(), "name"), 0644))

	defer lock.Close()

	fi := mylog.Check2(os.Stat(lock.Path()))

	c.Assert(fi.Mode().Perm(), Equals, os.FileMode(0644))
}

// Test that opening and closing a lock works as expected.
func (s *flockSuite) TestNewFileLock(c *C) {
	lock := mylog.Check2(osutil.NewFileLock(filepath.Join(c.MkDir(), "name")))

	defer lock.Close()

	fi := mylog.Check2(os.Stat(lock.Path()))

	c.Assert(fi.Mode().Perm(), Equals, os.FileMode(0600))
}

// Test that we can access the underlying open file.
func (s *flockSuite) TestFile(c *C) {
	fname := filepath.Join(c.MkDir(), "name")
	lock := mylog.Check2(osutil.NewFileLock(fname))

	defer lock.Close()

	f := lock.File()
	c.Assert(f, NotNil)
	c.Check(f.Name(), Equals, fname)
}

func flockSupportsConflictExitCodeSwitch(c *C) bool {
	output := mylog.Check2(exec.Command("flock", "--help").CombinedOutput())

	return bytes.Contains(output, []byte("--conflict-exit-code"))
}

// Test that Lock and Unlock work as expected.
func (s *flockSuite) TestLockUnlockWorks(c *C) {
	if !flockSupportsConflictExitCodeSwitch(c) {
		c.Skip("flock too old for this test")
	}

	lock := mylog.Check2(osutil.NewFileLock(filepath.Join(c.MkDir(), "name")))

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

// Test that ReadLock and Unlock work as expected.
func (s *flockSuite) TestReadLockUnlockWorks(c *C) {
	if !flockSupportsConflictExitCodeSwitch(c) {
		c.Skip("flock too old for this test")
	}

	lock := mylog.Check2(osutil.NewFileLock(filepath.Join(c.MkDir(), "name")))

	defer lock.Close()

	// Run a flock command in another process, it should succeed because it can
	// lock the lock as we didn't do it yet.
	cmd := exec.Command("flock", "--exclusive", "--nonblock", lock.Path(), "true")
	c.Assert(cmd.Run(), IsNil)

	// Grab a shared lock.
	c.Assert(lock.ReadLock(), IsNil)

	// Run a flock command in another process, it should fail with the distinct
	// error code because we hold a shared lock already and we asked it not to block.
	cmd = exec.Command("flock", "--exclusive", "--nonblock",
		"--conflict-exit-code", "2", lock.Path(), "true")
	c.Assert(cmd.Run(), ErrorMatches, "exit status 2")

	// Run a flock command in another process, it should succeed because we
	// hold a shared lock and those do not prevent others from acquiring a
	// shared lock.
	cmd = exec.Command("flock", "--shared", "--nonblock",
		"--conflict-exit-code", "2", lock.Path(), "true")
	c.Assert(cmd.Run(), IsNil)

	// Unlock the lock.
	c.Assert(lock.Unlock(), IsNil)

	// Run a flock command in another process, it should succeed because it can
	// grab the lock again now.
	cmd = exec.Command("flock", "--exclusive", "--nonblock", lock.Path(), "true")
	c.Assert(cmd.Run(), IsNil)
}

// Test that locking a locked lock does nothing.
func (s *flockSuite) TestLockLocked(c *C) {
	lock := mylog.Check2(osutil.NewFileLock(filepath.Join(c.MkDir(), "name")))

	defer lock.Close()

	// NOTE: technically this replaces the lock type but we only use LOCK_EX.
	c.Assert(lock.Lock(), IsNil)
	c.Assert(lock.Lock(), IsNil)
}

// Test that unlocking an unlocked lock does nothing.
func (s *flockSuite) TestUnlockUnlocked(c *C) {
	lock := mylog.Check2(osutil.NewFileLock(filepath.Join(c.MkDir(), "name")))

	defer lock.Close()

	c.Assert(lock.Unlock(), IsNil)
}

// Test that locking or unlocking a closed lock fails.
func (s *flockSuite) TestUsingClosedLock(c *C) {
	lock := mylog.Check2(osutil.NewFileLock(filepath.Join(c.MkDir(), "name")))

	lock.Close()

	c.Assert(lock.Lock(), ErrorMatches, "bad file descriptor")
	c.Assert(lock.Unlock(), ErrorMatches, "bad file descriptor")
}

// Test that non-blocking locking reports error on pre-acquired lock.
func (s *flockSuite) TestLockUnlockNonblockingWorks(c *C) {
	// Use the "flock" command to grab a lock for 9999 seconds in another process.
	lockPath := filepath.Join(c.MkDir(), "lock")
	sleeperKillerPath := filepath.Join(c.MkDir(), "pid")
	// we can't use --no-fork because we still support 14.04
	cmd := exec.Command("flock", "--exclusive", lockPath, "-c", fmt.Sprintf(`echo "kill $$" > %s && exec sleep 30`, sleeperKillerPath))

	// flock uses the env variable 'SHELL' to run the passed in command. a non-posix
	// shell will not understand $$. we can force flock to use its default by unsetting
	// the variable
	cmd.Env = append(cmd.Env, "SHELL=")

	c.Assert(cmd.Start(), IsNil)
	defer func() { exec.Command("/bin/sh", sleeperKillerPath).Run() }()

	// Give flock some chance to create the lock file.
	for i := 0; i < 10; i++ {
		if osutil.FileExists(lockPath) {
			break
		}
		time.Sleep(time.Millisecond * 300)
	}

	// Try to acquire the same lock file and see that it is busy.
	lock := mylog.Check2(osutil.NewFileLock(lockPath))

	c.Assert(lock, NotNil)
	defer lock.Close()

	c.Assert(lock.TryLock(), Equals, osutil.ErrAlreadyLocked)
}
