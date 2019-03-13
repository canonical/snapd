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
	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
)

type commonSuite struct {
	dir string
	up  *update.CommonProfileUpdate
}

var _ = Suite(&commonSuite{})

func (s *commonSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	s.up = &update.CommonProfileUpdate{}
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
