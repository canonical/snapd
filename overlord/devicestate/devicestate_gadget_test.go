// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package devicestate_test

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type deviceMgrGadgetSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrGadgetSuite{})

var snapYaml = `
name: foo-gadget
type: gadget
`

var gadgetYaml = `
volumes:
  pc:
    bootloader: grub
`

func setupGadgetUpdate(c *C, st *state.State) (chg *state.Change, tsk *state.Task) {
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	st.Lock()

	snapstate.Set(st, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	tsk = st.NewTask("update-gadget-assets", "update gadget")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg = st.NewChange("dummy", "...")
	chg.AddTask(tsk)

	st.Unlock()

	return chg, tsk
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreSimple(c *C) {
	var updateCalled bool
	var passedRollbackDir string
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error {
		updateCalled = true
		passedRollbackDir = path
		st, err := os.Stat(path)
		c.Assert(err, IsNil)
		m := st.Mode()
		c.Assert(m.IsDir(), Equals, true)
		c.Check(m.Perm(), Equals, os.FileMode(0750))
		return nil
	})
	defer restore()

	chg, t := setupGadgetUpdate(c, s.state)

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(updateCalled, Equals, true)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	c.Check(rollbackDir, Equals, passedRollbackDir)
	// should have been removed right after update
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystem})
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreNoUpdateNeeded(c *C) {
	var called bool
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error {
		called = true
		return gadget.ErrNoUpdate
	})
	defer restore()

	chg, t := setupGadgetUpdate(c, s.state)

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(t.Log(), HasLen, 1)
	c.Check(t.Log()[0], Matches, ".* INFO No gadget assets update needed")
	c.Check(called, Equals, true)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreRollbackDirCreateFailed(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root (permissions are not honored)")
	}

	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error {
		return errors.New("unexpected call")
	})
	defer restore()

	chg, t := setupGadgetUpdate(c, s.state)

	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	err := os.MkdirAll(dirs.SnapRollbackDir, 0000)
	c.Assert(err, IsNil)

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot prepare update rollback directory: .*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreUpdateFailed(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error {
		return errors.New("gadget exploded")
	})
	defer restore()
	chg, t := setupGadgetUpdate(c, s.state)

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*update gadget \(gadget exploded\).*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	// update rollback left for inspection
	c.Check(osutil.IsDirectory(rollbackDir), Equals, true)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreNotDuringFirstboot(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error {
		return errors.New("unexpected call")
	})
	defer restore()

	// simulate first-boot/seeding, there is no existing snap state information

	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})

	s.state.Lock()

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget")
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreBadGadgetYaml(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error {
		return errors.New("unexpected call")
	})
	defer restore()
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	// invalid gadget.yaml data
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", "foobar"},
	})

	s.state.Lock()

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*update gadget \(cannot read candidate gadget snap details: .*\).*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget")
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnClassicErrorsOut(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	restore = devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc) error {
		return errors.New("unexpected call")
	})
	defer restore()

	s.state.Lock()

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	// we cannot use "s.o.Settle()" here because this change has an
	// error which means that the settle will never converge
	for i := 0; i < 50; i++ {
		s.se.Ensure()
		s.se.Wait()

		s.state.Lock()
		ready := chg.IsReady()
		s.state.Unlock()
		if ready {
			break
		}
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*update gadget \(cannot run update gadget assets task on a classic system\).*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
}

type mockUpdater struct{}

func (m *mockUpdater) Backup() error { return nil }

func (m *mockUpdater) Rollback() error { return nil }

func (m *mockUpdater) Update() error { return nil }

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCallsToGadget(c *C) {
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	var gadgetCurrentYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
       - name: foo
         size: 10M
         type: bare
         content:
            - image: content.img
`
	var gadgetUpdateYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
       - name: foo
         size: 10M
         type: bare
         content:
            - image: content.img
         update:
           edition: 2
`
	snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, [][]string{
		{"meta/gadget.yaml", gadgetCurrentYaml},
		{"content.img", "some content"},
	})
	updateInfo := snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetUpdateYaml},
		{"content.img", "updated content"},
	})

	expectedRollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	updaterForStructureCalls := 0
	gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, rootDir, rollbackDir string) (gadget.Updater, error) {
		updaterForStructureCalls++

		c.Assert(ps.Name, Equals, "foo")
		c.Assert(rootDir, Equals, updateInfo.MountDir())
		c.Assert(filepath.Join(rootDir, "content.img"), testutil.FileEquals, "updated content")
		c.Assert(strings.HasPrefix(rollbackDir, expectedRollbackDir), Equals, true)
		c.Assert(osutil.IsDirectory(rollbackDir), Equals, true)
		return &mockUpdater{}, nil
	})

	s.state.Lock()

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 6; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(s.restartRequests, HasLen, 1)
	c.Check(updaterForStructureCalls, Equals, 1)
}

func (s *deviceMgrGadgetSuite) TestCurrentAndUpdateInfo(c *C) {
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}

	s.state.Lock()
	defer s.state.Unlock()

	snapsup := &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	}

	current, update, err := devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, IsNil)

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	// mock current first, but gadget.yaml is still missing
	ci := snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, nil)

	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read current gadget snap details: .*/33/meta/gadget.yaml: no such file or directory")

	// drop gadget.yaml for current snap
	ioutil.WriteFile(filepath.Join(ci.MountDir(), "meta/gadget.yaml"), []byte(gadgetYaml), 0644)

	// update missing snap.yaml
	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read candidate gadget snap details: cannot find installed snap .* .*/34/meta/snap.yaml")

	ui := snaptest.MockSnapWithFiles(c, snapYaml, si, nil)

	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(current, IsNil)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read candidate gadget snap details: .*/34/meta/gadget.yaml: no such file or directory")

	var updateGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    id: 123
`

	// drop gadget.yaml for update snap
	ioutil.WriteFile(filepath.Join(ui.MountDir(), "meta/gadget.yaml"), []byte(updateGadgetYaml), 0644)

	current, update, err = devicestate.GadgetCurrentAndUpdate(s.state, snapsup)
	c.Assert(err, IsNil)
	c.Assert(current, DeepEquals, &gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]gadget.Volume{
				"pc": {
					Bootloader: "grub",
				},
			},
		},
		RootDir: ci.MountDir(),
	})
	c.Assert(update, DeepEquals, &gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]gadget.Volume{
				"pc": {
					Bootloader: "grub",
					ID:         "123",
				},
			},
		},
		RootDir: ui.MountDir(),
	})
}

func (s *deviceMgrGadgetSuite) TestGadgetUpdateBlocksWhenOtherTasks(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tUpdate := s.state.NewTask("update-gadget-assets", "update gadget")
	t1 := s.state.NewTask("other-task-1", "other 1")
	t2 := s.state.NewTask("other-task-2", "other 2")

	// no other running tasks, does not block
	c.Assert(devicestate.GadgetUpdateBlocked(tUpdate, nil), Equals, false)

	// list of running tasks actually contains ones that are in the 'running' state
	t1.SetStatus(state.DoingStatus)
	t2.SetStatus(state.UndoingStatus)
	// block on any other running tasks
	c.Assert(devicestate.GadgetUpdateBlocked(tUpdate, []*state.Task{t1, t2}), Equals, true)
}

func (s *deviceMgrGadgetSuite) TestGadgetUpdateBlocksOtherTasks(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tUpdate := s.state.NewTask("update-gadget-assets", "update gadget")
	tUpdate.SetStatus(state.DoingStatus)
	t1 := s.state.NewTask("other-task-1", "other 1")
	t2 := s.state.NewTask("other-task-2", "other 2")

	// block on any other running tasks
	c.Assert(devicestate.GadgetUpdateBlocked(t1, []*state.Task{tUpdate}), Equals, true)
	c.Assert(devicestate.GadgetUpdateBlocked(t2, []*state.Task{tUpdate}), Equals, true)

	t2.SetStatus(state.UndoingStatus)
	// update-gadget should be the only running task, for the sake of
	// completeness pretend it's one of many running tasks
	c.Assert(devicestate.GadgetUpdateBlocked(t1, []*state.Task{tUpdate, t2}), Equals, true)

	// not blocking without gadget update task
	c.Assert(devicestate.GadgetUpdateBlocked(t1, []*state.Task{t2}), Equals, false)
}
