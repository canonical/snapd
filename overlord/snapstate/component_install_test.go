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
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

const (
	// Install from local file
	compOptIsLocal = 1 << iota
	// Component is being installed without matching assertions from the store
	compOptIsUnasserted
	// Component revision is already in snaps folder and mounted
	compOptRevisionPresent
	// Component revision is used by the currently active snap revision
	compOptIsActive
	// Component is of kernel-modules type
	compTypeIsKernMods
	// Current component is discarded at the end
	compCurrentIsDiscarded
	// Component is being installed with a snap, so skip setup-profiles and
	// prepare-kernel-modules-components
	compOptMultiCompInstall
	// Component is being installed with a snap that is being refreshed
	compOptDuringSnapRefresh
	// Component is being installed during a snap revert
	compOptDuringSnapRevert
)

// opts is a bitset with compOpt* as possible values.
func expectedComponentInstallTasks(opts int) []string {
	beforeMount, beforeLink, link, postOpHooksAndAfter, discard := expectedComponentInstallTasksSplit(opts)
	return append(append(append(append(beforeMount, beforeLink...), link...), postOpHooksAndAfter...), discard...)
}

func expectedComponentInstallTasksSplit(opts int) (beforeMount, beforeLink, link, postOpHooksAndAfter, discard []string) {
	if opts&compOptIsLocal != 0 || opts&compOptRevisionPresent != 0 {
		beforeMount = []string{"prepare-component"}
	} else {
		beforeMount = []string{"download-component"}
	}

	if opts&compOptIsUnasserted == 0 {
		beforeMount = append(beforeMount, "validate-component")
	}

	// Revision is not the same as the current one installed
	if opts&compOptRevisionPresent == 0 {
		beforeLink = append(beforeLink, "mount-component")
	}

	if opts&compOptIsActive != 0 {
		beforeLink = append(beforeLink, "run-hook[pre-refresh]")
	}

	// Component is installed (implicit if compOptRevisionPresent is set)
	if opts&compOptIsActive != 0 && opts&compOptDuringSnapRefresh == 0 {
		beforeLink = append(beforeLink, "unlink-current-component")
	}

	if opts&compOptMultiCompInstall == 0 {
		beforeLink = append(beforeLink, "setup-profiles")
	}

	if opts&compOptDuringSnapRevert == 0 {
		link = []string{"link-component"}
	}

	// expect the install hook if the snap wasn't already installed
	if opts&compOptIsActive == 0 {
		postOpHooksAndAfter = []string{"run-hook[install]"}
	} else {
		postOpHooksAndAfter = []string{"run-hook[post-refresh]"}
	}

	if opts&compTypeIsKernMods != 0 && opts&compOptMultiCompInstall == 0 {
		postOpHooksAndAfter = append(postOpHooksAndAfter, "prepare-kernel-modules-components")
	}

	if opts&compCurrentIsDiscarded != 0 {
		discard = append(discard, "discard-component")
	}

	return beforeMount, beforeLink, link, postOpHooksAndAfter, discard
}

func checkSetupTasks(c *C, compOpts int, ts *state.TaskSet) {
	// Check presence of snap setup / component setup in the tasks
	var firstTaskID, snapSetupTaskID string
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
			sn, err := snapstate.TaskSnapSetup(t)
			c.Assert(err, IsNil)
			snapsup = *sn
			firstTaskID = t.ID()
			if t.Has("snap-setup") {
				snapSetupTaskID = t.ID()
			} else {
				t.Get("snap-setup-task", &snapSetupTaskID)
			}
		default:
			var storedTaskID string
			c.Assert(t.Get("component-setup-task", &storedTaskID), IsNil)
			c.Assert(storedTaskID, Equals, firstTaskID)
			c.Assert(t.Get("snap-setup-task", &storedTaskID), IsNil)
			c.Assert(storedTaskID, Equals, snapSetupTaskID)
		}

		// ComponentSetup/SnapSetup found must match the ones from the first task
		csup, ssup, err := snapstate.TaskComponentSetup(t)
		c.Assert(err, IsNil)
		c.Assert(csup, DeepEquals, &compSetup)
		c.Assert(ssup, DeepEquals, &snapsup)
	}

	// we skip downloading assertions during reverts and when installing a
	// component from disk.
	c.Assert(
		compSetup.SkipAssertionsDownload,
		Equals,
		compOpts&compOptIsLocal != 0 || compOpts&compOptDuringSnapRevert != 0,
	)
}

func verifyComponentInstallTasks(c *C, opts int, ts *state.TaskSet) {
	kinds := taskKinds(ts.Tasks())

	expected := expectedComponentInstallTasks(opts)
	c.Assert(kinds, DeepEquals, expected)

	checkSetupTasks(c, opts, ts)

	t, err := ts.Edge(snapstate.LastBeforeLocalModificationsEdge)
	c.Assert(err, IsNil)

	if opts&compOptIsUnasserted == 0 {
		c.Assert(t.Kind(), Equals, "validate-component")
	} else {
		c.Assert(t.Kind(), Equals, "prepare-component")
	}

	if opts&compOptMultiCompInstall == 0 {
		snapsupTask, err := ts.Edge(snapstate.SnapSetupEdge)
		c.Assert(err, IsNil)

		var compsupsIDs []string
		err = snapsupTask.Get("component-setup-tasks", &compsupsIDs)
		c.Assert(err, IsNil)

		// for now, all non-multi-component installs are by path, so this will
		// point to prepare-component
		c.Assert(snapsupTask.Kind(), Equals, "prepare-component")
		c.Assert(compsupsIDs, DeepEquals, []string{snapsupTask.ID()})
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

	ci, err := snap.ReadComponentInfoFromContainer(compf, snapInfo, &snap.ComponentSideInfo{
		Revision: snap.R(1),
	})
	c.Assert(err, IsNil)

	return ci, compPath
}

func createTestSnapInfoForComponent(c *C, snapName string, snapRev snap.Revision, compName string) *snap.Info {
	return createTestSnapInfoForComponents(c, snapName, snapRev, map[string]string{compName: "test"})
}

func createTestSnapInfoForComponents(c *C, snapName string, snapRev snap.Revision, compNamesToType map[string]string) *snap.Info {
	snapType := "app"
	for _, typ := range compNamesToType {
		if typ == "kernel-modules" {
			snapType = "kernel"
		}
	}

	var b bytes.Buffer
	fmt.Fprintf(&b, `name: %s
type: %s
version: 1.1
components:
`, snapName, snapType)

	for compName, typ := range compNamesToType {
		fmt.Fprintf(&b, "  %s:\n    type: %s\n", compName, typ)
	}

	info, err := snap.InfoFromSnapYaml(b.Bytes())
	c.Assert(err, IsNil)

	var snapID string
	if !snapRev.Unset() && !snapRev.Local() {
		snapID = snapName + "-id"
	}

	info.SideInfo = snap.SideInfo{
		RealName: snapName,
		Revision: snapRev,
		SnapID:   snapID,
	}

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
		Current:         snapRev,
		TrackingChannel: "channel-for-components",
	})
}

func setStateWithOneComponent(st *state.State, snapName string,
	snapRev snap.Revision, compName string, compRev snap.Revision) {
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	setStateWithComponents(st, snapName, snapRev,
		[]*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)})
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
	s.testInstallComponentPath(c, testInstallComponentPathOpts{})
}

func (s *snapmgrTestSuite) TestInstallComponentPathUnasserted(c *C) {
	s.testInstallComponentPath(c, testInstallComponentPathOpts{
		unasserted: true,
	})
}

func (s *snapmgrTestSuite) TestInstallComponentPathWithLane(c *C) {
	s.testInstallComponentPath(c, testInstallComponentPathOpts{
		lane:        1,
		transaction: client.TransactionAllSnaps,
	})
}

func (s *snapmgrTestSuite) TestInstallComponentPathTransactionAllSnaps(c *C) {
	s.testInstallComponentPath(c, testInstallComponentPathOpts{
		transaction: client.TransactionAllSnaps,
	})
}

func (s *snapmgrTestSuite) TestInstallComponentPathTransactionPerSnap(c *C) {
	s.testInstallComponentPath(c, testInstallComponentPathOpts{
		transaction: client.TransactionPerSnap,
	})
}

type testInstallComponentPathOpts struct {
	lane        int
	unasserted  bool
	transaction client.TransactionType
}

func (s *snapmgrTestSuite) testInstallComponentPath(c *C, opts testInstallComponentPathOpts) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(33)
	if opts.unasserted {
		snapRev = snap.R(-1)
		compRev = snap.Revision{}
	}
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, compRev)

	installOpts := snapstate.Options{
		Flags: snapstate.Flags{
			Lane:        opts.lane,
			Transaction: opts.transaction,
		},
	}

	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath, installOpts)

	c.Assert(err, IsNil)

	expectedLane := opts.lane
	if opts.transaction != "" && opts.lane == 0 {
		expectedLane = 1
	}

	for _, t := range ts.Tasks() {
		c.Assert(t.Lanes(), DeepEquals, []int{expectedLane})
	}

	compOpts := compOptIsLocal
	if opts.unasserted {
		compOpts |= compOptIsUnasserted
	}

	verifyComponentInstallTasks(c, compOpts, ts)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.CanStat(compPath), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallUnassertedComponentFailsWithAssertedSnap(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.Revision{})
	_, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot mix asserted snap and unasserted components`)
}

func (s *snapmgrTestSuite) TestInstallAssertedComponentFailsWithUnassertedSnap(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(-1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(1))
	_, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Options{})
	c.Assert(err, ErrorMatches, `cannot mix unasserted snap and asserted components`)
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
		snapstate.Options{})
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
		snapstate.Options{})
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
		snapstate.Options{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.CanStat(compPath), Equals, true)

	var snapsup snapstate.SnapSetup
	c.Assert(ts.Tasks()[0].Get("snap-setup", &snapsup), IsNil)
	c.Assert(snapsup.InstanceKey, Equals, snapKey)
	c.Assert(snapsup.ComponentExclusiveOperation, Equals, true)
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
		snapstate.Options{})
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
		snapstate.Options{})
	c.Assert(err, IsNil)

	// note that we don't discard the component here, since the component
	// revision is the same as the one we install
	verifyComponentInstallTasks(c, compOptIsLocal|compOptRevisionPresent|compOptIsActive, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// Temporary file is deleted as component file is already in the system
	c.Assert(osutil.CanStat(compPath), Equals, false)
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
					[]*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)}),
			}),
		Current: snapRev1,
	})

	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Options{})
	c.Assert(err, IsNil)

	// In this case there is no unlink-current-component, as the component
	// is not installed for the active snap revision.
	verifyComponentInstallTasks(c, compOptIsLocal|compOptRevisionPresent, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// Temporary file is deleted as component file is already in the system
	c.Assert(osutil.CanStat(compPath), Equals, false)
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
		snapstate.Options{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compOptIsActive|compCurrentIsDiscarded, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	c.Assert(osutil.CanStat(compPath), Equals, true)
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
					[]*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)})}),
		Current: snapRev,
	})

	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Options{})
	c.Assert(err.Error(), Equals, `cannot install component "mysnap+mycomp" for disabled snap "mysnap"`)
	c.Assert(ts, IsNil)
	c.Assert(osutil.CanStat(compPath), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallComponentPathRemodelConflict(c *C) {
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
		snapstate.Options{})
	c.Assert(ts, IsNil)
	c.Assert(err.Error(), Equals,
		`remodeling in progress, no other changes allowed until this is done`)
}

func (s *snapmgrTestSuite) TestInstallComponentPathUpdateConflict(c *C) {
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
		snapstate.Options{})
	c.Assert(ts, IsNil)
	c.Assert(err.Error(), Equals,
		`snap "some-snap" has "update" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallComponentUpdateConflict(c *C) {
	const snapName = "some-snap"
	const compName = "standard-component"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		return []store.SnapResourceResult{{
			DownloadInfo: snap.DownloadInfo{
				DownloadURL: "http://example.com/" + compName,
			},
			Name:      compName,
			Revision:  3,
			Type:      "component/standard",
			Version:   "1.0",
			CreatedAt: "2024-01-01T00:00:00Z",
		}}
	}

	tupd, err := snapstate.Update(s.state, snapName,
		&snapstate.RevisionOptions{Channel: ""}, s.user.ID,
		snapstate.Flags{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("update", "update a snap")
	chg.AddAll(tupd)

	_, err = snapstate.InstallComponents(context.TODO(), s.state, []string{compName}, info, nil, snapstate.Options{})
	c.Assert(err.Error(), Equals, `snap "some-snap" has "update" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallComponentConflictsWithSelf(c *C) {
	const (
		snapName              = "some-snap"
		compName              = "standard-component"
		conflictComponentName = "kernel-modules-component"
	)

	typeMapping := map[string]string{
		compName:              "standard",
		conflictComponentName: "kernel-modules",
	}

	snapRev := snap.R(1)
	info := createTestSnapInfoForComponents(c, snapName, snapRev, typeMapping)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		results := make([]store.SnapResourceResult, 0, 2)
		for _, name := range []string{compName, conflictComponentName} {
			results = append(results, store.SnapResourceResult{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: "http://example.com/" + name,
				},
				Name:     name,
				Revision: 3,
				Type:     "component/" + typeMapping[name],
				Version:  "1.0",
			})
		}
		return results
	}

	tss, err := snapstate.InstallComponents(context.TODO(), s.state, []string{compName}, info, nil, snapstate.Options{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("install-component", "install a component")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	_, err = snapstate.InstallComponents(context.TODO(), s.state, []string{conflictComponentName}, info, nil, snapstate.Options{})
	c.Assert(err.Error(), Equals, `snap "some-snap" has "install-component" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallComponentCausesConflict(c *C) {
	const (
		snapName = "some-snap"
		compName = "standard-component"
	)

	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		return []store.SnapResourceResult{{
			DownloadInfo: snap.DownloadInfo{
				DownloadURL: "http://example.com/" + compName,
			},
			Name:      compName,
			Revision:  3,
			Type:      "component/standard",
			Version:   "1.0",
			CreatedAt: "2024-01-01T00:00:00Z",
		}}
	}

	tss, err := snapstate.InstallComponents(context.TODO(), s.state, []string{compName}, info, nil, snapstate.Options{})
	c.Assert(err, IsNil)
	chg := s.state.NewChange("install-component", "install a component")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	_, err = snapstate.Update(s.state, snapName, nil, s.user.ID, snapstate.Flags{})
	c.Assert(err.Error(), Equals, `snap "some-snap" has "install-component" change in progress`)
}

func (s *snapmgrTestSuite) TestInstallKernelModulesComponentPath(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponents(c, snapName, snapRev, map[string]string{compName: "kernel-modules"})
	_, compPath := createTestComponentWithType(c, snapName, compName, "kernel-modules", info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Options{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compTypeIsKernMods, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.CanStat(compPath), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallComponentPathCompRevisionPresentInTwoSeqPts(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	_, compPath := createTestComponent(c, snapName, compName, info)

	s.state.Lock()
	defer s.state.Unlock()

	// Current component is present in current and in another sequence point
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snap.R(10),
		SnapID: "some-snap-id"}
	currentCsi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(3))
	compsSi := []*sequence.ComponentState{
		sequence.NewComponentState(currentCsi, snap.StandardComponent),
	}
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, compsSi),
				sequence.NewRevisionSideState(ssi2, compsSi),
			}),
		Current: snapRev,
	}
	snapstate.Set(s.state, snapName, snapst)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName: snapName, ComponentName: compName}, compRev)
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Options{})
	c.Assert(err, IsNil)

	verifyComponentInstallTasks(c, compOptIsLocal|compOptIsActive, ts)
	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.CanStat(compPath), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallComponentPathRun(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, compPath := createTestComponent(c, snapName, compName, info)
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, snap.R(33))
	ts, err := snapstate.InstallComponentPath(s.state, csi, info, compPath,
		snapstate.Options{})
	c.Assert(err, IsNil)

	c.Assert(s.state.TaskCount(), Equals, len(ts.Tasks()))
	// File is not deleted
	c.Assert(osutil.CanStat(compPath), Equals, true)

	chg := s.state.NewChange("install component", "...")
	chg.AddAll(ts)

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	verifyComponentInstallTasks(c, compOptIsLocal, ts)

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)

	c.Assert(snapst.IsComponentInCurrentSeq(cref), Equals, true)
}

func (s *snapmgrTestSuite) TestInstallComponents(c *C) {
	s.testInstallComponents(c, testInstallComponentsOpts{})
}

func (s *snapmgrTestSuite) TestInstallComponentsWithLane(c *C) {
	s.testInstallComponents(c, testInstallComponentsOpts{
		lane:        1,
		transaction: client.TransactionAllSnaps,
	})
}

func (s *snapmgrTestSuite) TestInstallComponentsTransactionAllSnaps(c *C) {
	s.testInstallComponents(c, testInstallComponentsOpts{
		transaction: client.TransactionAllSnaps,
	})
}

func (s *snapmgrTestSuite) TestInstallComponentsTransactionPerSnap(c *C) {
	s.testInstallComponents(c, testInstallComponentsOpts{
		transaction: client.TransactionPerSnap,
	})
}

type testInstallComponentsOpts struct {
	lane        int
	transaction client.TransactionType
}

func (s *snapmgrTestSuite) testInstallComponents(c *C, opts testInstallComponentsOpts) {
	const snapName = "some-snap"
	snapRev := snap.R(1)

	compNamesToType := map[string]string{
		"one": "test",
		"two": "test",
	}

	info := createTestSnapInfoForComponents(c, snapName, snapRev, compNamesToType)

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snapRev,
		SnapID:   "some-snap-id",
	}

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(si, nil),
		}),
		Current:         snapRev,
		TrackingChannel: "channel-for-components",
	})

	components := []string{"standard-component", "kernel-modules-component"}

	compNameToType := func(name string) snap.ComponentType {
		typ := strings.TrimSuffix(name, "-component")
		if typ == name {
			c.Fatalf("unexpected component name %q", name)
		}
		return snap.ComponentType(typ)
	}

	s.fakeStore.snapResourcesFn = func(info *snap.Info) []store.SnapResourceResult {
		c.Assert(info.InstanceName(), DeepEquals, snapName)
		var results []store.SnapResourceResult
		for _, compName := range components {
			results = append(results, store.SnapResourceResult{
				DownloadInfo: snap.DownloadInfo{
					DownloadURL: "http://example.com/" + compName,
				},
				Name:      compName,
				Revision:  snap.R(3).N,
				Type:      fmt.Sprintf("component/%s", compNameToType(compName)),
				Version:   "1.0",
				CreatedAt: "2024-01-01T00:00:00Z",
			})
		}
		return results
	}

	installOpts := snapstate.Options{
		Flags: snapstate.Flags{
			Lane:        opts.lane,
			Transaction: opts.transaction,
		},
	}

	tss, err := snapstate.InstallComponents(context.Background(), s.state, components, info, nil, installOpts)
	c.Assert(err, IsNil)

	setupTs := tss[len(tss)-1]

	setupProfiles := setupTs.Tasks()[0]
	c.Assert(setupProfiles.Kind(), Equals, "setup-profiles")

	prepareKmodComps := setupTs.Tasks()[1]
	c.Assert(prepareKmodComps.Kind(), Equals, "prepare-kernel-modules-components")

	snapsupTask, err := setupTs.Edge(snapstate.SnapSetupEdge)
	c.Assert(err, IsNil)
	c.Assert(snapsupTask.Kind(), Equals, "setup-profiles")
	c.Assert(snapsupTask.Has("component-setup-tasks"), Equals, true)

	expectedLane := opts.lane
	if opts.transaction != "" && opts.lane == 0 {
		expectedLane = 1
	}

	// add to change so that we can use TaskComponentSetup
	chg := s.state.NewChange("install", "...")
	for _, ts := range tss {
		chg.AddAll(ts)

		for _, t := range ts.Tasks() {
			c.Assert(t.Lanes(), DeepEquals, []int{expectedLane})
		}
	}

	snapsup, err := snapstate.TaskSnapSetup(prepareKmodComps)
	c.Assert(err, IsNil)
	c.Assert(snapsup, NotNil)
	c.Assert(snapsup.ComponentExclusiveOperation, Equals, true)

	for _, ts := range tss[0 : len(tss)-1] {
		task := ts.Tasks()[0]
		compsup, snapsup, err := snapstate.TaskComponentSetup(task)
		c.Assert(err, IsNil)
		c.Assert(compsup, NotNil)
		c.Assert(snapsup, NotNil)

		opts := compOptMultiCompInstall
		if compNameToType(compsup.ComponentName()) == snap.KernelModulesComponent {
			opts |= compTypeIsKernMods
		}

		verifyComponentInstallTasks(c, opts, ts)

		linkTasks := tasksWithKind(ts, "link-component")
		c.Assert(linkTasks, HasLen, 1)

		// make sure that the link-component tasks wait on the all-inclusive
		// setup-profiles task
		c.Assert(linkTasks[0].WaitTasks(), testutil.DeepContains, setupProfiles)

		installHook := tasksWithKind(ts, "run-hook")
		c.Assert(installHook, HasLen, 1)

		// make sure that the run-hook[install] tasks wait on the all-inclusive
		// prepare-kernel-modules-components task
		c.Assert(prepareKmodComps.WaitTasks(), testutil.DeepContains, installHook[0])
	}
}

func (s *snapmgrTestSuite) TestInstallComponentsAlreadyInstalledError(c *C) {
	const snapName = "some-snap"
	snapRev := snap.R(1)

	compNamesToType := map[string]string{
		"one": "test",
		"two": "test",
	}

	info := createTestSnapInfoForComponents(c, snapName, snapRev, compNamesToType)

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		RealName: snapName,
		Revision: snapRev,
		SnapID:   "some-snap-id",
	}

	seq := snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{
		sequence.NewRevisionSideState(si, nil),
	})

	seq.AddComponentForRevision(snapRev, sequence.NewComponentState(&snap.ComponentSideInfo{
		Component: naming.NewComponentRef(snapName, "one"),
		Revision:  snap.R(1),
	}, snap.StandardComponent))

	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active:          true,
		Sequence:        seq,
		Current:         snapRev,
		TrackingChannel: "channel-for-components",
	})

	_, err := snapstate.InstallComponents(context.TODO(), s.state, []string{"one", "two"}, info, nil, snapstate.Options{})

	c.Assert(err, testutil.ErrorIs, snap.AlreadyInstalledComponentError{Component: "one"})
}

func (s *snapmgrTestSuite) TestInstallComponentsInvalidFlagAndTransaction(c *C) {
	const snapName = "some-snap"
	snapRev := snap.R(1)
	compNamesToType := map[string]string{
		"one": "standard",
		"two": "standard",
	}

	info := createTestSnapInfoForComponents(c, snapName, snapRev, compNamesToType)

	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.InstallComponents(context.TODO(), s.state, []string{"one", "two"}, info, nil, snapstate.Options{
		Flags: snapstate.Flags{Lane: 1},
	})
	c.Assert(err, ErrorMatches, `cannot specify a lane without setting transaction to "all-snaps"`)
}

func (s *snapmgrTestSuite) TestInstallComponentPathInvalidFlagAndTransaction(c *C) {
	const snapName = "some-snap"
	snapRev := snap.R(1)
	compNamesToType := map[string]string{
		"one": "standard",
	}

	info := createTestSnapInfoForComponents(c, snapName, snapRev, compNamesToType)
	_, compPath := createTestComponentWithType(c, snapName, "one", "standard", info)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName:      snapName,
		ComponentName: "one",
	}, snap.R(33))

	_, err := snapstate.InstallComponentPath(s.state, csi, info, compPath, snapstate.Options{
		Flags: snapstate.Flags{Lane: 1},
	})
	c.Assert(err, ErrorMatches, `cannot specify a lane without setting transaction to "all-snaps"`)
}

func (s *snapmgrTestSuite) TestInstallComponentsTooEarly(c *C) {
	const snapName = "some-snap"
	snapRev := snap.R(1)
	compNamesToType := map[string]string{
		"one": "standard",
		"two": "standard",
	}

	info := createTestSnapInfoForComponents(c, snapName, snapRev, compNamesToType)

	restore := snapstatetest.MockDeviceModel(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	_, err := snapstate.InstallComponents(context.TODO(), s.state, []string{"one", "two"}, info, nil, snapstate.Options{
		Seed: true,
	})
	c.Assert(err, ErrorMatches, `.*too early for operation, device model not yet acknowledged`)
}

func (s *snapmgrTestSuite) TestInstallComponentPathTooEarly(c *C) {
	const snapName = "some-snap"
	snapRev := snap.R(1)
	compNamesToType := map[string]string{
		"one": "standard",
	}

	info := createTestSnapInfoForComponents(c, snapName, snapRev, compNamesToType)
	_, compPath := createTestComponentWithType(c, snapName, "one", "standard", info)

	restore := snapstatetest.MockDeviceModel(nil)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	csi := snap.NewComponentSideInfo(naming.ComponentRef{
		SnapName:      snapName,
		ComponentName: "one",
	}, snap.R(33))

	_, err := snapstate.InstallComponentPath(s.state, csi, info, compPath, snapstate.Options{
		Seed: true,
	})
	c.Assert(err, ErrorMatches, `.*too early for operation, device model not yet acknowledged`)
}
