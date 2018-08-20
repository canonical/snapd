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
	"fmt"
	"sort"
	"testing"

	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type patchSuite struct{}

var _ = Suite(&patchSuite{})

func (s *patchSuite) TestInit(c *C) {
	restore := patch.Mock(2, nil, 1, nil)
	defer restore()

	st := state.New(nil)
	patch.Init(st)

	st.Lock()
	defer st.Unlock()
	var patchLevel int
	err := st.Get("patch-level", &patchLevel)
	c.Assert(err, IsNil)
	c.Check(patchLevel, Equals, 2)

	var patchSublevel int
	c.Assert(st.Get("patch-sublevel", &patchSublevel), IsNil)
	c.Check(patchSublevel, Equals, 1)
}

func (s *patchSuite) TestNothingToDo(c *C) {
	restore := patch.Mock(2, nil, 0, nil)
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 2)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, IsNil)
}

func (s *patchSuite) TestNoDowngrade(c *C) {
	restore := patch.Mock(2, nil, 0, nil)
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 3)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, ErrorMatches, `cannot downgrade: snapd is too old for the current system state \(patch level 3\)`)
}

func (s *patchSuite) TestApply(c *C) {
	p12 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n+1)
		return nil
	}
	p23 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n*10)
		return nil
	}
	sp12 := func(st *state.State) error {
		var o int
		st.Get("o", &o)
		st.Set("o", o+1)
		return nil
	}

	restore := patch.Mock(3, map[int]func(*state.State) error{
		2: p12,
		3: p23,
	}, 2, map[int]func(*state.State) error{
		2: sp12,
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Set("patch-sublevel", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var level int
	err = st.Get("patch-level", &level)
	c.Assert(err, IsNil)
	c.Check(level, Equals, 3)

	var sublevel int
	c.Assert(st.Get("patch-sublevel", &sublevel), IsNil)
	c.Check(sublevel, Equals, 2)

	var n, o int
	err = st.Get("n", &n)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 10)

	c.Assert(st.Get("o", &o), IsNil)
	c.Assert(o, Equals, 1)
}

func (s *patchSuite) TestMissing(c *C) {
	restore := patch.Mock(3, map[int]func(*state.State) error{
		3: func(s *state.State) error { return nil },
	}, 0, nil)
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, ErrorMatches, `cannot upgrade: snapd is too new for the current system state \(patch level 1\)`)
}

func (s *patchSuite) TestMissingSublevel(c *C) {
	restore := patch.Mock(0, nil, 3, map[int]func(*state.State) error{
		3: func(s *state.State) error { return nil },
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-sublevel", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, ErrorMatches, `cannot upgrade: snapd is too new for the current system state \(patch sublevel 1\)`)
}

func (s *patchSuite) TestError(c *C) {
	p12 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n+1)
		return nil
	}
	p23 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n*10)
		return fmt.Errorf("boom")
	}
	p34 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n*100)
		return nil
	}
	restore := patch.Mock(3, map[int]func(*state.State) error{
		2: p12,
		3: p23,
		4: p34,
	}, 0, nil)
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, ErrorMatches, `cannot patch system state from level 2 to 3: boom`)

	st.Lock()
	defer st.Unlock()

	var level int
	err = st.Get("patch-level", &level)
	c.Assert(err, IsNil)
	c.Check(level, Equals, 2)

	var n int
	err = st.Get("n", &n)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 10)
}

func (s *patchSuite) TestSanity(c *C) {
	patches := patch.PatchesForTest()
	levels := make([]int, 0, len(patches))
	for l := range patches {
		levels = append(levels, l)
	}
	sort.Ints(levels)
	// all steps present
	for i, level := range levels {
		c.Check(level, Equals, i+1)
	}
	// ends at implemented patch level
	c.Check(levels[len(levels)-1], Equals, patch.Level)
}
