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

package main_test

import (
	"bytes"
	"fmt"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
)

type updateSuite struct {
	testutil.BaseTest
	log *bytes.Buffer
}

var _ = Suite(&updateSuite{})

func (s *updateSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
}

func (s *updateSuite) TestSmoke(c *C) {
	upCtx := &testProfileUpdateContext{}
	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
}

func (s *updateSuite) TestUpdateFlow(c *C) {
	// The flow of update is as follows:
	// - the current profile and the desired profiles are loaded
	// - the needed changes are computed
	// - the needed changes are performed (one by one)
	// - the updated current profile is saved
	var funcsCalled []string
	var nChanges int
	upCtx := &testProfileUpdateContext{
		loadCurrentProfile: func() (*osutil.MountProfile, error) {
			funcsCalled = append(funcsCalled, "loaded-current")
			return &osutil.MountProfile{}, nil
		},
		loadDesiredProfile: func() (*osutil.MountProfile, error) {
			funcsCalled = append(funcsCalled, "loaded-desired")
			return &osutil.MountProfile{}, nil
		},
		neededChanges: func(old, new *osutil.MountProfile) []*update.Change {
			funcsCalled = append(funcsCalled, "changes-computed")
			return []*update.Change{{}, {}}
		},
		performChange: func(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
			nChanges++
			funcsCalled = append(funcsCalled, fmt.Sprintf("change-%d-performed", nChanges))
			return nil, nil
		},
		saveCurrentProfile: func(*osutil.MountProfile) error {
			funcsCalled = append(funcsCalled, "saved-current")
			return nil
		},
	}
	restore := upCtx.MockRelatedFunctions()
	defer restore()
	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
	c.Assert(funcsCalled, DeepEquals, []string{"loaded-desired", "loaded-current",
		"changes-computed", "change-1-performed", "change-2-performed", "saved-current"})
	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
}

func (s *updateSuite) TestResultingProfile(c *C) {
	// When the mount namespace is changed by performing actions the updated
	// current profile is comprised of the past changes that were reused (kept
	// unchanged) as well as newly mounted entries. Unmounted entries simple
	// cease to be.
	var saved *osutil.MountProfile
	upCtx := &testProfileUpdateContext{
		neededChanges: func(old, new *osutil.MountProfile) []*update.Change {
			return []*update.Change{
				{Action: update.Keep, Entry: osutil.MountEntry{Dir: "/keep"}},
				{Action: update.Unmount, Entry: osutil.MountEntry{Dir: "/unmount"}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/mount"}},
			}
		},
		saveCurrentProfile: func(profile *osutil.MountProfile) error {
			saved = profile
			return nil
		},
	}
	restore := upCtx.MockRelatedFunctions()
	defer restore()
	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
	c.Check(saved, DeepEquals, &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/keep"},
		{Dir: "/mount"},
	}})
}

func (s *updateSuite) TestSynthesizedPastChanges(c *C) {
	// When an mount update is performed it runs under the assumption
	// that past changes (i.e. the current profile) did occur. This is used
	// by the trespassing detector.
	text := `tmpfs /usr tmpfs 0 0`
	entry, err := osutil.ParseMountEntry(text)
	c.Assert(err, IsNil)
	as := &update.Assumptions{}
	upCtx := &testProfileUpdateContext{
		loadCurrentProfile: func() (*osutil.MountProfile, error) { return osutil.LoadMountProfileText(text) },
		loadDesiredProfile: func() (*osutil.MountProfile, error) { return osutil.LoadMountProfileText(text) },
		assumptions:        func() *update.Assumptions { return as },
	}
	restore := upCtx.MockRelatedFunctions()
	defer restore()

	// Perform the update, this will modify assumptions.
	c.Check(as.PastChanges(), HasLen, 0)
	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
	c.Check(as.PastChanges(), HasLen, 1)
	c.Check(as.PastChanges(), DeepEquals, []*update.Change{{
		Action: update.Mount,
		Entry:  entry,
	}})
}

func (s *updateSuite) TestSyntheticChanges(c *C) {
	// When a mount change is performed it may cause additional mount changes
	// to be performed, that were needed internally. Such changes are recorded
	// and saved into the current profile.
	var saved *osutil.MountProfile
	upCtx := &testProfileUpdateContext{
		loadDesiredProfile: func() (*osutil.MountProfile, error) {
			return &osutil.MountProfile{Entries: []osutil.MountEntry{
				{Dir: "/subdir/mount"},
			}}, nil
		},
		saveCurrentProfile: func(profile *osutil.MountProfile) error {
			saved = profile
			return nil
		},
		neededChanges: func(old, new *osutil.MountProfile) []*update.Change {
			return []*update.Change{
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/subdir/mount"}},
			}
		},
		performChange: func(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
			// If we are trying to mount /subdir/mount then synthesize a change
			// for making a tmpfs on /subdir.
			if change.Action == update.Mount && change.Entry.Dir == "/subdir/mount" {
				return []*update.Change{
					{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/subdir", Type: "tmpfs"}},
				}, nil
			}
			return nil, nil
		},
	}
	restore := upCtx.MockRelatedFunctions()
	defer restore()
	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
	c.Check(saved, DeepEquals, &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/subdir", Type: "tmpfs"},
		{Dir: "/subdir/mount"},
	}})
}

func (s *updateSuite) TestCannotPerformContentInterfaceChange(c *C) {
	// When performing a mount change for a content interface fails, we simply
	// ignore the error carry on. Such changes are not stored in the updated
	// current profile.
	var saved *osutil.MountProfile
	upCtx := &testProfileUpdateContext{
		saveCurrentProfile: func(profile *osutil.MountProfile) error {
			saved = profile
			return nil
		},
		neededChanges: func(old, new *osutil.MountProfile) []*update.Change {
			return []*update.Change{
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-1"}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-2"}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-3"}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-4"}},
			}
		},
		performChange: func(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
			// The change to /dir-2 cannot be made.
			if change.Action == update.Mount && change.Entry.Dir == "/dir-2" {
				return nil, errTesting
			}
			// The change to /dir-4 cannot be made either but with a special reason.
			if change.Action == update.Mount && change.Entry.Dir == "/dir-4" {
				return nil, update.ErrIgnoredMissingMount
			}
			return nil, nil
		},
	}
	restore := upCtx.MockRelatedFunctions()
	defer restore()
	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
	c.Check(saved, DeepEquals, &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/dir-1"},
		{Dir: "/dir-3"},
	}})
	// A message is logged though, unless specifically silenced with a crafted error.
	c.Check(s.log.String(), testutil.Contains, "cannot change mount namespace according to change mount (none /dir-2 none defaults 0 0): testing")
	c.Check(s.log.String(), Not(testutil.Contains), "cannot change mount namespace according to change mount (none /dir-4 none defaults 0 0): ")
}

func (s *updateSuite) TestCannotPerformLayoutChange(c *C) {
	// When performing a mount change for a layout, errors are immediately fatal.
	var saved *osutil.MountProfile
	upCtx := &testProfileUpdateContext{
		saveCurrentProfile: func(profile *osutil.MountProfile) error {
			saved = profile
			return nil
		},
		neededChanges: func(old, new *osutil.MountProfile) []*update.Change {
			return []*update.Change{
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-1"}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-2", Options: []string{"x-snapd.origin=layout"}}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-3"}},
			}
		},
		performChange: func(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
			// The change to /dir-2 cannot be made.
			if change.Action == update.Mount && change.Entry.Dir == "/dir-2" {
				return nil, errTesting
			}
			return nil, nil
		},
	}
	restore := upCtx.MockRelatedFunctions()
	defer restore()
	err := update.ExecuteMountProfileUpdate(upCtx)
	c.Check(err, Equals, errTesting)
	c.Check(saved, IsNil)
}

func (s *updateSuite) TestCannotPerformOvermountChange(c *C) {
	// When performing a mount change for an "overname", errors are immediately fatal.
	var saved *osutil.MountProfile
	upCtx := &testProfileUpdateContext{
		saveCurrentProfile: func(profile *osutil.MountProfile) error {
			saved = profile
			return nil
		},
		neededChanges: func(old, new *osutil.MountProfile) []*update.Change {
			return []*update.Change{
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-1"}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-2", Options: []string{"x-snapd.origin=overname"}}},
				{Action: update.Mount, Entry: osutil.MountEntry{Dir: "/dir-3"}},
			}
		},
		performChange: func(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
			// The change to /dir-2 cannot be made.
			if change.Action == update.Mount && change.Entry.Dir == "/dir-2" {
				return nil, errTesting
			}
			return nil, nil
		},
	}
	restore := upCtx.MockRelatedFunctions()
	defer restore()
	err := update.ExecuteMountProfileUpdate(upCtx)
	c.Check(err, Equals, errTesting)
	c.Check(saved, IsNil)
}

func (s *updateSuite) TestKeepSyntheticMountsLP2043993(c *C) {
	baseSourceDir := c.MkDir()
	sourceFile := filepath.Join(baseSourceDir, "rofs/dir/source")

	baseTargetDir := c.MkDir()
	targetFile := filepath.Join(baseTargetDir, "target")

	as := update.Assumptions{}
	restore := as.MockUnrestrictedPaths("/")
	defer restore()

	// mock for permission errors
	restore = update.MockSysFchown(func(fd int, uid sys.UserID, gid sys.GroupID) error {
		return nil
	})
	defer restore()

	// mock for permission errors
	restore = update.MockSysMount(func(source, target, fstype string, flags uintptr, data string) (err error) {
		return nil
	})
	defer restore()

	// mock for permission errors
	restore = update.MockSysUnmount(func(target string, flags int) (err error) {
		return nil
	})
	defer restore()

	var mkdiratFailed bool
	restore = update.MockSysMkdirat(func(dirfd int, path string, mode uint32) (err error) {
		if path == "dir" && !mkdiratFailed {
			mkdiratFailed = true
			return syscall.EROFS
		}
		return syscall.Mkdirat(dirfd, path, mode)
	})
	defer restore()

	desiredMountEntry := osutil.MountEntry{
		Name:    sourceFile,
		Dir:     targetFile,
		Options: []string{"rbind", "rw", "x-snapd.id=test-id", osutil.XSnapdKindFile(), osutil.XSnapdOriginLayout()},
	}

	var saved osutil.MountProfile
	upCtx := &testProfileUpdateContext{
		assumptions: func() *update.Assumptions {
			return &as
		},
		loadDesiredProfile: func() (*osutil.MountProfile, error) {
			return &osutil.MountProfile{Entries: []osutil.MountEntry{desiredMountEntry}}, nil
		},
		loadCurrentProfile: func() (*osutil.MountProfile, error) {
			return &saved, nil
		},
		saveCurrentProfile: func(profile *osutil.MountProfile) error {
			saved.Entries = profile.Entries
			return nil
		},
	}

	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
	c.Assert(saved.Entries, HasLen, 2)
	// synth mount created due to read-only fs
	c.Check(saved.Entries[0].Type, Equals, "tmpfs")
	c.Check(saved.Entries[0].Name, Equals, "tmpfs")
	c.Check(saved.Entries[0].Dir, Equals, filepath.Join(baseSourceDir, "rofs"))
	c.Check(saved.Entries[0].XSnapdSynthetic(), Equals, true)
	c.Check(saved.Entries[0].XSnapdNeededBy(), Equals, "test-id")
	// desired mount exists
	c.Check(saved.Entries[1], DeepEquals, desiredMountEntry)

	c.Assert(update.ExecuteMountProfileUpdate(upCtx), IsNil)
	c.Assert(saved.Entries, HasLen, 2)
	// TODO: the change of order is a bit unexpected, but that is a larger issue
	// synth mount kept because it is needed by a desired mount
	c.Check(saved.Entries[1].Type, Equals, "tmpfs")
	c.Check(saved.Entries[1].Name, Equals, "tmpfs")
	c.Check(saved.Entries[1].Dir, Equals, filepath.Join(baseSourceDir, "rofs"))
	c.Check(saved.Entries[1].XSnapdSynthetic(), Equals, true)
	c.Check(saved.Entries[1].XSnapdNeededBy(), Equals, "test-id")
	c.Check(saved.Entries[0], DeepEquals, desiredMountEntry)
}

// testProfileUpdateContext implements MountProfileUpdateContext and is suitable for testing.
type testProfileUpdateContext struct {
	loadCurrentProfile func() (*osutil.MountProfile, error)
	loadDesiredProfile func() (*osutil.MountProfile, error)
	saveCurrentProfile func(*osutil.MountProfile) error
	assumptions        func() *update.Assumptions

	// The remaining functions are defined for consistency but are installed by
	// calling their mock helpers. They are not a part of the interface.
	neededChanges func(*osutil.MountProfile, *osutil.MountProfile) []*update.Change
	performChange func(*update.Change, *update.Assumptions) ([]*update.Change, error)
}

// MockRelatedFunctions mocks NeededChanges and Change.Perform for the purpose of testing.
func (upCtx *testProfileUpdateContext) MockRelatedFunctions() (restore func()) {
	neededChanges := func(*osutil.MountProfile, *osutil.MountProfile) []*update.Change { return nil }
	if upCtx.neededChanges != nil {
		neededChanges = upCtx.neededChanges
	}
	restore1 := update.MockNeededChanges(neededChanges)

	performChange := func(*update.Change, *update.Assumptions) ([]*update.Change, error) { return nil, nil }
	if upCtx.performChange != nil {
		performChange = upCtx.performChange
	}
	restore2 := update.MockChangePerform(performChange)

	return func() {
		restore1()
		restore2()
	}
}

func (upCtx *testProfileUpdateContext) Lock() (unlock func(), err error) {
	return func() {}, nil
}

func (upCtx *testProfileUpdateContext) Assumptions() *update.Assumptions {
	if upCtx.assumptions != nil {
		return upCtx.assumptions()
	}
	return &update.Assumptions{}
}

func (upCtx *testProfileUpdateContext) LoadCurrentProfile() (*osutil.MountProfile, error) {
	if upCtx.loadCurrentProfile != nil {
		return upCtx.loadCurrentProfile()
	}
	return &osutil.MountProfile{}, nil
}

func (upCtx *testProfileUpdateContext) LoadDesiredProfile() (*osutil.MountProfile, error) {
	if upCtx.loadDesiredProfile != nil {
		return upCtx.loadDesiredProfile()
	}
	return &osutil.MountProfile{}, nil
}

func (upCtx *testProfileUpdateContext) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if upCtx.saveCurrentProfile != nil {
		return upCtx.saveCurrentProfile(profile)
	}
	return nil
}
