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

package patch_test

import (
	"errors"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type patch1Suite struct{}

var _ = Suite(&patch1Suite{})

// makeState creates a state with SnapState missing Type and Current.
func (s *patch1Suite) makeState() *state.State {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// app
	var fooSnapst snapstate.SnapState
	fooSnapst.Sequence = []*snap.SideInfo{
		{
			RealName: "foo1",
			Revision: snap.R(2),
		},
		{
			RealName: "foo1",
			Revision: snap.R(22),
		},
	}
	snapstate.Set(st, "foo", &fooSnapst)

	// core
	var coreSnapst snapstate.SnapState
	coreSnapst.Sequence = []*snap.SideInfo{
		{
			RealName: "core",
			Revision: snap.R(1),
		},
		{
			RealName: "core",
			Revision: snap.R(11),
		},
		{
			RealName: "core",
			Revision: snap.R(111),
		},
	}
	snapstate.Set(st, "core", &coreSnapst)

	// broken
	var borkenSnapst snapstate.SnapState
	borkenSnapst.Sequence = []*snap.SideInfo{
		{
			RealName: "borken",
			Revision: snap.R("x1"),
		},
		{
			RealName: "borken",
			Revision: snap.R("x2"),
		},
	}
	snapstate.Set(st, "borken", &borkenSnapst)

	var wipSnapst snapstate.SnapState
	wipSnapst.Candidate = &snap.SideInfo{
		RealName: "wip",
		Revision: snap.R(11),
	}
	snapstate.Set(st, "wip", &wipSnapst)

	return st
}

func (s *patch1Suite) TestPatch1(c *C) {
	restore := patch.MockReadInfo(s.readInfo)
	defer restore()

	st := s.makeState()

	err := patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	expected := []struct {
		name string
		typ  snap.Type
		cur  snap.Revision
	}{
		{"foo", snap.TypeApp, snap.R(22)},
		{"core", snap.TypeOS, snap.R(111)},
		{"borken", snap.TypeApp, snap.R(-2)},
		{"wip", "", snap.R(0)},
	}

	for _, exp := range expected {
		var snapst snapstate.SnapState
		err := snapstate.Get(st, exp.name, &snapst)
		c.Assert(err, IsNil)
		c.Check(snap.Type(snapst.SnapType), Equals, exp.typ)
		c.Check(snapst.Current, Equals, exp.cur)
	}
}

func (s *patch1Suite) readInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	if name == "borken" {
		return nil, errors.New(`cannot read info for "borken" snap`)
	}
	// naive emulation for now, always works
	info := &snap.Info{SuggestedName: name, SideInfo: *si}
	info.Type = snap.TypeApp
	if name == "gadget" {
		info.Type = snap.TypeGadget
	}
	if name == "core" {
		info.Type = snap.TypeOS
	}
	return info, nil
}
