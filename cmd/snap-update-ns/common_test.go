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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type commonSuite struct {
	dir string
	up  *update.CommonProfileUpdate
}

var _ = Suite(&commonSuite{})

func (s *commonSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	s.up = update.NewCommonProfileUpdate("foo", false,
		filepath.Join(s.dir, "current.fstab"),
		filepath.Join(s.dir, "desired.fstab"))
}

func (s *commonSuite) TestInstanceName(c *C) {
	c.Check(s.up.InstanceName(), Equals, "foo")
}

func (s *commonSuite) TestLock(c *C) {
	// Mock away real freezer code, allowing test code to return an error when freezing.
	var freezingError error
	restore := update.MockFreezing(func(string) error { return freezingError }, func(string) error { return nil })
	defer restore()
	// Mock system directories, we use the lock directory.
	dirs.SetRootDir(s.dir)
	defer dirs.SetRootDir("")

	// We will use 2nd lock for our testing.
	testLock, err := mount.OpenLock(s.up.InstanceName())
	c.Assert(err, IsNil)
	defer testLock.Close()

	// When fromSnapConfine is false we acquire our own lock.
	s.up.SetFromSnapConfine(false)
	c.Check(s.up.FromSnapConfine(), Equals, false)
	unlock, err := s.up.Lock()
	c.Assert(err, IsNil)
	// The lock is acquired now. We should not be able to get another lock.
	c.Check(testLock.TryLock(), Equals, osutil.ErrAlreadyLocked)
	// We can release the original lock now and see our test lock working.
	unlock()
	c.Assert(testLock.TryLock(), IsNil)

	// When fromSnapConfine is true we test existing lock but don't grab one.
	s.up.SetFromSnapConfine(true)
	c.Check(s.up.FromSnapConfine(), Equals, true)
	err = testLock.Lock()
	c.Assert(err, IsNil)
	unlock, err = s.up.Lock()
	c.Assert(err, IsNil)
	unlock()

	// When the test lock is unlocked the common update helper reports an error
	// since it was expecting the lock to be held. Oh, and the lock is not leaked.
	testLock.Unlock()
	unlock, err = s.up.Lock()
	c.Check(err, ErrorMatches, `mount namespace of snap "foo" is not locked but --from-snap-confine was used`)
	c.Check(unlock, IsNil)
	c.Assert(testLock.TryLock(), IsNil)

	// When freezing fails the lock acquired internally is not leaked.
	freezingError = errTesting
	s.up.SetFromSnapConfine(false)
	c.Check(s.up.FromSnapConfine(), Equals, false)
	testLock.Unlock()
	unlock, err = s.up.Lock()
	c.Check(err, Equals, errTesting)
	c.Check(unlock, IsNil)
	c.Check(testLock.TryLock(), IsNil)
}

func (s *commonSuite) TestNeededChanges(c *C) {
	// Smoke test for computing needed changes.
	// Complete tests for the algorithm are in changes_test.go
	entry := osutil.MountEntry{Dir: "/tmp", Name: "none", Type: "tmpfs"}
	current := &osutil.MountProfile{}
	desired := &osutil.MountProfile{Entries: []osutil.MountEntry{entry}}
	changes := s.up.NeededChanges(current, desired)
	c.Check(changes, DeepEquals, []*update.Change{{Action: update.Mount, Entry: entry}})
}

func (s *commonSuite) TestPerformChange(c *C) {
	// Smoke test for performing mount namespace change.
	// Complete tests for the algorithm are in changes_test.go
	entry := osutil.MountEntry{Dir: "/tmp", Name: "none", Type: "tmpfs"}
	change := &update.Change{Action: update.Mount, Entry: entry}
	as := &update.Assumptions{}
	var changeSeen *update.Change
	var assumptionsSeen *update.Assumptions
	restore := update.MockChangePerform(func(change *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		changeSeen = change
		assumptionsSeen = as
		return nil, nil
	})
	defer restore()

	synth, err := s.up.PerformChange(change, as)
	c.Assert(err, IsNil)
	c.Check(synth, HasLen, 0)
	// NOTE: we're using Equals to check that the exact objects were passed.
	c.Check(changeSeen, Equals, change)
	c.Check(assumptionsSeen, Equals, as)
}

func (s *commonSuite) TestLoadDesiredProfile(c *C) {
	up := s.up
	text := "tmpfs /tmp tmpfs defaults 0 0\n"

	// Ask the common profile update helper to read the desired profile.
	profile, err := up.LoadCurrentProfile()
	c.Assert(err, IsNil)

	// A profile that is not present on disk just reads as a valid empty profile.
	c.Check(profile.Entries, HasLen, 0)

	// Write a desired user mount profile for snap "foo".
	path := up.DesiredProfilePath()
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the common profile update helper to read the desired profile.
	profile, err = up.LoadDesiredProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// The profile is returned unchanged.
	c.Check(builder.String(), Equals, text)
}

func (s *commonSuite) TestLoadCurrentProfile(c *C) {
	up := s.up
	text := "tmpfs /tmp tmpfs defaults 0 0\n"

	// Ask the common profile update helper to read the current profile.
	profile, err := up.LoadCurrentProfile()
	c.Assert(err, IsNil)

	// A profile that is not present on disk just reads as a valid empty profile.
	c.Check(profile.Entries, HasLen, 0)

	// Write a current user mount profile for snap "foo".
	path := up.CurrentProfilePath()
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the common profile update helper to read the current profile.
	profile, err = up.LoadCurrentProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// The profile is returned unchanged.
	c.Check(builder.String(), Equals, text)
}

func (s *commonSuite) TestSaveCurrentProfile(c *C) {
	up := s.up
	text := "tmpfs /tmp tmpfs defaults 0 0\n"

	// Prepare a mount profile to be saved.
	profile, err := osutil.LoadMountProfileText(text)
	c.Assert(err, IsNil)

	// Prepare the directory for saving the profile.
	path := up.CurrentProfilePath()
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)

	// Ask the common profile update to write the current profile.
	c.Assert(up.SaveCurrentProfile(profile), IsNil)
	c.Check(path, testutil.FileEquals, text)
}
