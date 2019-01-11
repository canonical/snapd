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

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
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
	up := &testProfileUpdate{}
	c.Assert(update.ExecuteMountProfileUpdate(up), IsNil)
}

func (s *updateSuite) TestUpdateFlow(c *C) {
	// The flow of update is as follows:
	// - the current profile and the desired profiles are loaded
	// - the needed changes are computed
	// - the needed changes are performed (one by one)
	// - the updated current profile is saved
	var loadedCurrent, loadedDesired, changesComputed, savedCurrent bool
	var changesPerformed int
	up := &testProfileUpdate{
		loadCurrentProfile: func() (*osutil.MountProfile, error) {
			loadedCurrent = true
			return &osutil.MountProfile{}, nil
		},
		loadDesiredProfile: func() (*osutil.MountProfile, error) {
			loadedDesired = true
			return &osutil.MountProfile{}, nil
		},
		neededChanges: func(old, new *osutil.MountProfile) []*update.Change {
			changesComputed = true
			return []*update.Change{{}, {}}
		},
		performChange: func(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
			changesPerformed++
			return nil, nil
		},
		saveCurrentProfile: func(*osutil.MountProfile) error {
			savedCurrent = true
			return nil
		},
	}
	c.Assert(update.ExecuteMountProfileUpdate(up), IsNil)
	c.Check(loadedCurrent, Equals, true)
	c.Check(loadedDesired, Equals, true)
	c.Check(changesComputed, Equals, true)
	c.Check(changesPerformed, Equals, 2)
	c.Check(savedCurrent, Equals, true)
}

func (s *updateSuite) TestResultingProfile(c *C) {
	// When the mount namespace is changed by performing actions the updated
	// current profile is comprised of the past changes that were reused (kept
	// unchanged) as well as newly mounted entries. Unmounted entries simple
	// cease to be.
	var saved *osutil.MountProfile
	up := &testProfileUpdate{
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
	c.Assert(update.ExecuteMountProfileUpdate(up), IsNil)
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
	up := &testProfileUpdate{
		loadCurrentProfile: func() (*osutil.MountProfile, error) { return osutil.LoadMountProfileText(text) },
		loadDesiredProfile: func() (*osutil.MountProfile, error) { return osutil.LoadMountProfileText(text) },
		assumptions:        func() *update.Assumptions { return as },
	}

	// Perform the update, this will modify assumptions.
	c.Check(as.PastChanges(), HasLen, 0)
	c.Assert(update.ExecuteMountProfileUpdate(up), IsNil)
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
	up := &testProfileUpdate{
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
	c.Assert(update.ExecuteMountProfileUpdate(up), IsNil)
	c.Check(saved, DeepEquals, &osutil.MountProfile{Entries: []osutil.MountEntry{
		{Dir: "/subdir", Type: "tmpfs"},
		{Dir: "/subdir/mount"},
	}})
}

func (s *updateSuite) TestCannotPerformContentInterfaceChange(c *C) {
	// When performing a mount change for a content interface fails we simply
	// ignore the error carry on. Such changes are not stored in the updated
	// current profile.
	var saved *osutil.MountProfile
	up := &testProfileUpdate{
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
	c.Assert(update.ExecuteMountProfileUpdate(up), IsNil)
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
	up := &testProfileUpdate{
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
	err := update.ExecuteMountProfileUpdate(up)
	c.Check(err, Equals, errTesting)
	c.Check(saved, IsNil)
}

func (s *updateSuite) TestCannotPerformOvermountChange(c *C) {
	// When performing a mount change for an "overname", errors are immediately fatal.
	var saved *osutil.MountProfile
	up := &testProfileUpdate{
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
	err := update.ExecuteMountProfileUpdate(up)
	c.Check(err, Equals, errTesting)
	c.Check(saved, IsNil)
}

// testProfileUpdate implements MountProfileUpdate and is suitable for testing.
type testProfileUpdate struct {
	loadCurrentProfile func() (*osutil.MountProfile, error)
	loadDesiredProfile func() (*osutil.MountProfile, error)
	saveCurrentProfile func(*osutil.MountProfile) error
	neededChanges      func(old, new *osutil.MountProfile) []*update.Change
	performChange      func(*update.Change, *update.Assumptions) ([]*update.Change, error)
	assumptions        func() *update.Assumptions
}

func (up *testProfileUpdate) Lock() (unlock func(), err error) {
	return func() {}, nil
}

func (up *testProfileUpdate) Assumptions() *update.Assumptions {
	if up.assumptions != nil {
		return up.assumptions()
	}
	return &update.Assumptions{}
}

func (up *testProfileUpdate) LoadCurrentProfile() (*osutil.MountProfile, error) {
	if up.loadCurrentProfile != nil {
		return up.loadCurrentProfile()
	}
	return &osutil.MountProfile{}, nil
}

func (up *testProfileUpdate) LoadDesiredProfile() (*osutil.MountProfile, error) {
	if up.loadDesiredProfile != nil {
		return up.loadDesiredProfile()
	}
	return &osutil.MountProfile{}, nil
}

func (up *testProfileUpdate) NeededChanges(old, new *osutil.MountProfile) []*update.Change {
	if up.neededChanges != nil {
		return up.neededChanges(old, new)
	}
	return nil
}

func (up *testProfileUpdate) PerformChange(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
	if up.performChange != nil {
		return up.performChange(change, as)
	}
	return nil, nil
}

func (up *testProfileUpdate) SaveCurrentProfile(profile *osutil.MountProfile) error {
	if up.saveCurrentProfile != nil {
		return up.saveCurrentProfile(profile)
	}
	return nil
}
