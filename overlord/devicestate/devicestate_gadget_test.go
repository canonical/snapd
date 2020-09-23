// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
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

var uc20gadgetYaml = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed
        type: 21686148-6449-6E6F-744E-656564454649
        size: 20M
      - name: ubuntu-boot
        role: system-boot
        type: 21686148-6449-6E6F-744E-656564454649
        size: 10M
      - name: ubuntu-data
        role: system-data
        type: 21686148-6449-6E6F-744E-656564454649
        size: 50M
`

func (s *deviceMgrGadgetSuite) setupModelWithGadget(c *C, gadget string) {
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       gadget,
		"base":         "core18",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "serial",
	})
}

func (s *deviceMgrGadgetSuite) setupUC20ModelWithGadget(c *C, gadget string) {
	s.makeModelAssertionInState(c, "canonical", "pc20-model", map[string]interface{}{
		"display-name": "UC20 pc model",
		"architecture": "amd64",
		"base":         "core20",
		// enough to have a grade set
		"grade": "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              "pckernelidididididididididididid",
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            gadget,
				"id":              "pcididididididididididididididid",
				"type":            "gadget",
				"default-channel": "20",
			}},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc20-model",
		Serial: "serial",
	})
}

func (s *deviceMgrGadgetSuite) setupGadgetUpdate(c *C, modelGrade string) (chg *state.Change, tsk *state.Task) {
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
	gadgetYamlContent := gadgetYaml
	if modelGrade != "" {
		gadgetYamlContent = uc20gadgetYaml
	}
	snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, [][]string{
		{"meta/gadget.yaml", gadgetYamlContent},
	})
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYamlContent},
	})

	s.state.Lock()
	defer s.state.Unlock()

	if modelGrade == "" {
		s.setupModelWithGadget(c, "foo-gadget")
	} else {
		s.setupUC20ModelWithGadget(c, "foo-gadget")
	}

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	tsk = s.state.NewTask("update-gadget-assets", "update gadget")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg = s.state.NewChange("dummy", "...")
	chg.AddTask(tsk)

	return chg, tsk
}

func (s *deviceMgrGadgetSuite) testUpdateGadgetOnCoreSimple(c *C, grade string) {
	var updateCalled bool
	var passedRollbackDir string
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, observer gadget.ContentUpdateObserver) error {
		updateCalled = true
		passedRollbackDir = path
		st, err := os.Stat(path)
		c.Assert(err, IsNil)
		m := st.Mode()
		c.Assert(m.IsDir(), Equals, true)
		c.Check(m.Perm(), Equals, os.FileMode(0750))
		if grade == "" {
			// non UC20 model
			c.Check(observer, IsNil)
		} else {
			c.Check(observer, NotNil)
			// expecting a very specific observer
			trustedUpdateObserver, ok := observer.(*boot.TrustedAssetsUpdateObserver)
			c.Assert(ok, Equals, true, Commentf("unexpected type: %T", observer))
			c.Assert(trustedUpdateObserver, NotNil)
		}
		return nil
	})
	defer restore()

	chg, t := s.setupGadgetUpdate(c, grade)

	// procure modeenv and stamp that we sealed keys
	if grade != "" {
		// state after mark-seeded ran
		modeenv := boot.Modeenv{
			Mode:           "run",
			RecoverySystem: "",
		}
		err := modeenv.WriteTo("")
		c.Assert(err, IsNil)

		// sealed keys stamp
		stamp := filepath.Join(dirs.SnapFDEDir, "sealed-keys")
		c.Assert(os.MkdirAll(filepath.Dir(stamp), 0755), IsNil)
		err = ioutil.WriteFile(stamp, nil, 0644)
		c.Assert(err, IsNil)
	}
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	s.settle(c)

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

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreSimple(c *C) {
	// unset grade
	s.testUpdateGadgetOnCoreSimple(c, "")
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnUC20CoreSimple(c *C) {
	s.testUpdateGadgetOnCoreSimple(c, "dangerous")
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreNoUpdateNeeded(c *C) {
	var called bool
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		called = true
		return gadget.ErrNoUpdate
	})
	defer restore()

	chg, t := s.setupGadgetUpdate(c, "")

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

	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("unexpected call")
	})
	defer restore()

	chg, t := s.setupGadgetUpdate(c, "")

	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	err := os.MkdirAll(dirs.SnapRollbackDir, 0000)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot prepare update rollback directory: .*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreUpdateFailed(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("gadget exploded")
	})
	defer restore()
	chg, t := s.setupGadgetUpdate(c, "")

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	s.settle(c)

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
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
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
	s.state.Set("seeded", true)

	s.setupModelWithGadget(c, "foo-gadget")

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.settle(c)

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
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
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
	s.state.Set("seeded", true)

	s.setupModelWithGadget(c, "foo-gadget")

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

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, `(?s).*update gadget \(cannot read candidate snap gadget metadata: .*\).*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget")
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreParanoidChecks(c *C) {
	restore := devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("unexpected call")
	})
	defer restore()
	siCurrent := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	si := &snap.SideInfo{
		RealName: "foo-gadget-unexpected",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}

	s.state.Lock()

	s.state.Set("seeded", true)

	s.setupModelWithGadget(c, "foo-gadget")

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

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.IsReady(), Equals, true)
	c.Assert(chg.Err(), ErrorMatches, `(?s).*\(cannot apply gadget assets update from non-model gadget snap "foo-gadget-unexpected", expected "foo-gadget" snap\)`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnClassicErrorsOut(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	restore = devicestate.MockGadgetUpdate(func(current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("unexpected call")
	})
	defer restore()

	s.state.Lock()

	s.state.Set("seeded", true)

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	chg := s.state.NewChange("dummy", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.settle(c)

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
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, rootDir, rollbackDir string, _ gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updaterForStructureCalls++

		c.Assert(ps.Name, Equals, "foo")
		c.Assert(rootDir, Equals, updateInfo.MountDir())
		c.Assert(filepath.Join(rootDir, "content.img"), testutil.FileEquals, "updated content")
		c.Assert(strings.HasPrefix(rollbackDir, expectedRollbackDir), Equals, true)
		c.Assert(osutil.IsDirectory(rollbackDir), Equals, true)
		return &mockUpdater{}, nil
	})
	defer restore()

	s.state.Lock()
	s.state.Set("seeded", true)

	s.setupModelWithGadget(c, "foo-gadget")

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

	s.settle(c)

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

	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "foo-gadget",
		"base":         "core18",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	current, err := devicestate.CurrentGadgetInfo(s.state, deviceCtx)
	c.Assert(current, IsNil)
	c.Check(err, IsNil)

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{siCurrent},
		Current:  siCurrent.Revision,
		Active:   true,
	})

	// mock current first, but gadget.yaml is still missing
	ci := snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, nil)

	current, err = devicestate.CurrentGadgetInfo(s.state, deviceCtx)

	c.Assert(current, IsNil)
	c.Assert(err, ErrorMatches, "cannot read current gadget snap details: .*/33/meta/gadget.yaml: no such file or directory")

	// drop gadget.yaml for current snap
	ioutil.WriteFile(filepath.Join(ci.MountDir(), "meta/gadget.yaml"), []byte(gadgetYaml), 0644)

	current, err = devicestate.CurrentGadgetInfo(s.state, deviceCtx)
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

	// pending update
	update, err := devicestate.PendingGadgetInfo(snapsup, deviceCtx)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read candidate gadget snap details: cannot find installed snap .* .*/34/meta/snap.yaml")

	ui := snaptest.MockSnapWithFiles(c, snapYaml, si, nil)

	update, err = devicestate.PendingGadgetInfo(snapsup, deviceCtx)
	c.Assert(update, IsNil)
	c.Assert(err, ErrorMatches, "cannot read candidate snap gadget metadata: .*/34/meta/gadget.yaml: no such file or directory")

	var updateGadgetYaml = `
volumes:
  pc:
    bootloader: grub
    id: 123
`

	// drop gadget.yaml for update snap
	ioutil.WriteFile(filepath.Join(ui.MountDir(), "meta/gadget.yaml"), []byte(updateGadgetYaml), 0644)

	update, err = devicestate.PendingGadgetInfo(snapsup, deviceCtx)
	c.Assert(err, IsNil)
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
