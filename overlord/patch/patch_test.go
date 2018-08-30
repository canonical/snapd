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
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type patchSuite struct {
	restoreSanitize func()
}

var _ = Suite(&patchSuite{})

func (s *patchSuite) SetUpTest(c *C) {
	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
}

func (s *patchSuite) TearDownTest(c *C) {
	s.restoreSanitize()
}

func (s *patchSuite) TestInit(c *C) {
	restore := patch.Mock(2, 1, nil)
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
	restore := patch.Mock(2, 1, nil)
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 2)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, IsNil)
}

func (s *patchSuite) TestNoDowngrade(c *C) {
	restore := patch.Mock(2, 0, nil)
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
	p121 := func(st *state.State) error {
		var o int
		st.Get("o", &o)
		st.Set("o", o+1)
		return nil
	}
	p23 := func(st *state.State) error {
		var n int
		st.Get("n", &n)
		st.Set("n", n*10)
		return nil
	}

	// patch level 3, sublevel 1
	restore := patch.Mock(3, 1, map[int][]patch.PatchFunc{
		2: {p12, p121},
		3: {p23},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
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
	c.Check(sublevel, Equals, 0)

	var n, o int
	err = st.Get("n", &n)
	c.Assert(err, IsNil)
	c.Check(n, Equals, 10)

	c.Assert(st.Get("o", &o), IsNil)
	c.Assert(o, Equals, 1)
}

func (s *patchSuite) TestApplyLevel6(c *C) {
	var sequence []int
	p50 := generatePatchFunc(50, &sequence)
	p60 := generatePatchFunc(60, &sequence)
	p61 := generatePatchFunc(61, &sequence)

	restore := patch.Mock(6, 1, map[int][]patch.PatchFunc{
		5: {p50},
		6: {p60, p61},
	})
	defer restore()

	// simulate the special case where sublevel is introduced for system that's already on patch level 6.
	// only p61 patch should be applied.
	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 6)
	st.Unlock()
	c.Assert(patch.Apply(st), IsNil)

	st.Lock()
	defer st.Unlock()

	var level, sublevel int
	c.Assert(sequence, DeepEquals, []int{61})
	c.Assert(st.Get("patch-level", &level), IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), IsNil)
	c.Check(level, Equals, 6)
	c.Check(sublevel, Equals, 1)
}

func (s *patchSuite) TestApplyFromSublevel(c *C) {
	var sequence []int
	p60 := generatePatchFunc(60, &sequence)
	p61 := generatePatchFunc(61, &sequence)
	p62 := generatePatchFunc(62, &sequence)
	p70 := generatePatchFunc(70, &sequence)
	p71 := generatePatchFunc(71, &sequence)

	restore := patch.Mock(7, 1, map[int][]patch.PatchFunc{
		6: {p60, p61, p62},
		7: {p70, p71},
	})
	defer restore()

	// we'll be patching from 6.0 -> 7.1
	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 6)
	st.Set("patch-sublevel", 0)
	st.Unlock()
	c.Assert(patch.Apply(st), IsNil)

	st.Lock()

	var level, sublevel int
	c.Assert(st.Get("patch-level", &level), IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), IsNil)
	c.Check(level, Equals, 7)
	c.Check(sublevel, Equals, 1)
	c.Assert(sequence, DeepEquals, []int{61, 62, 70, 71})

	// now patching from 7.1 -> 7.2
	sequence = []int{}
	p72 := generatePatchFunc(72, &sequence)
	patch.Mock(7, 2, map[int][]patch.PatchFunc{
		6: {p60, p61, p62},
		7: {p70, p71, p72},
	})

	st.Unlock()
	c.Assert(patch.Apply(st), IsNil)
	c.Assert(sequence, DeepEquals, []int{72})

	st.Lock()
	defer st.Unlock()

	c.Assert(st.Get("patch-level", &level), IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), IsNil)
	c.Check(level, Equals, 7)
	c.Check(sublevel, Equals, 2)
}

func (s *patchSuite) TestMissing(c *C) {
	restore := patch.Mock(3, 0, map[int][]patch.PatchFunc{
		3: {func(s *state.State) error { return nil }},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, ErrorMatches, `cannot upgrade: snapd is too new for the current system state \(patch level 1\)`)
}

func (s *patchSuite) TestDowngradeSublevel(c *C) {
	restore := patch.Mock(3, 1, map[int][]patch.PatchFunc{
		3: {func(s *state.State) error { return nil }},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 3)
	st.Set("patch-sublevel", 6)
	st.Unlock()

	// we're at patch level 3, sublevel 6 according to state, but the implemented level is 3,1
	c.Assert(patch.Apply(st), IsNil)

	st.Lock()
	defer st.Unlock()
	var level, sublevel int
	c.Assert(st.Get("patch-level", &level), IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), IsNil)
	c.Check(level, Equals, 3)
	c.Check(sublevel, Equals, 1)
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
	restore := patch.Mock(3, 0, map[int][]patch.PatchFunc{
		2: {p12},
		3: {p23},
		4: {p34},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()
	st.Set("patch-level", 1)
	st.Unlock()
	err := patch.Apply(st)
	c.Assert(err, ErrorMatches, `cannot patch system state to level 3, sublevel 0: boom`)

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

func (s *patchSuite) TestRefreshBackFromLevel60(c *C) {
	var sequence []int

	p60 := generatePatchFunc(60, &sequence)
	p61 := generatePatchFunc(61, &sequence)
	p62 := generatePatchFunc(62, &sequence)

	restore := patch.Mock(6, 2, map[int][]patch.PatchFunc{
		6: {p60, p61, p62},
	})
	defer restore()

	st := state.New(nil)
	st.Lock()

	// simulate the situation where core was refreshed
	// from a revision with patch level 6 that's not sublevel-aware back to 6.2.
	st.Set("patch-level", 6)
	st.Set("patch-sublevel", 2)

	const coreYaml = `name: core
version: 1
type: os
`
	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(5142)}
	siCore2 := &snap.SideInfo{RealName: "core", Revision: snap.R(5500)}
	snaptest.MockSnapCurrent(c, coreYaml, siCore2)
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{siCore1, siCore2},
		Current:  siCore2.Revision,
	})
	pastTime := time.Now().Add(-23 * time.Hour)
	c.Assert(os.Chtimes(filepath.Join(dirs.SnapMountDir, "core", "current"), pastTime, pastTime), IsNil)
	st.Unlock()

	c.Assert(patch.Apply(st), IsNil)
	c.Assert(sequence, DeepEquals, []int{61, 62})

	// the patches shouldn't be applied again
	sequence = []int{}
	c.Assert(patch.Apply(st), IsNil)
	c.Assert(sequence, HasLen, 0)

	// new sublevel patch 6.3 gets implemented, and is applied
	p63 := generatePatchFunc(63, &sequence)
	patch.Mock(6, 3, map[int][]patch.PatchFunc{
		6: {p60, p61, p62, p63},
	})

	c.Assert(patch.Apply(st), IsNil)
	c.Assert(sequence, DeepEquals, []int{63})
}
func (s *patchSuite) TestRefreshBackFromLevel60ShortSequence(c *C) {
	var sequence []int

	p60 := generatePatchFunc(60, &sequence)
	p61 := generatePatchFunc(61, &sequence)
	p62 := generatePatchFunc(62, &sequence)

	restore := patch.Mock(6, 2, map[int][]patch.PatchFunc{
		6: {p60, p61, p62},
	})

	defer restore()

	st := state.New(nil)
	st.Lock()

	// simulate the situation where core was refreshed
	// from a revision with patch level 6 that's not sublevel-aware back to 6.2,
	// but sequence for core is missing.
	st.Set("patch-level", 6)
	st.Set("patch-sublevel", 2)

	siCore := &snap.SideInfo{RealName: "core", Revision: snap.R(5500)}
	snapstate.Set(st, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{siCore},
		Current:  siCore.Revision,
	})
	st.Unlock()

	c.Assert(patch.Apply(st), IsNil)
	c.Assert(sequence, HasLen, 0)

	st.Lock()
	defer st.Unlock()
	var level, sublevel int
	c.Assert(st.Get("patch-level", &level), IsNil)
	c.Assert(st.Get("patch-sublevel", &sublevel), IsNil)
	c.Check(level, Equals, 6)
	c.Check(sublevel, Equals, 2)
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

	// Sublevel matches the number of patches for last Level.
	c.Check(len(patches[patch.Level])-1, Equals, patch.Sublevel)
}

func generatePatchFunc(testValue int, sequence *[]int) patch.PatchFunc {
	return func(st *state.State) error {
		*sequence = append(*sequence, testValue)
		return nil
	}
}
