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
                "patch-level": 0,
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
	err = os.WriteFile(dirs.SnapStateFile, statePatch1JSON, 0644)
	c.Assert(err, IsNil)
}

func (s *patch1Suite) TestPatch1(c *C) {
	restore := patch.MockPatch1ReadType(s.readType)
	defer restore()

	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	// go from patch-level 0 to patch-level 1
	restorer := patch.MockLevel(1, 1)
	defer restorer()

	err = patch.Apply(st)
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

	// ensure we only moved forward to patch-level 1
	var patchLevel int
	err = st.Get("patch-level", &patchLevel)
	c.Assert(err, IsNil)
	c.Assert(patchLevel, Equals, 1)
}

func (s *patch1Suite) readType(name string, rev snap.Revision) (snap.Type, error) {
	if name == "borken" {
		return snap.TypeApp, errors.New(`cannot read info for "borken" snap`)
	}
	// naive emulation for now, always works
	if name == "gadget" {
		return snap.TypeGadget, nil
	}
	if name == "core" {
		return snap.TypeOS, nil
	}

	return snap.TypeApp, nil
}
