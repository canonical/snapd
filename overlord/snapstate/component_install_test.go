// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	. "gopkg.in/check.v1"
)

const (
	compOptIsLocal = 1 << iota
	compOptRevisionPresent
	compOptComponentInstalled
)

// opts is a bitset with compOpt* as possible values.
func expectedComponentInstallTasks(opts int) []string {
	var startTasks []string
	// Installation of a local component container
	if opts&compOptIsLocal != 0 {
		startTasks = []string{"prepare-component"}
	} else {
		startTasks = []string{"download-component"}
	}
	// Revision is not the same as the current one installed
	if opts&compOptRevisionPresent == 0 {
		startTasks = append(startTasks, "mount-component")
	}
	// Component is installed (implicit if compOptRevisionPresent is set)
	if opts&compOptComponentInstalled != 0 || opts&compOptRevisionPresent != 0 {
		startTasks = append(startTasks, "unlink-current-component")
	}
	// link-component is always present
	startTasks = append(startTasks, "link-component")

	return startTasks
}

func verifyComponentInstallTasks(c *C, opts int, ts *state.TaskSet) {
	kinds := taskKinds(ts.Tasks())

	expected := expectedComponentInstallTasks(opts)

	c.Assert(kinds, DeepEquals, expected)
}

func createTestComponent(c *C, snapName, compName string) (*snap.ComponentInfo, string) {
	componentYaml := fmt.Sprintf(`component: %s+%s
type: test
version: 1.0
`, snapName, compName)
	compPath := snaptest.MakeTestComponent(c, componentYaml)
	compf, err := snapfile.Open(compPath)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf)
	c.Assert(err, IsNil)

	return ci, compPath
}

func createTestSnapInfoForComponent(c *C, snapName string, snapRev snap.Revision, compName string) *snap.Info {
	snapYaml := fmt.Sprintf(`name: %s
type: app
version: 1.1
components:
  %s:
    type: test
`, snapName, compName)
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	info.SideInfo = snap.SideInfo{RealName: snapName, Revision: snapRev}

	return info
}

func (s *snapmgrTestSuite) setStateWithOneComponent(c *C, snapName string,
	snapRev snap.Revision, compName string, compRev snap.Revision) {
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "snapidididididididididididididid"}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*snapstate.RevisionSideState{
				snapstate.NewRevisionSideInfo(ssi,
					[]*snap.ComponentSideInfo{csi})}),
		Current: snapRev,
	})
}

func (s *snapmgrTestSuite) TestInstallComponentPath(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	_, compPath := createTestComponent(c, snapName, compName)
	info := createTestSnapInfoForComponent(c, snapName, snap.R(1), compName)

	s.state.Lock()
	defer s.state.Unlock()

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.FileExists(compPath), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallComponentPathCompRevisionPresent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	_, compPath := createTestComponent(c, snapName, compName)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)

	s.state.Lock()
	defer s.state.Unlock()

	// Current component same revision to the one we install
	s.setStateWithOneComponent(c, snapName, snapRev, compName, compRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, compRev)
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compOptRevisionPresent, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// Temporary file is deleted
	c.Assert(osutil.FileExists(compPath), Equals, false)
}

func (s *snapmgrTestSuite) TestInstallComponentPathCompAlreadyInstalled(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(33)
	_, compPath := createTestComponent(c, snapName, compName)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)

	s.state.Lock()
	defer s.state.Unlock()

	// Current component revision different to the one we install
	s.setStateWithOneComponent(c, snapName, snapRev, compName, snap.R(7))

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, compRev)
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compOptComponentInstalled, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
}

func (s *snapmgrTestSuite) TestInstallComponentPathSnapNotActive(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	_, compPath := createTestComponent(c, snapName, compName)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)

	s.state.Lock()
	defer s.state.Unlock()

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*snapstate.RevisionSideState{
				snapstate.NewRevisionSideInfo(ssi,
					[]*snap.ComponentSideInfo{csi})}),
		Current: snapRev,
	})

	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err.Error(), Equals, `cannot install component "mysnap+mycomp" for disabled snap "mysnap"`)
	c.Assert(ts, IsNil)
	c.Assert(osutil.FileExists(compPath), Equals, true)
}
