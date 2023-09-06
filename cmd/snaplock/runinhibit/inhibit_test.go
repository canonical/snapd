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
	"encoding/json"
	"fmt"
	"io/ioutil"
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
	inhibitInfo runinhibit.InhibitInfo
}

var _ = Suite(&runInhibitSuite{})

func (s *runInhibitSuite) SetUpTest(c *C) {
	s.inhibitInfo = runinhibit.InhibitInfo{Revision: snap.R(11)}
	dirs.SetRootDir(c.MkDir())
}

func (s *runInhibitSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

// Locking cannot be done with an empty hint as that is equivalent to unlocking.
func (s *runInhibitSuite) TestLockWithEmptyHint(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	err = runinhibit.LockWithHint("pkg", runinhibit.HintNotInhibited, s.inhibitInfo)
	c.Assert(err, ErrorMatches, "lock hint cannot be empty")

	_, err = os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)
}

// Locking cannot be done with an unset revision.
func (s *runInhibitSuite) TestLockWithUnsetRevision(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	err = runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, runinhibit.InhibitInfo{Revision: snap.R(0)})
	c.Assert(err, ErrorMatches, "snap revision cannot be unset")

	_, err = os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)
}

// testInhibitInfo checks inhibit info file <snap>.<hint> content.
func testInhibitInfo(c *C, snapName, hint string, expectedInfo runinhibit.InhibitInfo) {
	infoPath := filepath.Join(runinhibit.InhibitDir, fmt.Sprintf("%s.%s", snapName, hint))
	var info runinhibit.InhibitInfo
	buf, err := ioutil.ReadFile(infoPath)
	c.Assert(err, IsNil)
	c.Assert(json.Unmarshal(buf, &info), IsNil)
	c.Check(info, Equals, expectedInfo)
}

// Locking a file creates required directories and writes the hint and inhibit info files.
func (s *runInhibitSuite) TestLockWithHint(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	expectedInfo := runinhibit.InhibitInfo{Revision: snap.R(42)}
	err = runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, expectedInfo)
	c.Assert(err, IsNil)

	fi, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(err, IsNil)
	c.Check(fi.IsDir(), Equals, true)

	// Check hint file <snap>.lock
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "refresh")
	// Check inhibit info file <snap>.<hint>
	testInhibitInfo(c, "pkg", "refresh", expectedInfo)
}

// The lock can be re-acquired to present a different hint.
func (s *runInhibitSuite) TestLockLocked(c *C) {
	expectedInfo := runinhibit.InhibitInfo{Revision: snap.R(42)}
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, expectedInfo)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "refresh")
	testInhibitInfo(c, "pkg", "refresh", expectedInfo)

	expectedInfo = runinhibit.InhibitInfo{Revision: snap.R(43)}
	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("just-testing"), expectedInfo)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "just-testing")
	testInhibitInfo(c, "pkg", "just-testing", expectedInfo)

	expectedInfo = runinhibit.InhibitInfo{Revision: snap.R(44)}
	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("short"), expectedInfo)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "short")
	testInhibitInfo(c, "pkg", "short", expectedInfo)
}

// Unlocking an unlocked lock doesn't break anything.
func (s *runInhibitSuite) TestUnlockUnlocked(c *C) {
	err := runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileAbsent)
}

// Unlocking an locked lock truncates the hint and removes inhibit info file.
func (s *runInhibitSuite) TestUnlockLocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo)
	c.Assert(err, IsNil)

	err = runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)

	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "")
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.refresh"), testutil.FileAbsent)
}

// IsLocked doesn't fail when the lock directory or lock file is missing.
func (s *runInhibitSuite) TestIsLockedMissing(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	hint, info, err := runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})

	err = os.MkdirAll(runinhibit.InhibitDir, 0755)
	c.Assert(err, IsNil)

	hint, info, err = runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

// IsLocked returns the previously set hint/info.
func (s *runInhibitSuite) TestIsLockedLocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo)
	c.Assert(err, IsNil)

	hint, info, err := runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
	c.Check(info, Equals, s.inhibitInfo)
}

// IsLocked returns not-inhibited after unlocking.
func (s *runInhibitSuite) TestIsLockedUnlocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo)
	c.Assert(err, IsNil)
	err = runinhibit.Unlock("pkg")
	c.Assert(err, IsNil)

	hint, info, err := runinhibit.IsLocked("pkg")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

func (s *runInhibitSuite) TestRemoveLockFile(c *C) {
	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo), IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FilePresent)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.refresh"), testutil.FilePresent)

	c.Assert(runinhibit.RemoveLockFile("pkg"), IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileAbsent)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.refresh"), testutil.FileAbsent)
	// Removing an absent lock file is not an error.
	c.Assert(runinhibit.RemoveLockFile("pkg"), IsNil)
}

func (s *runInhibitSuite) TestLockWithHintFilePostfix(c *C) {
	// This would confuse inhibit info to overwrite the hint file
	err := runinhibit.LockWithHint("pkg", "lock", s.inhibitInfo)
	c.Assert(err, ErrorMatches, `hint cannot have value "lock"`)
}
