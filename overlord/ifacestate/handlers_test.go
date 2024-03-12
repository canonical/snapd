// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package ifacestate_test

import (
	"path"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
)

const snapAyaml = `name: snap-a
type: app
base: base-snap-a
`

type handlersSuite struct {
	st *state.State
}

var _ = Suite(&handlersSuite{})

func (s *handlersSuite) SetUpTest(c *C) {
	s.st = state.New(nil)
	dirs.SetRootDir(c.MkDir())
}

func (s *handlersSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *handlersSuite) TestInSameChangeWaitChain(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// no wait chain (yet)
	startT := s.st.NewTask("start", "...start")
	intermediateT := s.st.NewTask("intermediateT", "...intermediateT")
	searchT := s.st.NewTask("searchT", "...searchT")
	c.Check(ifacestate.InSameChangeWaitChain(startT, searchT), Equals, false)

	// add (indirect) wait chain
	searchT.WaitFor(intermediateT)
	intermediateT.WaitFor(startT)
	c.Check(ifacestate.InSameChangeWaitChain(startT, searchT), Equals, true)
}

func (s *handlersSuite) TestInSameChangeWaitChainDifferentChanges(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t1 := s.st.NewTask("t1", "...")
	chg1 := s.st.NewChange("chg1", "...")
	chg1.AddTask(t1)

	t2 := s.st.NewTask("t2", "...")
	chg2 := s.st.NewChange("chg2", "...")
	chg2.AddTask(t2)

	// add a cross change wait chain
	t2.WaitFor(t1)
	c.Check(ifacestate.InSameChangeWaitChain(t1, t2), Equals, false)
}

func (s *handlersSuite) TestInSameChangeWaitChainWithCycles(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// cycles like this are unexpected in practice but are easier to test than
	// the exponential paths situation that e.g. seed changes present.
	startT := s.st.NewTask("start", "...start")
	task1 := s.st.NewTask("task1", "...")
	task1.WaitFor(startT)
	task2 := s.st.NewTask("task2", "...")
	task2.WaitFor(task1)
	task3 := s.st.NewTask("task3", "...")
	task3.WaitFor(task2)

	startT.WaitFor(task2)
	startT.WaitFor(task3)

	unrelated := s.st.NewTask("unrelated", "...")
	c.Check(ifacestate.InSameChangeWaitChain(startT, unrelated), Equals, false)
}

func mockInstalledSnap(c *C, st *state.State, snapYaml string) *snap.Info {
	snapInfo := snaptest.MockSnap(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(1),
	})

	snapName := snapInfo.SnapName()
	si := &snap.SideInfo{RealName: snapName, SnapID: snapName + "-id", Revision: snap.R(1)}
	snapstate.Set(st, snapName, &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: string(snapInfo.Type()),
	})
	return snapInfo
}

func (s *handlersSuite) TestBuildConfinementOptions(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	snapInfo := mockInstalledSnap(c, s.st, snapAyaml)
	flags := snapstate.Flags{}
	opts, err := ifacestate.BuildConfinementOptions(s.st, snapInfo, snapstate.Flags{})

	c.Check(err, IsNil)
	c.Check(len(opts.ExtraLayouts), Equals, 0)
	c.Check(opts.Classic, Equals, flags.Classic)
	c.Check(opts.DevMode, Equals, flags.DevMode)
	c.Check(opts.JailMode, Equals, flags.JailMode)
}

func (s *handlersSuite) TestBuildConfinementOptionsWithLogNamespace(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// journal quota is still experimental, so we must enable the experimental
	// quota-groups option
	tr := config.NewTransaction(s.st)
	tr.Set("core", "experimental.quota-groups", true)
	tr.Commit()

	snapInfo := mockInstalledSnap(c, s.st, snapAyaml)

	// Create a new quota group with a journal quota
	err := servicestatetest.MockQuotaInState(s.st, "foo", "", []string{snapInfo.InstanceName()}, nil, quota.NewResourcesBuilder().WithJournalNamespace().Build())
	c.Assert(err, IsNil)

	flags := snapstate.Flags{}
	opts, err := ifacestate.BuildConfinementOptions(s.st, snapInfo, snapstate.Flags{})

	c.Check(err, IsNil)
	c.Assert(len(opts.ExtraLayouts), Equals, 1)
	c.Check(opts.ExtraLayouts[0].Bind, Equals, path.Join(dirs.SnapSystemdRunDir, "journal.snap-foo"))
	c.Check(opts.ExtraLayouts[0].Path, Equals, path.Join(dirs.SnapSystemdRunDir, "journal"))
	c.Check(opts.Classic, Equals, flags.Classic)
	c.Check(opts.DevMode, Equals, flags.DevMode)
	c.Check(opts.JailMode, Equals, flags.JailMode)
}
