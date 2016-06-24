// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

// makeMigrationTestState creates a test state with a bunch of snaps
// for migration testing
func (s *snapmgrTestSuite) makeMigrationTestState() {

	// app
	var fooSnapst snapstate.SnapState
	fooSnapst.Sequence = []*snap.SideInfo{
		{
			OfficialName: "foo1",
			Revision:     snap.R(2),
		},
		{
			OfficialName: "foo1",
			Revision:     snap.R(22),
		},
	}
	fooSnapst.Current = snap.R(22)
	snapstate.Set(s.state, "foo", &fooSnapst)

	// core
	var coreSnapst snapstate.SnapState
	coreSnapst.Sequence = []*snap.SideInfo{
		{
			OfficialName: "core",
			Revision:     snap.R(1),
		},
		{
			OfficialName: "core",
			Revision:     snap.R(11),
		},
		{
			OfficialName: "core",
			Revision:     snap.R(111),
		},
	}
	coreSnapst.Current = snap.R(111)
	snapstate.Set(s.state, "core", &coreSnapst)

	// broken
	var borkenSnapst snapstate.SnapState
	borkenSnapst.Sequence = []*snap.SideInfo{
		{
			OfficialName: "borken",
			Revision:     snap.R("x1"),
		},
		{
			OfficialName: "borken",
			Revision:     snap.R("x2"),
		},
	}
	borkenSnapst.Current = snap.R(-2)
	snapstate.Set(s.state, "borken", &borkenSnapst)

	var wipSnapst snapstate.SnapState
	wipSnapst.Candidate = &snap.SideInfo{
		OfficialName: "wip",
		Revision:     snap.R(11),
	}
	snapstate.Set(s.state, "wip", &wipSnapst)
}

func (s *snapmgrTestSuite) TestMigrateToTypeInState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.makeMigrationTestState()
	err := snapstate.MigrateToTypeInState(s.state)
	c.Assert(err, IsNil)

	expected := []struct {
		name string
		typ  snap.Type
	}{
		{"foo", snap.TypeApp},
		{"core", snap.TypeOS},
		{"borken", snap.TypeApp},
	}

	for _, exp := range expected {
		var snapst snapstate.SnapState
		err := snapstate.Get(s.state, exp.name, &snapst)
		c.Assert(err, IsNil)
		typ, err := snapst.Type()
		c.Check(err, IsNil)
		c.Check(typ, Equals, exp.typ)
	}

	// wip was left alone
	var snapst snapstate.SnapState
	err = snapstate.Get(s.state, "wip", &snapst)
	c.Assert(err, IsNil)
	c.Check(snapst.SnapType, Equals, "")
}

func (s *snapmgrTestSuite) TestMigrateToCurrentRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.makeMigrationTestState()

	// ensure all Current revs are unset
	var stateMap map[string]*snapstate.SnapState
	err := s.state.Get("snaps", &stateMap)
	c.Assert(err, IsNil)
	for _, snapState := range stateMap {
		snapState.Current = snap.Revision{}
	}
	s.state.Set("snaps", stateMap)

	// now do the migration
	err = snapstate.MigrateToCurrentRevision(s.state)
	c.Assert(err, IsNil)

	expected := []struct {
		name string
		rev  snap.Revision
	}{
		{"foo", snap.R(22)},
		{"core", snap.R(111)},
		{"borken", snap.R(-2)},
		{"wip", snap.Revision{}},
	}

	for _, exp := range expected {
		var snapst snapstate.SnapState
		err := snapstate.Get(s.state, exp.name, &snapst)
		c.Assert(err, IsNil)
		c.Check(snapst.Current, Equals, exp.rev)
	}
}
