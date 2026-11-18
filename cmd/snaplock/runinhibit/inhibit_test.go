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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type runInhibitSuite struct {
	inhibitInfo runinhibit.InhibitInfo
}

var _ = Suite(&runInhibitSuite{})

func (s *runInhibitSuite) SetUpTest(c *C) {
	s.inhibitInfo = runinhibit.InhibitInfo{Previous: snap.R(11)}
	dirs.SetRootDir(c.MkDir())
}

func (s *runInhibitSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

// Locking cannot be done with an empty hint as that is equivalent to unlocking.
func (s *runInhibitSuite) TestLockWithEmptyHint(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	err = runinhibit.LockWithHint("pkg", runinhibit.HintNotInhibited, s.inhibitInfo, nil)
	c.Assert(err, ErrorMatches, "lock hint cannot be empty")

	_, err = os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)
}

// Locking cannot be done with an unset revision.
func (s *runInhibitSuite) TestLockWithUnsetRevision(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	err = runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, runinhibit.InhibitInfo{Previous: snap.R(0)}, nil)
	c.Assert(err, ErrorMatches, "snap revision cannot be unset")

	_, err = os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)
}

// testInhibitInfo checks inhibit info file <snap>.<hint> content.
func testInhibitInfo(c *C, snapName, hint string, expectedInfo runinhibit.InhibitInfo) {
	infoPath := filepath.Join(runinhibit.InhibitDir, fmt.Sprintf("%s.%s", snapName, hint))
	var info runinhibit.InhibitInfo
	buf, err := os.ReadFile(infoPath)
	c.Assert(err, IsNil)
	c.Assert(json.Unmarshal(buf, &info), IsNil)
	c.Check(info, Equals, expectedInfo)
}

// Locking a file creates required directories and writes the hint and inhibit info files.
func (s *runInhibitSuite) TestLockWithHint(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	var unlockerCalled, relockCalled int
	fakeUnlocker := func() (relock func()) {
		unlockerCalled++
		return func() { relockCalled++ }
	}
	expectedInfo := runinhibit.InhibitInfo{Previous: snap.R(42)}
	err = runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, expectedInfo, fakeUnlocker)
	c.Assert(err, IsNil)
	c.Check(unlockerCalled, Equals, 1)
	c.Check(relockCalled, Equals, 1)

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
	expectedInfo := runinhibit.InhibitInfo{Previous: snap.R(42)}
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, expectedInfo, nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "refresh")
	testInhibitInfo(c, "pkg", "refresh", expectedInfo)

	expectedInfo = runinhibit.InhibitInfo{Previous: snap.R(43)}
	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("just-testing"), expectedInfo, nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "just-testing")
	testInhibitInfo(c, "pkg", "just-testing", expectedInfo)

	expectedInfo = runinhibit.InhibitInfo{Previous: snap.R(44)}
	err = runinhibit.LockWithHint("pkg", runinhibit.Hint("short"), expectedInfo, nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "short")
	testInhibitInfo(c, "pkg", "short", expectedInfo)
}

// Unlocking an unlocked lock doesn't break anything.
func (s *runInhibitSuite) TestUnlockUnlocked(c *C) {
	err := runinhibit.Unlock("pkg", nil)
	c.Assert(err, IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileAbsent)
}

// Unlocking an locked lock truncates the hint and removes inhibit info file.
func (s *runInhibitSuite) TestUnlockLocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)

	var unlockerCalled, relockCalled int
	fakeUnlocker := func() (relock func()) {
		unlockerCalled++
		return func() { relockCalled++ }
	}
	err = runinhibit.Unlock("pkg", fakeUnlocker)
	c.Assert(err, IsNil)
	c.Check(unlockerCalled, Equals, 1)
	c.Check(relockCalled, Equals, 1)

	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileEquals, "")
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.refresh"), testutil.FileAbsent)
}

func (s *runInhibitSuite) TestUnlockStaleGateRefreshLocks(c *C) {
	// pkg1 has a gate-refresh lock
	err := runinhibit.LockWithHint("pkg1", runinhibit.HintInhibitedGateRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)
	// pkg2 has a refresh lock
	err = runinhibit.LockWithHint("pkg2", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)
	// pkg3 changed from a gate-refresh lock to a refresh lock, and still has
	// the gate-refresh inhibit info file
	err = runinhibit.LockWithHint("pkg3", runinhibit.HintInhibitedGateRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)
	err = runinhibit.LockWithHint("pkg3", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)
	c.Assert(filepath.Join(runinhibit.InhibitDir, "pkg3.gate-refresh"), testutil.FilePresent)

	var unlockerCalled, relockCalled int
	fakeUnlocker := func() (relock func()) {
		unlockerCalled++
		return func() { relockCalled++ }
	}
	unlocked, err := runinhibit.UnlockStaleGateRefreshLocks(fakeUnlocker)
	c.Assert(err, IsNil)
	c.Check(unlocked, DeepEquals, []string{"pkg1"})
	c.Check(unlockerCalled, Equals, 1)
	c.Check(relockCalled, Equals, 1)

	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg1.lock"), testutil.FileEquals, "")
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg1.gate-refresh"), testutil.FileAbsent)

	for _, name := range []naming.InstanceName{"pkg2", "pkg3"} {
		hint, info, err := runinhibit.IsLocked(name, nil)
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
		c.Check(info, Equals, s.inhibitInfo)
	}
}

func (s *runInhibitSuite) TestUnlockStaleGateRefreshLocksNoInhibitDir(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	unlocked, err := runinhibit.UnlockStaleGateRefreshLocks(nil)
	c.Assert(err, IsNil)
	c.Check(unlocked, HasLen, 0)
}

func (s *runInhibitSuite) TestUnlockStaleGateRefreshLocksError(c *C) {
	err := runinhibit.LockWithHint("pkg1", runinhibit.HintInhibitedGateRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)
	err = runinhibit.LockWithHint("pkg2", runinhibit.HintInhibitedGateRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)

	// replace the hint file of pkg1 with a directory, so that it cannot be
	// opened for writing
	hintFile := filepath.Join(runinhibit.InhibitDir, "pkg1.lock")
	c.Assert(os.Remove(hintFile), IsNil)
	c.Assert(os.Mkdir(hintFile, 0755), IsNil)

	unlocked, err := runinhibit.UnlockStaleGateRefreshLocks(nil)
	c.Check(err, ErrorMatches, `cannot release gate-refresh inhibition lock of snap "pkg1": .*`)
	c.Check(unlocked, DeepEquals, []string{"pkg2"})

	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg2.lock"), testutil.FileEquals, "")
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg2.gate-refresh"), testutil.FileAbsent)
}

// IsLocked doesn't fail when the lock directory or lock file is missing.
func (s *runInhibitSuite) TestIsLockedMissing(c *C) {
	_, err := os.Stat(runinhibit.InhibitDir)
	c.Assert(os.IsNotExist(err), Equals, true)

	var unlockerCalled, relockCalled int
	fakeUnlocker := func() (relock func()) {
		unlockerCalled++
		return func() { relockCalled++ }
	}

	hint, info, err := runinhibit.IsLocked("pkg", fakeUnlocker)
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
	c.Check(unlockerCalled, Equals, 1)
	c.Check(relockCalled, Equals, 1)

	err = os.MkdirAll(runinhibit.InhibitDir, 0755)
	c.Assert(err, IsNil)

	hint, info, err = runinhibit.IsLocked("pkg", fakeUnlocker)
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
	c.Check(unlockerCalled, Equals, 2)
	c.Check(relockCalled, Equals, 2)
}

// IsLocked returns the previously set hint/info.
func (s *runInhibitSuite) TestIsLockedLocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)

	hint, info, err := runinhibit.IsLocked("pkg", nil)
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
	c.Check(info, Equals, s.inhibitInfo)
}

// IsLocked returns not-inhibited after unlocking.
func (s *runInhibitSuite) TestIsLockedUnlocked(c *C) {
	err := runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil)
	c.Assert(err, IsNil)
	err = runinhibit.Unlock("pkg", nil)
	c.Assert(err, IsNil)

	hint, info, err := runinhibit.IsLocked("pkg", nil)
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

func (s *runInhibitSuite) TestRemoveLockFile(c *C) {
	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil), IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FilePresent)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.refresh"), testutil.FilePresent)

	var unlockerCalled, relockCalled int
	fakeUnlocker := func() (relock func()) {
		unlockerCalled++
		return func() { relockCalled++ }
	}

	c.Assert(runinhibit.RemoveLockFile("pkg", fakeUnlocker), IsNil)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.lock"), testutil.FileAbsent)
	c.Check(filepath.Join(runinhibit.InhibitDir, "pkg.refresh"), testutil.FileAbsent)
	c.Check(unlockerCalled, Equals, 1)
	c.Check(relockCalled, Equals, 1)
	// Removing an absent lock file is not an error.
	c.Assert(runinhibit.RemoveLockFile("pkg", fakeUnlocker), IsNil)
	c.Check(unlockerCalled, Equals, 2)
	c.Check(relockCalled, Equals, 2)
}

func checkFileLocked(c *C, path string) {
	flock, err := osutil.NewFileLock(path)
	c.Assert(err, IsNil)
	c.Check(flock.TryLock(), Equals, osutil.ErrAlreadyLocked)
	flock.Close()
}

func checkFileNotLocked(c *C, path string) {
	flock, err := osutil.NewFileLock(path)
	c.Assert(err, IsNil)
	c.Check(flock.TryLock(), IsNil)
	flock.Close()
}

func (s *runInhibitSuite) TestWaitWhileInhibitedWalkthrough(c *C) {
	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil), IsNil)

	notInhibitedCalled := 0
	inhibitedCalled := 0
	// closed channel returns immediately
	waitCh := make(chan time.Time)
	close(waitCh)
	tickerWait := func() <-chan time.Time {
		// lock should be released during wait interval
		checkFileNotLocked(c, runinhibit.HintFile("pkg"))

		if inhibitedCalled == 3 {
			// let's remove run inhibtion
			c.Assert(runinhibit.Unlock("pkg", nil), IsNil)
		}

		return waitCh
	}
	defer runinhibit.MockNewTicker(tickerWait)()

	notInhibited := func(ctx context.Context) error {
		notInhibitedCalled++

		hint, _, err := runinhibit.IsLocked("pkg", nil)
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintNotInhibited)

		// lock should be held
		checkFileLocked(c, runinhibit.HintFile("pkg"))

		return nil
	}
	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		inhibitedCalled++

		// notInhibited() should not be called before inhibited
		c.Check(notInhibitedCalled, Equals, 0)

		c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
		c.Check(*inhibitInfo, Equals, s.inhibitInfo)

		// lock should be held
		checkFileLocked(c, runinhibit.HintFile("pkg"))

		return false, nil
	}

	flock, err := runinhibit.WaitWhileInhibited(context.TODO(), "pkg", notInhibited, inhibited, 1*time.Millisecond)
	c.Assert(err, IsNil)

	// we are still holding the lock
	checkFileLocked(c, runinhibit.HintFile("pkg"))

	c.Check(notInhibitedCalled, Equals, 1)
	c.Check(inhibitedCalled, Equals, 3)

	flock.Close()
}

func (s *runInhibitSuite) TestWaitWhileInhibitedContextCancellation(c *C) {
	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil), IsNil)

	ctx, cancel := context.WithCancel(context.Background())

	inhibitedCalled := 0
	tickerWait := func() <-chan time.Time {
		// lock should be released during wait interval
		checkFileNotLocked(c, runinhibit.HintFile("pkg"))

		if inhibitedCalled == 2 {
			// let's cancel the context
			cancel()
		}

		// give precedence to ctx.Done() channel
		return time.After(200 * time.Millisecond)
	}
	defer runinhibit.MockNewTicker(tickerWait)()

	notInhibited := func(ctx context.Context) error {
		return fmt.Errorf("this should never be reached")
	}
	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		inhibitedCalled++

		c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
		c.Check(*inhibitInfo, Equals, s.inhibitInfo)

		// lock should be held
		checkFileLocked(c, runinhibit.HintFile("pkg"))

		return false, nil
	}

	flock, err := runinhibit.WaitWhileInhibited(ctx, "pkg", notInhibited, inhibited, 1*time.Millisecond)
	c.Check(errors.Is(err, context.Canceled), Equals, true)
	c.Check(flock, IsNil)

	// lock must be released on error
	checkFileNotLocked(c, runinhibit.HintFile("pkg"))

	c.Check(inhibitedCalled, Equals, 2)
}

func (s *runInhibitSuite) TestWaitWhileInhibitedNilCallbacks(c *C) {
	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil), IsNil)

	waitCalled := 0
	// closed channel returns immediately
	waitCh := make(chan time.Time)
	close(waitCh)
	tickerWait := func() <-chan time.Time {
		waitCalled++
		// lock should be released during wait interval
		checkFileNotLocked(c, runinhibit.HintFile("pkg"))
		// let's remove run inhibtion
		c.Assert(runinhibit.Unlock("pkg", nil), IsNil)

		return waitCh
	}
	defer runinhibit.MockNewTicker(tickerWait)()

	flock, err := runinhibit.WaitWhileInhibited(context.TODO(), "pkg", nil, nil, 1*time.Millisecond)
	// nil callbacks are skipped and must not panic
	c.Assert(err, IsNil)

	// we are still holding the lock
	checkFileLocked(c, runinhibit.HintFile("pkg"))

	c.Check(waitCalled, Equals, 1)

	flock.Close()
}

func (s *runInhibitSuite) TestWaitWhileInhibitedCallbackError(c *C) {
	c.Assert(os.MkdirAll(runinhibit.InhibitDir, 0755), IsNil)
	c.Assert(os.WriteFile(runinhibit.HintFile("pkg"), nil, 0644), IsNil)

	notInhibited := func(ctx context.Context) error {
		return fmt.Errorf("notInhibited error")
	}
	flock, err := runinhibit.WaitWhileInhibited(context.TODO(), "pkg", notInhibited, nil, 1*time.Millisecond)
	c.Assert(err, ErrorMatches, "notInhibited error")
	c.Check(flock, IsNil)
	// lock must be released on error
	checkFileNotLocked(c, runinhibit.HintFile("pkg"))

	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil), IsNil)
	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		return false, fmt.Errorf("inhibited error")
	}
	flock, err = runinhibit.WaitWhileInhibited(context.TODO(), "pkg", nil, inhibited, 1*time.Millisecond)
	c.Assert(err, ErrorMatches, "inhibited error")
	c.Check(flock, IsNil)
	// lock must be released on error
	checkFileNotLocked(c, runinhibit.HintFile("pkg"))
}

func (s *runInhibitSuite) TestWaitWhileInhibitedCont(c *C) {
	c.Assert(runinhibit.LockWithHint("pkg", runinhibit.HintInhibitedForRefresh, s.inhibitInfo, nil), IsNil)

	notInhibitedCalled := 0
	inhibitedCalled := 0

	notInhibited := func(ctx context.Context) error {
		notInhibitedCalled++
		return nil
	}
	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		inhibitedCalled++

		// lock should be held
		checkFileLocked(c, runinhibit.HintFile("pkg"))

		// continue
		return true, nil
	}

	flock, err := runinhibit.WaitWhileInhibited(context.TODO(), "pkg", notInhibited, inhibited, 1*time.Millisecond)
	c.Assert(err, IsNil)

	// we are still holding the lock
	checkFileLocked(c, runinhibit.HintFile("pkg"))

	c.Check(notInhibitedCalled, Equals, 0)
	c.Check(inhibitedCalled, Equals, 1)

	hint, inhibitInfo, err := runinhibit.IsLocked("pkg", nil)
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
	c.Check(inhibitInfo, Equals, s.inhibitInfo)

	flock.Close()
}

func (s *runInhibitSuite) TestWaitWhileInhibitedHintFileNotExist(c *C) {
	c.Assert(os.RemoveAll(runinhibit.HintFile("pkg")), IsNil)

	notInhibitedCalled := 0
	inhibitedCalled := 0

	notInhibited := func(ctx context.Context) error {
		notInhibitedCalled++
		c.Check(runinhibit.HintFile("pkg"), testutil.FileAbsent)
		return nil
	}
	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		inhibitedCalled++
		c.Error("this should never be called")
		return false, nil
	}

	flock, err := runinhibit.WaitWhileInhibited(context.TODO(), "pkg", notInhibited, inhibited, 1*time.Millisecond)
	c.Assert(err, IsNil)
	// hint file still does not exist
	c.Check(runinhibit.HintFile("pkg"), testutil.FileAbsent)
	// flock is nil because lock file does not exist
	c.Check(flock, IsNil)

	c.Check(notInhibitedCalled, Equals, 1)
	c.Check(inhibitedCalled, Equals, 0)
}

func (s *runInhibitSuite) TestWaitWhileInhibitedHintFileNotExistNilCallback(c *C) {
	c.Assert(os.RemoveAll(runinhibit.HintFile("pkg")), IsNil)

	inhibited := func(ctx context.Context, hint runinhibit.Hint, inhibitInfo *runinhibit.InhibitInfo) (cont bool, err error) {
		c.Error("this should never be called")
		return false, nil
	}

	// check that we don't panic when notInhibited is nil
	flock, err := runinhibit.WaitWhileInhibited(context.TODO(), "pkg", nil, inhibited, 1*time.Millisecond)
	c.Assert(err, IsNil)
	// hint file still does not exist
	c.Check(runinhibit.HintFile("pkg"), testutil.FileAbsent)
	// flock is nil because lock file does not exist
	c.Check(flock, IsNil)
}
