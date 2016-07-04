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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type patch1Suite struct{}

var _ = Suite(&patch1Suite{})

var statePatch1JSON = []byte(`
{
	"data": {
		"snaps": {
			"foo": {
				"sequence": [{
					"name": "foo1",
					"revision": "2"
				}, {
					"name": "foo1",
					"revision": "22"
				}]
			},

			"core": {
				"sequence": [{
					"name": "core",
					"revision": "1"
				}, {
					"name": "core",
					"revision": "11"
				}, {
					"name": "core",
					"revision": "111"
				}]
			},

			"borken": {
				"sequence": [{
					"name": "borken",
					"revision": "x1"
				}, {
					"name": "borken",
					"revision": "x2"
				}]
			},

			"wip": {
				"candidate": {
					"name": "wip",
					"revision": "11"
				}
			}
		}
	}
}
`)

func (s *patch1Suite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(dirs.SnapStateFile, statePatch1JSON, 0644)
	c.Assert(err, IsNil)
}

func (s *patch1Suite) TestPatch1(c *C) {
	restore := patch.MockReadInfo(s.readInfo)
	defer restore()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	err = patch.ApplyOne(patch.Patch1, st, 1)
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
