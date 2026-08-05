// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ctlcmd_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type componentsSuite struct {
	testutil.BaseTest
	st          *state.State
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&componentsSuite{})

const snapWithTwoCompsYaml = `name: test-snap
version: 1.0
summary: test-snap
components:
  comp1:
    type: standard
  comp2:
    type: test
`

func (s *componentsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.BaseTest.AddCleanup(func() { dirs.SetRootDir("/") })

	s.mockHandler = hooktest.NewMockHandler()
	s.st = state.New(nil)

	s.st.Lock()
	defer s.st.Unlock()

	info := snaptest.MockSnapCurrent(c, snapWithTwoCompsYaml, &snap.SideInfo{
		RealName: "test-snap",
		Revision: snap.R(1),
	})

	snapstate.Set(s.st, info.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: info.SnapName(), Revision: info.Revision},
		}),
		Current: info.Revision,
	})

	task := s.st.NewTask("test-task", "test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
}

func (s *componentsSuite) TestNoComponents(c *C) {
	// Set up snap state pointing at a revision with no declared components.
	// We need a different snap name to avoid conflicting with the "current"
	// symlink already created for "test-snap" in SetUpTest.
	const noCompSnapYaml = `name: no-comp-snap
version: 1.0
summary: no-comp-snap
`
	info := snaptest.MockSnapCurrent(c, noCompSnapYaml, &snap.SideInfo{
		RealName: "no-comp-snap",
		Revision: snap.R(1),
	})
	s.st.Lock()
	snapstate.Set(s.st, info.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: info.SnapName(), Revision: info.Revision},
		}),
		Current: info.Revision,
	})
	task := s.st.NewTask("no-comp-task", "no comp task")
	s.st.Unlock()
	setup := &hookstate.HookSetup{Snap: "no-comp-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	stdout, stderr, _, err := ctlcmd.Run(ctx, []string{"components"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Matches, `(?s).*No components are available for this snap.*`)
}

func (s *componentsSuite) setInstalledComponents(compNames []string, compRevs []snap.Revision) {
	compStates := make([]*sequence.ComponentState, len(compNames))
	for i, name := range compNames {
		cref := naming.NewComponentRef("test-snap", name)
		csi := snap.NewComponentSideInfo(cref, compRevs[i])
		compStates[i] = sequence.NewComponentState(csi, snap.StandardComponent)
	}

	s.st.Lock()
	defer s.st.Unlock()

	snapstate.Set(s.st, "test-snap", &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(
				&snap.SideInfo{RealName: "test-snap", Revision: snap.R(1)},
				compStates,
			),
		}),
		Current: snap.R(1),
	})
}

func (s *componentsSuite) TestAllAvailable(c *C) {
	stdout, stderr, _, err := ctlcmd.Run(s.mockContext, []string{"components"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stderr), Equals, "")
	c.Check(string(stdout), Equals, `
Component  Status     Type
+comp1     available  standard
+comp2     available  test
`[1:])
}

func (s *componentsSuite) TestOneInstalledOneAvailable(c *C) {
	s.setInstalledComponents([]string{"comp2"}, []snap.Revision{snap.R(11)})

	stdout, stderr, _, err := ctlcmd.Run(s.mockContext, []string{"components"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stderr), Equals, "")
	// installed components come first, regardless of name order
	c.Check(string(stdout), Matches, `(?s).*\+comp2\s+installed\s+test.*\+comp1\s+available\s+standard.*`)
}

func (s *componentsSuite) TestAllInstalled(c *C) {
	s.setInstalledComponents(
		[]string{"comp1", "comp2"},
		[]snap.Revision{snap.R(11), snap.R(22)},
	)

	// non-root users can also run snapctl components
	stdout, stderr, _, err := ctlcmd.Run(s.mockContext, []string{"components"}, 1000, nil)
	c.Assert(err, IsNil)
	c.Check(string(stderr), Equals, "")
	// alphabetical order within the installed group
	c.Check(string(stdout), Matches, `(?s).*\+comp1\s+installed\s+standard.*\+comp2\s+installed\s+test.*`)
}

func (s *componentsSuite) TestEnsureContextError(c *C) {
	_, _, _, err := ctlcmd.Run(nil, []string{"components"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "components"\) from outside of a snap`)
}

func (s *componentsSuite) TestCurrentInfoError(c *C) {
	s.st.Lock()
	task := s.st.NewTask("no-state-task", "no state task")
	s.st.Unlock()
	setup := &hookstate.HookSetup{Snap: "no-state-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	_, _, _, err = ctlcmd.Run(ctx, []string{"components"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot get snap info: snap "no-state-snap" is not installed`)
}

func (s *componentsSuite) TestExtraArgsRejected(c *C) {
	_, _, _, err := ctlcmd.Run(s.mockContext, []string{"components", "unexpected"}, 0, nil)
	c.Assert(err, ErrorMatches, `unexpected arguments: \[unexpected\]`)
}
