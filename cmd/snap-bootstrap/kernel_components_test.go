// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"os"
	"time"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

type kernelCompSuite struct {
	testutil.BaseTest

	tmpDir string
}

var _ = Suite(&kernelCompSuite{})

func (s *kernelCompSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	_, r := logger.MockLogger()
	s.AddCleanup(r)

	// mock /run/mnt
	s.tmpDir = c.MkDir()
	dirs.SetRootDir(s.tmpDir)
	r = func() { dirs.SetRootDir("") }
	s.AddCleanup(r)
}

type testReadStateBackend struct {
	path string
}

func (b *testReadStateBackend) EnsureBefore(d time.Duration) {
	panic("cannot use EnsureBefore in testReadStateBackend")
}

func (b *testReadStateBackend) Checkpoint(data []byte) error {
	return osutil.AtomicWriteFile(b.path, data, 0600, 0)
}

var _ state.Backend = &testReadStateBackend{}

func (s *kernelCompSuite) TestReadState(c *C) {
	// make <root>/var/lib/snapd/
	snapdStateDir := dirs.SnapdStateDir(s.tmpDir)
	c.Assert(os.MkdirAll(snapdStateDir, 0755), IsNil)
	// Write state with components
	st := state.New(&testReadStateBackend{path: dirs.SnapStateFile})
	st.Lock()
	const snapName = "mykernel"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(1)
	compRev := snap.R(33)
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi2 := snap.NewComponentSideInfo(cref2, compRev)

	snapSt := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*snapstate.RevisionSideState{
				snapstate.NewRevisionSideInfo(ssi,
					[]*snap.ComponentSideInfo{csi, csi2})}),
		Current: snapRev,
	}
	snapstate.Set(st, snapName, snapSt)
	st.Unlock()

	conts, err := main.KernelComponentsToMount(snapName, s.tmpDir)
	c.Assert(err, IsNil)
	c.Assert(len(conts), Equals, 2)
	c.Check(conts[0].Filename(), Equals, "mykernel+mycomp_33.comp")
	c.Check(conts[0].MountDir(), testutil.Contains, "snap/mykernel/components/1/mycomp")
	c.Check(conts[1].Filename(), Equals, "mykernel+mycomp2_33.comp")
	c.Check(conts[1].MountDir(), testutil.Contains, "snap/mykernel/components/1/mycomp2")
}

func (s *kernelCompSuite) TestReadStateNoState(c *C) {
	conts, err := main.KernelComponentsToMount("mykernel", s.tmpDir)
	c.Assert(err, ErrorMatches, `cannot open state file .*`)
	c.Assert(conts, IsNil)
}

func (s *kernelCompSuite) TestReadStateBadState(c *C) {
	snapdStateDir := dirs.SnapdStateDir(s.tmpDir)
	c.Assert(os.MkdirAll(snapdStateDir, 0755), IsNil)
	c.Assert(os.WriteFile(dirs.SnapStateFileUnder(s.tmpDir), []byte{}, 0644), IsNil)

	conts, err := main.KernelComponentsToMount("mykernel", s.tmpDir)
	c.Assert(err, ErrorMatches, `error reading state file .*`)
	c.Assert(conts, IsNil)
}

func (s *kernelCompSuite) TestReadStateNoSnapInState(c *C) {
	// make <root>/var/lib/snapd/
	snapdStateDir := dirs.SnapdStateDir(s.tmpDir)
	c.Assert(os.MkdirAll(snapdStateDir, 0755), IsNil)
	// Write state with components
	st := state.New(&testReadStateBackend{path: dirs.SnapStateFile})
	st.Lock()
	st.Unlock()

	conts, err := main.KernelComponentsToMount("mykernel", s.tmpDir)
	c.Assert(err, ErrorMatches, `error reading state for snap "mykernel".*`)
	c.Assert(conts, IsNil)
}
