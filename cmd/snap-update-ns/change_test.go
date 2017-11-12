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
	"errors"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/testutil"
)

type changeSuite struct {
	testutil.BaseTest
	sys *update.SyscallRecorder
}

var (
	errTesting = errors.New("testing")
)

var _ = Suite(&changeSuite{})

func (s *changeSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// Mock and record system interactions.
	s.sys = &update.SyscallRecorder{}
	s.BaseTest.AddCleanup(update.MockSystemCalls(s.sys))
}

func (s *changeSuite) TestString(c *C) {
	change := update.Change{
		Entry:  mount.Entry{Dir: "/a/b", Name: "/dev/sda1"},
		Action: update.Mount,
	}
	c.Assert(change.String(), Equals, "mount (/dev/sda1 /a/b none defaults 0 0)")
}

// When there are no profiles we don't do anything.
func (s *changeSuite) TestNeededChangesNoProfiles(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, IsNil)
}

// When the profiles are the same we don't do anything.
func (s *changeSuite) TestNeededChangesNoChange(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuf"}}}
	desired := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuf"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/stuf"}, Action: update.Keep},
	})
}

// When the content interface is connected we should mount the new entry.
func (s *changeSuite) TestNeededChangesTrivialMount(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuf"}}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: desired.Entries[0], Action: update.Mount},
	})
}

// When the content interface is disconnected we should unmount the mounted entry.
func (s *changeSuite) TestNeededChangesTrivialUnmount(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{{Dir: "/common/stuf"}}}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: current.Entries[0], Action: update.Unmount},
	})
}

// When umounting we unmount children before parents.
func (s *changeSuite) TestNeededChangesUnmountOrder(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuf/extra"},
		{Dir: "/common/stuf"},
	}}
	desired := &mount.Profile{}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/stuf/extra"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuf"}, Action: update.Unmount},
	})
}

// When mounting we mount the parents before the children.
func (s *changeSuite) TestNeededChangesMountOrder(c *C) {
	current := &mount.Profile{}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuf/extra"},
		{Dir: "/common/stuf"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/stuf"}, Action: update.Mount},
		{Entry: mount.Entry{Dir: "/common/stuf/extra"}, Action: update.Mount},
	})
}

// When parent changes we don't reuse its children
func (s *changeSuite) TestNeededChangesChangedParentSameChild(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuf", Name: "/dev/sda1"},
		{Dir: "/common/stuf/extra"},
		{Dir: "/common/unrelated"},
	}}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuf", Name: "/dev/sda2"},
		{Dir: "/common/stuf/extra"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: mount.Entry{Dir: "/common/stuf/extra"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuf", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuf", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: mount.Entry{Dir: "/common/stuf/extra"}, Action: update.Mount},
	})
}

// When child changes we don't touch the unchanged parent
func (s *changeSuite) TestNeededChangesSameParentChangedChild(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuf"},
		{Dir: "/common/stuf/extra", Name: "/dev/sda1"},
		{Dir: "/common/unrelated"},
	}}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/common/stuf"},
		{Dir: "/common/stuf/extra", Name: "/dev/sda2"},
		{Dir: "/common/unrelated"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/common/unrelated"}, Action: update.Keep},
		{Entry: mount.Entry{Dir: "/common/stuf/extra", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/common/stuf"}, Action: update.Keep},
		{Entry: mount.Entry{Dir: "/common/stuf/extra", Name: "/dev/sda2"}, Action: update.Mount},
	})
}

// cur = ['/a/b', '/a/b-1', '/a/b-1/3', '/a/b/c']
// des = ['/a/b', '/a/b-1', '/a/b/c'
//
// We are smart about comparing entries as directories. Here even though "/a/b"
// is a prefix of "/a/b-1" it is correctly reused.
func (s *changeSuite) TestNeededChangesSmartEntryComparison(c *C) {
	current := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/a/b", Name: "/dev/sda1"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b-1/3"},
		{Dir: "/a/b/c"},
	}}
	desired := &mount.Profile{Entries: []mount.Entry{
		{Dir: "/a/b", Name: "/dev/sda2"},
		{Dir: "/a/b-1"},
		{Dir: "/a/b/c"},
	}}
	changes := update.NeededChanges(current, desired)
	c.Assert(changes, DeepEquals, []*update.Change{
		{Entry: mount.Entry{Dir: "/a/b/c"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/a/b", Name: "/dev/sda1"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/a/b-1/3"}, Action: update.Unmount},
		{Entry: mount.Entry{Dir: "/a/b-1"}, Action: update.Keep},

		{Entry: mount.Entry{Dir: "/a/b", Name: "/dev/sda2"}, Action: update.Mount},
		{Entry: mount.Entry{Dir: "/a/b/c"}, Action: update.Mount},
	})
}

// Change.Perform calls the mount system call.
func (s *changeSuite) TestPerformMount(c *C) {
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "source", Dir: "target", Type: "type"}}
	c.Assert(chg.Perform(), IsNil)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`mount "source" "target" "type" 0 ""`})
}

// Change.Perform returns errors from mount system call
func (s *changeSuite) TestPerformMountError(c *C) {
	s.sys.InsertFault(`mount "source" "target" "type" 0 ""`, errTesting)
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "source", Dir: "target", Type: "type"}}
	c.Assert(chg.Perform(), Equals, errTesting)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`mount "source" "target" "type" 0 ""`})
}

// Change.Perform returns errors from bad flags
func (s *changeSuite) TestPerformMountOptionError(c *C) {
	chg := &update.Change{Action: update.Mount, Entry: mount.Entry{Name: "source", Dir: "target", Type: "type", Options: []string{"bogus"}}}
	c.Assert(chg.Perform(), ErrorMatches, `unsupported mount option: "bogus"`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}

// Change.Perform calls the unmount system call.
func (s *changeSuite) TestPerformUnmount(c *C) {
	chg := &update.Change{Action: update.Unmount, Entry: mount.Entry{Name: "source", Dir: "target", Type: "type"}}
	c.Assert(chg.Perform(), IsNil)
	// The flag 8 is UMOUNT_NOFOLLOW
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "target" UMOUNT_NOFOLLOW`})
}

// Change.Perform returns errors from unmount system call
func (s *changeSuite) TestPerformUnountError(c *C) {
	s.sys.InsertFault(`unmount "target" UMOUNT_NOFOLLOW`, errTesting)
	chg := &update.Change{Action: update.Unmount, Entry: mount.Entry{Name: "source", Dir: "target", Type: "type"}}
	c.Assert(chg.Perform(), Equals, errTesting)
	c.Assert(s.sys.Calls(), DeepEquals, []string{`unmount "target" UMOUNT_NOFOLLOW`})
}

// Change.Perform handles unknown actions.
func (s *changeSuite) TestPerformUnknownAction(c *C) {
	chg := &update.Change{Action: update.Action(42)}
	c.Assert(chg.Perform(), ErrorMatches, `cannot process mount change, unknown action: .*`)
	c.Assert(s.sys.Calls(), HasLen, 0)
}
