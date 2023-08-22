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

package runinhibit_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type runInhibitSuite struct {
	lockFile string
}

var _ = Suite(&runInhibitSuite{})

func (s *runInhibitSuite) SetUpTest(c *C) {
	rootDir := c.MkDir()
	s.lockFile = filepath.Join(rootDir, "/var/lib/snapd/inhibit/pkg_v2.lock")
	dirs.SetRootDir(rootDir)
}

func (s *runInhibitSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *runInhibitSuite) TestLockFilePath(c *C) {
	lockFile := runinhibit.LockFile("pkg")
	c.Assert(lockFile, Equals, s.lockFile)
}

// Locking cannot be done with an empty hint as that is equivalent to unlocking.
func (s *runInhibitSuite) TestLockWithEmptyHint(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	err = runinhibit.LockWithHint("pkg", runinhibit.HintNotInhibited)
	c.Assert(err, ErrorMatches, "lock hint cannot be empty")

	_, err = os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)
}

// Locking a file creates required directories and writes the hint file.
func (s *runInhibitSuite) TestLockWithHint(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	err = runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh)
	c.Assert(err, IsNil)

	fi, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)

	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"refresh","revision":"unset"}`)
}

// The lock can be re-acquired to present a different hint.
func (s *runInhibitSuite) TestLockLocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh)
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"refresh","revision":"unset"}`)

	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("just-testing"))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"just-testing","revision":"unset"}`)

	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("short"))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"short","revision":"unset"}`)
}

// Unlocking an unlocked lock doesn't break anything.
func (s *runInhibitSuite) TestUnlockUnlocked(c *C) {
	err := runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileAbsent)
}

// Unlocking a locked lock multiple times shouldn't break anything.
func (s *runInhibitSuite) TestUnlockLockedTwice(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh)
	c.Assert(err, IsNil)

	err = runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"","revision":"unset"}`)

	err = runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"","revision":"unset"}`)
}

// Unlocking an locked lock truncates the hint.
func (s *runInhibitSuite) TestUnlockLocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh)
	c.Assert(err, IsNil)

	err = runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)

	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"","revision":"unset"}`)
}

// IsLocked doesn't fail when the lock directory or lock file is missing.
func (s *runInhibitSuite) TestIsLockedMissing(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	hint, err := runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)

	err = os.MkdirAll(runinhibit.InhibitDir, 0755)
	c.Assert(err, IsNil)

	hint, err = runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
}

// IsLocked returns the previously set hint.
func (s *runInhibitSuite) TestIsLockedLocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh)
	c.Assert(err, IsNil)

	hint, err := runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
}

// IsLocked returns not-inhibited after unlocking.
func (s *runInhibitSuite) TestIsLockedUnlocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh)
	c.Assert(err, IsNil)
	err = runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)

	hint, err := runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
}

func (s *runInhibitSuite) TestRemoveLockFile(c *C) {
	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh), IsNil)
	c.Check(s.lockFile, testutil.FilePresent)

	c.Assert(runinhibit.RemoveLockFile("pkg"), IsNil)
	c.Check(s.lockFile, testutil.FileAbsent)
	// Removing an absent lock file is not an error.
	c.Assert(runinhibit.RemoveLockFile("pkg"), IsNil)
}

func (s *runInhibitSuite) TestSetRevision(c *C) {
	err := runinhibit.SetRevision("pkg", snap.R(1))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"","revision":"1"}`)

	err = runinhibit.SetRevision("pkg", snap.R(2))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"","revision":"2"}`)

	err = runinhibit.SetRevision("pkg", snap.R("x3"))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"","revision":"x3"}`)
}

func (s *runInhibitSuite) TestSetRevisionWithUnsetRevision(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	err = runinhibit.SetRevision("pkg", snap.R(0))
	c.Assert(err, ErrorMatches, "snap revision cannot be unset")

	_, err = os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)
}

// SetRevision and LockWithHint do not conflict with each other.
func (s *runInhibitSuite) TestSetRevisionWithLockWithHint(c *C) {
	err := runinhibit.SetRevision("pkg", snap.R(1))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"","revision":"1"}`)

	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("refresh"))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"refresh","revision":"1"}`)

	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("just-testing"))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"just-testing","revision":"1"}`)

	err = runinhibit.SetRevision("pkg", snap.R(2))
	c.Assert(err, IsNil)
	c.Check(s.lockFile, testutil.FileEquals, `{"hint":"just-testing","revision":"2"}`)
}

func (s *runInhibitSuite) TestGetRevision(c *C) {
	rev, err := runinhibit.GetRevision("pkg")
	c.Assert(err, IsNil)
	c.Check(rev, Equals, snap.R(0))

	err = runinhibit.SetRevision("pkg", snap.R(1))
	c.Assert(err, IsNil)
	rev, err = runinhibit.GetRevision("pkg")
	c.Assert(err, IsNil)
	c.Check(rev, Equals, snap.R(1))

	err = runinhibit.SetRevision("pkg", snap.R(2))
	c.Assert(err, IsNil)
	rev, err = runinhibit.GetRevision("pkg")
	c.Assert(err, IsNil)
	c.Check(rev, Equals, snap.R(2))

	err = runinhibit.SetRevision("pkg", snap.R("x3"))
	c.Assert(err, IsNil)
	rev, err = runinhibit.GetRevision("pkg")
	c.Assert(err, IsNil)
	c.Check(rev, Equals, snap.R("x3"))
}
