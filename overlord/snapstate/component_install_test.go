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
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	. "gopkg.in/check.v1"
)

const (
	// Install from local file
	compOptIsLocal = 1 << iota
	// Component revision is already in snaps folder and mounted
	compOptRevisionPresent
	// Component revision is used by the currently active snap revision
	compOptIsActive
	// Component is of kernel-modules type
	compTypeIsKernMods
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
	if opts&compTypeIsKernMods != 0 {
		startTasks = append(startTasks, "prepare-kernel-modules-components")
	}
	// Component is installed (implicit if compOptRevisionPresent is set)
	if opts&compOptIsActive != 0 {
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

	// Check presence of attributes
	var firstTaskID string
	var compSetup snapstate.ComponentSetup
	var snapsup snapstate.SnapSetup
	for i, t := range ts.Tasks() {
		switch i {
		case 0:
			if t.Change() == nil {
				// Add to a change so we can call snapstate.TaskComponentSetup
				chg := t.State().NewChange("install", "install...")
				chg.AddAll(ts)
			}
			c.Assert(t.Get("component-setup", &compSetup), IsNil)
			c.Assert(t.Get("snap-setup", &snapsup), IsNil)
			firstTaskID = t.ID()
		default:
			var storedTaskID string
			c.Assert(t.Get("component-setup-task", &storedTaskID), IsNil)
			c.Assert(storedTaskID, Equals, firstTaskID)
			c.Assert(t.Get("snap-setup-task", &storedTaskID), IsNil)
			c.Assert(storedTaskID, Equals, firstTaskID)
		}
		// ComponentSetup/SnapSetup found must match the ones from the first task
		csup, ssup, err := snapstate.TaskComponentSetup(t)
		c.Assert(err, IsNil)
		c.Assert(csup, DeepEquals, &compSetup)
		c.Assert(ssup, DeepEquals, &snapsup)
	}
}

func createTestComponent(c *C, snapName, compName string, snapInfo *snap.Info) (*snap.ComponentInfo, string) {
	return createTestComponentWithType(c, snapName, compName, "test", snapInfo)
}

func createTestComponentWithType(c *C, snapName, compName string, typ string, snapInfo *snap.Info) (*snap.ComponentInfo, string) {
	componentYaml := fmt.Sprintf(`component: %s+%s
type: %s
version: 1.0
`, snapName, compName, typ)
	compPath := snaptest.MakeTestComponent(c, componentYaml)
	compf, err := snapfile.Open(compPath)
	c.Assert(err, IsNil)

	ci, err := snap.ReadComponentInfoFromContainer(compf, snapInfo)
	c.Assert(err, IsNil)

	return ci, compPath
}

func createTestSnapInfoForComponent(c *C, snapName string, snapRev snap.Revision, compName string) *snap.Info {
	return createTestSnapInfoForComponentWithType(c, snapName, snapRev, compName, "test")
}

func createTestSnapInfoForComponentWithType(c *C, snapName string, snapRev snap.Revision, compName, typ string) *snap.Info {
	snapYaml := fmt.Sprintf(`name: %s
type: app
version: 1.1
components:
  %s:
    type: %s
`, snapName, compName, typ)
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	info.SideInfo = snap.SideInfo{RealName: snapName, Revision: snapRev}

	return info
}

func createTestSnapSetup(info *snap.Info, flags snapstate.Flags) *snapstate.SnapSetup {
	return &snapstate.SnapSetup{
		Base:        info.Base,
		SideInfo:    &info.SideInfo,
		Channel:     info.Channel,
		Flags:       flags.ForSnapSetup(),
		Type:        info.Type(),
		Version:     info.Version,
		PlugsOnly:   len(info.Slots) == 0,
		InstanceKey: info.InstanceKey,
	}
}

func setStateWithOneSnap(st *state.State, snapName string, snapRev snap.Revision) {
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	snapstate.Set(st, snapName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, nil)}),
		Current: snapRev,
	})
}

func setStateWithOneComponent(st *state.State, snapName string,
	snapRev snap.Revision, compName string, compRev snap.Revision) {
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	setStateWithComponents(st, snapName, snapRev,
		[]*sequence.ComponentState{sequence.NewComponentState(csi, snap.TestComponent)})
}

func setStateWithComponents(st *state.State, snapName string,
	snapRev snap.Revision, comps []*sequence.ComponentState) {
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	snapstate.Set(st, snapName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, comps)}),
		Current: snapRev,
	})
}

func (s *snapmgrTestSuite) TestInstallComponentPath(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

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

func (s *snapmgrTestSuite) TestInstallComponentPathWrongComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	// The snap does not declare "mycomp"
	info := createTestSnapInfoForComponent(c, snapName, snapRev, "other-comp")
	_, compPath := createTestComponent(c, snapName, compName, nil)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(ts, IsNil)
	c.Assert(err, ErrorMatches, `.*"mycomp" is not a component for snap "mysnap"`)
}

func (s *snapmgrTestSuite) TestInstallComponentPathWrongType(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	// The component in snap.yaml has type different to the one in component.yaml
	// (we have to set it in this way as parsers check for allowed types).
	info.Components[compName] = &snap.Component{
		Type: "random-comp-type",
	}

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(ts, IsNil)
	c.Assert(err.Error(), Equals,
		`inconsistent component type ("random-comp-type" in snap, "test" in component)`)
}

func (s *snapmgrTestSuite) TestInstallComponentPathForParallelInstall(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const snapKey = "key"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snap.R(1), compName)
	info.InstanceKey = snapKey
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	// The instance is already installed to make sure it is checked
	instanceName := snap.InstanceName(snapName, snapKey)
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev}
	snapstate.Set(s.state, instanceName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, nil)}),
		Current:     snapRev,
		InstanceKey: snapKey,
	})

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.FileExists(compPath), Equals, true)

	var snapsup snapstate.SnapSetup
	c.Assert(ts.Tasks()[0].Get("snap-setup", &snapsup), IsNil)
	c.Assert(snapsup.InstanceKey, Equals, snapKey)
}

func (s *snapmgrTestSuite) TestInstallComponentPathWrongSnap(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, "mysnap", snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	otherInfo := createTestSnapInfoForComponent(c, "other-snap", snapRev, "mycomp")

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, "mysnap", snapRev)
	setStateWithOneSnap(s.state, "other-snap", snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, otherInfo, compPath,
		snapstate.Flags{})
	c.Assert(ts, IsNil)
	c.Assert(err, ErrorMatches,
		`component "mysnap\+mycomp" is not a component for snap "other-snap"`)
}

func (s *snapmgrTestSuite) TestInstallComponentPathCompRevisionPresent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	// Current component same revision to the one we install
	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, compRev)
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compOptRevisionPresent|compOptIsActive, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// Temporary file is deleted as component file is already in the system
	c.Assert(osutil.FileExists(compPath), Equals, false)
}

func (s *snapmgrTestSuite) TestInstallComponentPathCompRevisionPresentDiffSnapRev(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev1 := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(7)
	info := createTestSnapInfoForComponent(c, snapName, snapRev1, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	// There is a component with the same revision to the one we install
	// (but it is not for the currently active snap revision).
	ssi1 := &snap.SideInfo{RealName: snapName, Revision: snapRev1,
		SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2,
		SnapID: "some-snap-id"}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi1, nil),
				sequence.NewRevisionSideState(ssi2,
					[]*sequence.ComponentState{sequence.NewComponentState(csi, snap.TestComponent)}),
			}),
		Current: snapRev1,
	})

	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	// In this case there is no unlink-current-component, as the component
	// is not installed for the active snap revision.
	verifyComponentInstallTasks(c, compOptIsLocal|compOptRevisionPresent, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// Temporary file is deleted as component file is already in the system
	c.Assert(osutil.FileExists(compPath), Equals, false)
}

func (s *snapmgrTestSuite) TestInstallComponentPathCompAlreadyInstalled(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(33)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	// Current component revision different to the one we install
	setStateWithOneComponent(s.state, snapName, snapRev, compName, snap.R(7))

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, compRev)
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compOptIsActive, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(osutil.FileExists(compPath), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallComponentPathSnapNotActive(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi,
					[]*sequence.ComponentState{sequence.NewComponentState(csi, snap.TestComponent)})}),
		Current: snapRev,
	})

	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err.Error(), Equals, `cannot install component "mysnap+mycomp" for disabled snap "mysnap"`)
	c.Assert(ts, IsNil)
	c.Assert(osutil.FileExists(compPath), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallComponentRemodelConflict(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	tugc := s.state.NewTask("update-managed-boot-config", "update managed boot config")
	chg := s.state.NewChange("remodel", "remodel")
	chg.AddTask(tugc)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(ts, IsNil)
	c.Assert(err.Error(), Equals,
		`remodeling in progress, no other changes allowed until this is done`)
}

func (s *snapmgrTestSuite) TestInstallComponentUpdateConflict(c *C) {
	const snapName = "some-snap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	tupd, err := snapstate.Update(s.state, snapName,
		&snapstate.RevisionOptions{Channel: ""}, s.user.ID,
		snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("update", "update a snap")
	chg.AddAll(tupd)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(ts, IsNil)
	c.Assert(err.Error(), Equals,
		`snap "some-snap" has "update" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallKernelModulesComponentPath(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponentWithType(c, snapName, snapRev, compName, "kernel-modules")
	_, compPath := createTestComponentWithType(c, snapName, compName, "kernel-modules", info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Flags{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compTypeIsKernMods, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.FileExists(compPath), Equals, true)
}
