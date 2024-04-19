// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

type deviceMgrGadgetSuite struct {
	deviceMgrBaseSuite

	managedbl *bootloadertest.MockTrustedAssetsBootloader
}

var _ = Suite(&deviceMgrGadgetSuite{})

const pcGadgetSnapYaml = `
name: pc
type: gadget
`

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

var uc20gadgetYamlWithSave = uc20gadgetYaml + `
      - name: ubuntu-save
        role: system-save
        type: 21686148-6449-6E6F-744E-656564454649
        size: 50M
`

// this is the kind of volumes setup recommended to be prepared for a possible
// UC18 -> UC20 transition
var hybridGadgetYaml = `
volumes:
  hybrid:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: pc-core.img
      - name: EFI System
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        filesystem-label: system-boot
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
          - source: mmx64.efi
            target: EFI/boot/mmx64.efi
          - source: grub.cfg
            target: EFI/ubuntu/grub.cfg
      - name: Ubuntu Boot
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        filesystem: ext4
        filesystem-label: ubuntu-boot
        size: 750M
`

func (s *deviceMgrGadgetSuite) SetUpTest(c *C) {
	classic := false
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)

	s.managedbl = bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	s.managedbl.StaticCommandLine = "console=ttyS0 console=tty1 panic=-1"
	s.managedbl.CandidateStaticCommandLine = "console=ttyS0 console=tty1 panic=-1 candidate"

	s.state.Lock()
	defer s.state.Unlock()
}

func (s *deviceMgrGadgetSuite) mockModeenvForMode(c *C, mode string) {
	// mock minimal modeenv
	modeenv := boot.Modeenv{
		Mode:           mode,
		RecoverySystem: "",
		CurrentKernelCommandLines: []string{
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		},
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)
}

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

func (s *deviceMgrGadgetSuite) setupUC20ModelWithGadget(c *C, gadget, grade string) {
	s.makeModelAssertionInState(c, "canonical", "pc20-model", map[string]interface{}{
		"display-name": "UC20 pc model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        grade,
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

func (s *deviceMgrGadgetSuite) setupClassicWithModesModel(c *C, gadget string) *asserts.Model {
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "classic-with-modes",
		Serial: "didididi",
	})
	return s.makeModelAssertionInState(c, "canonical", "classic-with-modes",
		map[string]interface{}{
			"architecture": "amd64",
			"classic":      "true",
			"distribution": "ubuntu",
			"base":         "core22",
			"snaps": []interface{}{
				map[string]interface{}{
					"name": "pc-linux",
					"id":   "pclinuxdidididididididididididid",
					"type": "kernel",
				},
				map[string]interface{}{
					"name": gadget,
					"id":   "pcididididididididididididididid",
					"type": "gadget",
				},
			},
		})
}

func (s *deviceMgrGadgetSuite) setupGadgetUpdate(c *C, modelGrade, gadgetYamlContent, gadgetYamlContentNext string, isClassic bool) (chg *state.Change, tsk *state.Task) {
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
		{"meta/gadget.yaml", gadgetYamlContent},
		{"managed-asset", "managed asset rev 33"},
		{"trusted-asset", "trusted asset rev 33"},
	})
	if gadgetYamlContentNext == "" {
		gadgetYamlContentNext = gadgetYamlContent
	}
	snaptest.MockSnapWithFiles(c, snapYaml, si, [][]string{
		{"meta/gadget.yaml", gadgetYamlContentNext},
		{"managed-asset", "managed asset rev 34"},
		// SHA3-384: 88478d8afe6925b348b9cd00085f3535959fde7029a64d7841b031acc39415c690796757afab1852a9e09da913a0151b
		{"trusted-asset", "trusted asset rev 34"},
	})

	s.state.Lock()
	defer s.state.Unlock()

	if isClassic {
		s.setupClassicWithModesModel(c, "foo-gadget")
	} else if modelGrade == "" {
		s.setupModelWithGadget(c, "foo-gadget")
	} else {
		s.setupUC20ModelWithGadget(c, "foo-gadget", "dangerous")
	}

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCurrent}),
		Current:  siCurrent.Revision,
		Active:   true,
	})

	tsk = s.state.NewTask("update-gadget-assets", "update gadget")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg = s.state.NewChange("sample", "...")
	chg.AddTask(tsk)

	return chg, tsk
}

func (s *deviceMgrGadgetSuite) testUpdateGadgetSimple(c *C, grade string, encryption, immediate bool, gadgetYamlCont, gadgetYamlContNext string, isClassic bool) {
	var updateCalled bool
	var passedRollbackDir string

	defer boot.MockSetEfiBootVariables(func(description string, assetPath string, optionalData []byte) error {
		c.Check(description, Equals, "ubuntu-test")
		return nil
	})()

	if grade != "" {
		bootDir := c.MkDir()
		tbl := bootloadertest.Mock("trusted", bootDir).WithTrustedAssets()
		tbl.TrustedAssetsMap = map[string]string{"trusted-asset": "trusted-asset"}
		tbl.ManagedAssetsList = []string{"managed-asset"}
		tbl.EfiLoadOptionDesc = "ubuntu-test"
		tbl.EfiLoadOptionPath = "/some/path"
		tbl.EfiLoadOptionData = nil
		bootloader.Force(tbl)
		defer func() { bootloader.Force(nil) }()
	}

	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, observer gadget.ContentUpdateObserver) error {
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

			// check that observer is behaving correctly with
			// respect to trusted and managed assets
			targetDir := c.MkDir()
			act, err := observer.Observe(gadget.ContentUpdate, gadget.SystemSeed, targetDir, "managed-asset",
				&gadget.ContentChange{After: filepath.Join(update.RootDir, "managed-asset")})
			c.Assert(err, IsNil)
			c.Check(act, Equals, gadget.ChangeIgnore)
			act, err = observer.Observe(gadget.ContentUpdate, gadget.SystemSeed, targetDir, "trusted-asset",
				&gadget.ContentChange{After: filepath.Join(update.RootDir, "trusted-asset")})
			c.Assert(err, IsNil)
			c.Check(act, Equals, gadget.ChangeApply)
			// check that the behavior is correct
			m, err := boot.ReadModeenv("")
			c.Assert(err, IsNil)
			c.Assert(m.CurrentTrustedRecoveryBootAssets, NotNil)
			c.Check(m.CurrentTrustedRecoveryBootAssets["trusted-asset"], DeepEquals,
				[]string{"88478d8afe6925b348b9cd00085f3535959fde7029a64d7841b031acc39415c690796757afab1852a9e09da913a0151b"})
		}
		return nil
	})
	defer restore()

	chg, t := s.setupGadgetUpdate(c, grade, gadgetYamlCont, gadgetYamlContNext, isClassic)

	// procure modeenv and stamp that we sealed keys
	if grade != "" {
		// state after mark-seeded ran
		modeenv := boot.Modeenv{
			Mode:           "run",
			RecoverySystem: "",
		}
		err := modeenv.WriteTo("")
		c.Assert(err, IsNil)

		if encryption {
			// sealed keys stamp
			stamp := filepath.Join(dirs.SnapFDEDir, "sealed-keys")
			c.Assert(os.MkdirAll(filepath.Dir(stamp), 0755), IsNil)
			err = os.WriteFile(stamp, nil, 0644)
			c.Assert(err, IsNil)
		}
	}
	devicestate.SetBootOkRan(s.mgr, true)

	expectedRst := restart.RestartSystem
	s.state.Lock()
	s.state.Set("seeded", true)
	if immediate {
		expectedRst = restart.RestartSystemNow
		chg.Set("system-restart-immediate", true)
	}
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that "gadget-restart-required" was set
	var restartRequired bool
	c.Check(chg.Get("gadget-restart-required", &restartRequired), IsNil)
	c.Check(restartRequired, Equals, true)

	// Expect the change to be in wait status at this point, as a restart
	// will have been requested
	c.Check(t.Status(), Equals, state.WaitStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
	// Restart and re-run to completion
	s.mockRestartAndSettle(c, s.state, chg)

	c.Check(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(updateCalled, Equals, true)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	c.Check(rollbackDir, Equals, passedRollbackDir)
	// should have been removed right after update
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	if isClassic {
		c.Check(s.restartRequests, HasLen, 0)
	} else {
		c.Check(s.restartRequests, DeepEquals, []restart.RestartType{expectedRst})
	}
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreSimple(c *C) {
	// unset grade
	encryption := false
	immediate := false
	isClassic := false
	s.testUpdateGadgetSimple(c, "", encryption, immediate, gadgetYaml, "", isClassic)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnClassicWithModesSimple(c *C) {
	r := release.MockOnClassic(true)
	defer r()
	encryption := false
	immediate := false
	isClassic := true
	s.testUpdateGadgetSimple(c, "dangerous", encryption, immediate, gadgetYaml, "", isClassic)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnUC20CoreSimpleWithEncryption(c *C) {
	encryption := true
	immediate := false
	isClassic := false
	s.testUpdateGadgetSimple(c, "dangerous", encryption, immediate, uc20gadgetYaml, "", isClassic)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnUC20CoreSimpleNoEncryption(c *C) {
	encryption := false
	immediate := false
	isClassic := false
	s.testUpdateGadgetSimple(c, "dangerous", encryption, immediate, uc20gadgetYaml, "", isClassic)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnUC20CoreSimpleSystemRestartImmediate(c *C) {
	encryption := false
	immediate := true
	isClassic := false
	s.testUpdateGadgetSimple(c, "dangerous", encryption, immediate, uc20gadgetYaml, "", isClassic)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreNoUpdateNeeded(c *C) {
	var called bool
	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		called = true
		return gadget.ErrNoUpdate
	})
	defer restore()

	isClassic := false
	chg, t := s.setupGadgetUpdate(c, "", gadgetYaml, "", isClassic)

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

	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("unexpected call")
	})
	defer restore()

	isClassic := false
	chg, t := s.setupGadgetUpdate(c, "", gadgetYaml, "", isClassic)

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
	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
		return errors.New("gadget exploded")
	})
	defer restore()
	isClassic := false
	chg, t := s.setupGadgetUpdate(c, "", gadgetYaml, "", isClassic)

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
	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
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
	chg := s.state.NewChange("sample", "...")
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
	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCurrent}),
		Current:  siCurrent.Revision,
		Active:   true,
	})

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("sample", "...")
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
	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCurrent}),
		Current:  siCurrent.Revision,
		Active:   true,
	})

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("sample", "...")
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

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
			}, nil
	})
	defer r()

	expectedRollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget_34")
	updaterForStructureCalls := 0
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, fromPs, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, _ gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updaterForStructureCalls++

		c.Assert(loc, Equals, gadget.StructureLocation{
			Device:         "/dev/foo",
			Offset:         quantity.OffsetMiB,
			RootMountPoint: "",
		})

		c.Assert(ps.Name(), Equals, "foo")
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCurrent}),
		Current:  siCurrent.Revision,
		Active:   true,
	})

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// simulate restart and settle again
	s.mockRestartAndSettle(c, s.state, chg)

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

	current, err := devicestate.CurrentGadgetData(s.state, deviceCtx)
	c.Assert(current, IsNil)
	c.Check(err, IsNil)

	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCurrent}),
		Current:  siCurrent.Revision,
		Active:   true,
	})

	// mock current first, but gadget.yaml is still missing
	ci := snaptest.MockSnapWithFiles(c, snapYaml, siCurrent, nil)

	current, err = devicestate.CurrentGadgetData(s.state, deviceCtx)

	c.Assert(current, IsNil)
	c.Assert(err, ErrorMatches, "cannot read current gadget snap details: .*/33/meta/gadget.yaml: no such file or directory")

	// drop gadget.yaml for current snap
	os.WriteFile(filepath.Join(ci.MountDir(), "meta/gadget.yaml"), []byte(gadgetYaml), 0644)

	current, err = devicestate.CurrentGadgetData(s.state, deviceCtx)
	c.Assert(err, IsNil)
	c.Assert(current, DeepEquals, &gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"pc": {
					Name:       "pc",
					Bootloader: "grub",
					Schema:     "gpt",
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
	os.WriteFile(filepath.Join(ui.MountDir(), "meta/gadget.yaml"), []byte(updateGadgetYaml), 0644)

	update, err = devicestate.PendingGadgetInfo(snapsup, deviceCtx)
	c.Assert(err, IsNil)
	c.Assert(update, DeepEquals, &gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"pc": {
					Name:       "pc",
					Bootloader: "grub",
					Schema:     "gpt",
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

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreHybridFirstboot(c *C) {
	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, _ gadget.ContentUpdateObserver) error {
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
		{"meta/gadget.yaml", hybridGadgetYaml},
	})

	s.state.Lock()
	s.state.Set("seeded", true)

	s.setupModelWithGadget(c, "foo-gadget")

	t := s.state.NewTask("update-gadget-assets", "update gadget")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// simulate restart and settle again
	s.mockRestartAndSettle(c, s.state, chg)

	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "foo-gadget")
	c.Check(osutil.IsDirectory(rollbackDir), Equals, false)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreHybridShouldWork(c *C) {
	encryption := false
	immediate := false
	isClassic := false
	s.testUpdateGadgetSimple(c, "", encryption, immediate, hybridGadgetYaml, "", isClassic)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreOldIsInvalidNowButShouldWork(c *C) {
	encryption := false
	immediate := false
	// this is not gadget yaml that we should support, by the UC16/18
	// rules it actually has two system-boot role partitions,
	hybridGadgetYamlBroken := hybridGadgetYaml + `
        role: system-boot
`
	isClassic := false
	s.testUpdateGadgetSimple(c, "", encryption, immediate, hybridGadgetYamlBroken, hybridGadgetYaml, isClassic)
}

func (s *deviceMgrGadgetSuite) makeMinimalKernelAssetsUpdateChange(c *C) (chg *state.Change, tsk *state.Task) {
	s.state.Lock()
	defer s.state.Unlock()

	siGadget := &snap.SideInfo{
		RealName: "foo-gadget",
		Revision: snap.R(1),
		SnapID:   "foo-gadget-id",
	}
	gadgetSnapYaml := "name: foo-gadget\nversion: 1.0\ntype: gadget"
	gadgetYamlContent := `
volumes:
  pi:
    bootloader: grub`
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, siGadget, [][]string{
		{"meta/gadget.yaml", gadgetYamlContent},
	})
	s.setupModelWithGadget(c, "foo-gadget")
	snapstate.Set(s.state, "foo-gadget", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siGadget}),
		Current:  siGadget.Revision,
		Active:   true,
	})

	snapKernelYaml := "name: pc-kernel\nversion: 1.0\ntype: kernel"
	siCurrent := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapKernelYaml, siCurrent, nil)
	siNext := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(34),
		SnapID:   "foo-id",
	}
	snaptest.MockSnapWithFiles(c, snapKernelYaml, siNext, nil)
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siNext, siCurrent}),
		Current:  siCurrent.Revision,
		Active:   true,
	})

	s.bootloader.SetBootVars(map[string]string{
		"snap_core":   "core_1.snap",
		"snap_kernel": "pc-kernel_33.snap",
	})

	tsk = s.state.NewTask("update-gadget-assets", "update gadget")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: siNext,
		Type:     snap.TypeKernel,
	})
	chg = s.state.NewChange("sample", "...")
	chg.AddTask(tsk)

	return chg, tsk
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreFromKernel(c *C) {
	var updateCalled int
	var passedRollbackDir string

	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, observer gadget.ContentUpdateObserver) error {
		updateCalled++
		passedRollbackDir = path

		c.Check(strings.HasSuffix(current.RootDir, "/snap/foo-gadget/1"), Equals, true)
		c.Check(strings.HasSuffix(update.RootDir, "/snap/foo-gadget/1"), Equals, true)
		c.Check(strings.HasSuffix(current.KernelRootDir, "/snap/pc-kernel/33"), Equals, true)
		c.Check(strings.HasSuffix(update.KernelRootDir, "/snap/pc-kernel/34"), Equals, true)

		// KernelUpdatePolicy is used
		c.Check(reflect.ValueOf(policy), DeepEquals, reflect.ValueOf(gadget.UpdatePolicyFunc(gadget.KernelUpdatePolicy)))
		return nil
	})
	defer restore()

	chg, t := s.makeMinimalKernelAssetsUpdateChange(c)
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// simulate restart and settle again
	s.mockRestartAndSettle(c, s.state, chg)

	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(updateCalled, Equals, 1)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "pc-kernel_34")
	c.Check(rollbackDir, Equals, passedRollbackDir)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetOnCoreFromKernelRemodel(c *C) {
	var updateCalled int
	var passedRollbackDir string

	restore := devicestate.MockGadgetUpdate(func(model gadget.Model, current, update gadget.GadgetData, path string, policy gadget.UpdatePolicyFunc, observer gadget.ContentUpdateObserver) error {
		updateCalled++
		passedRollbackDir = path

		c.Check(strings.HasSuffix(current.RootDir, "/snap/foo-gadget/1"), Equals, true)
		c.Check(strings.HasSuffix(update.RootDir, "/snap/foo-gadget/1"), Equals, true)
		c.Check(strings.HasSuffix(current.KernelRootDir, "/snap/pc-kernel/33"), Equals, true)
		c.Check(strings.HasSuffix(update.KernelRootDir, "/snap/pc-kernel/34"), Equals, true)

		// KernelUpdatePolicy is used even when we remodel
		c.Check(reflect.ValueOf(policy), DeepEquals, reflect.ValueOf(gadget.UpdatePolicyFunc(gadget.KernelUpdatePolicy)))
		return nil
	})
	defer restore()

	chg, t := s.makeMinimalKernelAssetsUpdateChange(c)
	devicestate.SetBootOkRan(s.mgr, true)

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "foo-gadget",
		"base":         "core18",
		"revision":     "1",
	})

	s.state.Lock()
	// pretend we are remodeling
	chg.Set("new-model", string(asserts.Encode(newModel)))
	s.state.Set("seeded", true)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// simulate restart and settle again
	s.mockRestartAndSettle(c, s.state, chg)

	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(updateCalled, Equals, 1)
	rollbackDir := filepath.Join(dirs.SnapRollbackDir, "pc-kernel_34")
	c.Check(rollbackDir, Equals, passedRollbackDir)
}

type testGadgetCommandlineUpdateOpts struct {
	updated       bool
	isClassic     bool
	grade         string
	cmdlineAppend string
	// This is the part of cmdlineAppend that is allowed by the gadget
	allowedCmdline string
	// and this is the not allowed part
	notAllowedCmdline   string
	cmdlineAppendDanger string
}

func (s *deviceMgrGadgetSuite) testGadgetCommandlineUpdateRun(c *C, fromFiles, toFiles [][]string, errMatch, logMatch string, opts testGadgetCommandlineUpdateOpts) {
	restore := release.MockOnClassic(opts.isClassic)
	defer restore()

	s.state.Lock()

	currentSi := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{currentSi}),
		Current:  currentSi.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, currentSi, fromFiles)
	updateSi := *currentSi
	updateSi.Revision = snap.R(34)
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, &updateSi, toFiles)

	tsk := s.state.NewTask("update-gadget-cmdline", "update gadget command line")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &updateSi,
		Type:     snap.TypeGadget,
	})
	argsAppended := false
	if opts.cmdlineAppend != "" {
		tsk.Set("cmdline-append", opts.cmdlineAppend)
		argsAppended = true
	}
	if opts.cmdlineAppendDanger != "" {
		tsk.Set("dangerous-cmdline-append", opts.cmdlineAppendDanger)
		argsAppended = true
	}
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(tsk)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	if errMatch == "" {
		if opts.updated && !argsAppended {
			// Ensure that "gadget-restart-required" was set
			var restartRequired bool
			c.Check(chg.Get("gadget-restart-required", &restartRequired), IsNil)
			c.Check(restartRequired, Equals, true)

			// Expect the change to be in wait status at this point, as a restart
			// will have been requested
			c.Check(tsk.Status(), Equals, state.WaitStatus)
			c.Check(chg.Status(), Equals, state.WaitStatus)
			// Restart and re-run to completion
			s.mockRestartAndSettle(c, s.state, chg)
		}

		c.Assert(chg.IsReady(), Equals, true)
		c.Check(chg.Err(), IsNil)
		c.Check(tsk.Status(), Equals, state.DoneStatus)

		// we log on success
		log := tsk.Log()
		if logMatch != "" {
			c.Check(log[0], Matches, fmt.Sprintf(".* %v", logMatch))
			if argsAppended {
				if opts.notAllowedCmdline != "" && opts.allowedCmdline != "" {
					// Part updated, part rejected
					c.Assert(log, HasLen, 2)
					c.Check(log[1], Matches, ".* Updated kernel command line")
				} else {
					c.Assert(log, HasLen, 1)
				}
			} else {
				c.Assert(log, HasLen, 2)
				c.Check(log[1], Matches, ".* INFO Task set to wait until a system restart allows to continue")
			}
		} else {
			c.Check(log, HasLen, 0)
		}
		if opts.isClassic || argsAppended {
			c.Check(s.restartRequests, HasLen, 0)
		} else if opts.updated {
			// update was applied, thus a restart was requested
			c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystem})
		} else {
			// update was not applied or failed
			c.Check(s.restartRequests, HasLen, 0)

			// Ensure that "gadget-restart-required" was not set
			var restartRequired bool
			c.Check(chg.Get("gadget-restart-required", &restartRequired), FitsTypeOf, &state.NoStateError{})
		}
	} else {
		c.Assert(chg.IsReady(), Equals, true)
		c.Check(chg.Err(), ErrorMatches, errMatch)
		c.Check(tsk.Status(), Equals, state.ErrorStatus)
	}
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineWithExistingArgs(c *C) {
	// arguments change
	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupUC20ModelWithGadget(c, "pc", "dangerous")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// update the modeenv to have the gadget arguments included to mimic the
	// state we would have in the system
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
	}
	c.Assert(m.Write(), IsNil)
	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from old gadget",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	s.state.Unlock()

	opts := testGadgetCommandlineUpdateOpts{
		updated:   true,
		isClassic: false,
		grade:     "dangerous",
	}
	s.testGadgetCommandlineUpdateRun(c,
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.extra", "args from old gadget"},
		},
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.extra", "args from updated gadget"},
		},
		"", "Updated kernel command line", opts)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
		// gadget arguments are picked up for the candidate command line
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from updated gadget",
	})
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 1)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	// bootenv was cleared
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "console=ttyS0 console=tty1 panic=-1 args from updated gadget",
	})
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineClassicWithModesWithExistingArgs(c *C) {
	// arguments change
	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupClassicWithModesModel(c, "pc")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// update the modeenv to have the gadget arguments included to mimic the
	// state we would have in the system
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
	}
	c.Assert(m.Write(), IsNil)
	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from old gadget",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	s.state.Unlock()

	opts := testGadgetCommandlineUpdateOpts{
		updated:   true,
		isClassic: true,
		grade:     "dangerous",
	}
	s.testGadgetCommandlineUpdateRun(c,
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.extra", "args from old gadget"},
		},
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.extra", "args from updated gadget"},
		},
		"", "Updated kernel command line", opts)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
		// gadget arguments are picked up for the candidate command line
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from updated gadget",
	})
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 1)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	// bootenv was cleared
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "console=ttyS0 console=tty1 panic=-1 args from updated gadget",
	})
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineWithNewArgs(c *C) {
	// no command line arguments prior to the gadget update
	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupUC20ModelWithGadget(c, "pc", "dangerous")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
	}
	c.Assert(m.Write(), IsNil)
	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	s.state.Unlock()

	opts := testGadgetCommandlineUpdateOpts{
		updated:   true,
		isClassic: false,
		grade:     "dangerous",
	}
	s.testGadgetCommandlineUpdateRun(c,
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			// old gadget does not carry command line arguments
		},
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.extra", "args from new gadget"},
		},
		"", "Updated kernel command line", opts)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		// gadget arguments are picked up for the candidate command line
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from new gadget",
	})
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 1)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	// bootenv was cleared
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "console=ttyS0 console=tty1 panic=-1 args from new gadget",
	})
}

func (s *deviceMgrGadgetSuite) testUpdateGadgetCommandlineWithNewAppendedArgs(c *C, opts testGadgetCommandlineUpdateOpts) {
	// no command line arguments prior to the gadget update
	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupUC20ModelWithGadget(c, "pc", opts.grade)
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
	}
	c.Assert(m.Write(), IsNil)
	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "console=ttyS0 console=tty1 panic=-1",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	s.state.Unlock()

	yaml := gadgetYaml + `
kernel-cmdline:
  allow:
    - par1=val
    - par2
    - append1=val
    - append2
`
	files := [][]string{
		{"meta/gadget.yaml", yaml},
	}
	expLog := ""
	if opts.updated {
		expLog = "Updated kernel command line"
	}
	if opts.notAllowedCmdline != "" {
		expLog = fmt.Sprintf("%q is not allowed by the gadget and has been filtered out from the kernel command line", opts.notAllowedCmdline)
	}
	// The task comes from setting a system option so it is not a
	// real gadget update and to/from files are the same.
	s.testGadgetCommandlineUpdateRun(c, files, files, "", expLog, opts)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)

	// gadget arguments are picked up for the candidate command line
	oldCmdline := "snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1"
	newCmdline := strutil.JoinNonEmpty([]string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		opts.allowedCmdline}, " ")
	if opts.grade == "dangerous" {
		newCmdline = strutil.JoinNonEmpty([]string{
			newCmdline, opts.cmdlineAppendDanger}, " ")
	}
	expCmdlines := []string{oldCmdline}
	numSetBootVarsCalls := 0
	// It might have not changed if all arguments are forbidden by the gadget
	if newCmdline != oldCmdline {
		expCmdlines = append(expCmdlines, newCmdline)
		numSetBootVarsCalls = 1
	}

	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, expCmdlines)
	c.Check(s.managedbl.SetBootVarsCalls, Equals, numSetBootVarsCalls)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	// bootenv was cleared
	extraArgs := opts.allowedCmdline
	if opts.grade == "dangerous" {
		extraArgs = strutil.JoinNonEmpty([]string{extraArgs, opts.cmdlineAppendDanger}, " ")
	}
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  strutil.JoinNonEmpty([]string{"console=ttyS0 console=tty1 panic=-1", extraArgs}, " "),
	})
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineWithNewAppendedArgs(c *C) {
	var opts testGadgetCommandlineUpdateOpts
	for _, isClassic := range []bool{false, true} {
		opts = testGadgetCommandlineUpdateOpts{
			updated:        true,
			isClassic:      isClassic,
			grade:          "dangerous",
			cmdlineAppend:  "append1=val append2",
			allowedCmdline: "append1=val append2",
		}
		s.testUpdateGadgetCommandlineWithNewAppendedArgs(c, opts)
		opts = testGadgetCommandlineUpdateOpts{
			updated:             true,
			isClassic:           isClassic,
			grade:               "dangerous",
			cmdlineAppendDanger: "danger1=val danger2",
		}
		s.testUpdateGadgetCommandlineWithNewAppendedArgs(c, opts)
		opts = testGadgetCommandlineUpdateOpts{
			updated:             true,
			isClassic:           isClassic,
			grade:               "dangerous",
			cmdlineAppend:       "append1=val append2",
			allowedCmdline:      "append1=val append2",
			cmdlineAppendDanger: "danger1=val danger2",
		}
		s.testUpdateGadgetCommandlineWithNewAppendedArgs(c, opts)
		opts = testGadgetCommandlineUpdateOpts{
			updated:           true,
			isClassic:         isClassic,
			grade:             "dangerous",
			cmdlineAppend:     "not.allowed=val append2",
			allowedCmdline:    "append2",
			notAllowedCmdline: "not.allowed=val",
		}
		s.testUpdateGadgetCommandlineWithNewAppendedArgs(c, opts)
		opts = testGadgetCommandlineUpdateOpts{
			updated:           true,
			isClassic:         isClassic,
			grade:             "dangerous",
			cmdlineAppend:     "not.allowed=val nope",
			allowedCmdline:    "",
			notAllowedCmdline: "not.allowed=val nope",
		}
		s.testUpdateGadgetCommandlineWithNewAppendedArgs(c, opts)
	}
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineWithNewAppendedArgsSigned(c *C) {
	var opts testGadgetCommandlineUpdateOpts
	for _, isClassic := range []bool{false, true} {
		opts = testGadgetCommandlineUpdateOpts{
			updated:             false,
			isClassic:           isClassic,
			grade:               "signed",
			cmdlineAppendDanger: "danger1=val danger2",
		}
		s.testUpdateGadgetCommandlineWithNewAppendedArgs(c, opts)
		opts = testGadgetCommandlineUpdateOpts{
			updated:             true,
			isClassic:           isClassic,
			grade:               "signed",
			cmdlineAppend:       "append1=val append2",
			allowedCmdline:      "append1=val append2",
			cmdlineAppendDanger: "danger1=val danger2",
		}
		s.testUpdateGadgetCommandlineWithNewAppendedArgs(c, opts)
	}
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineDroppedArgs(c *C) {
	// no command line arguments prior to the gadget up
	s.state.Lock()
	bootloader.Force(s.managedbl)
	s.setupUC20ModelWithGadget(c, "pc", "dangerous")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
	}
	c.Assert(m.Write(), IsNil)
	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from gadget",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	s.state.Unlock()

	opts := testGadgetCommandlineUpdateOpts{
		updated:   true,
		isClassic: false,
		grade:     "dangerous",
	}
	s.testGadgetCommandlineUpdateRun(c,
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			// old gadget carries command line arguments
			{"cmdline.extra", "args from gadget"},
		},
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			// new one does not
		},
		"", "Updated kernel command line", opts)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
		// this is the expected new command line
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
	})
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 1)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args")
	c.Assert(err, IsNil)
	// bootenv was cleared
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
	})
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineUnchanged(c *C) {
	// no command line arguments prior to the gadget update
	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupUC20ModelWithGadget(c, "pc", "dangerous")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
	}
	c.Assert(m.Write(), IsNil)
	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from gadget",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	s.state.Unlock()

	sameFiles := [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"cmdline.extra", "args from gadget"},
	}
	// old and new gadget have the same command line arguments, nothing changes
	opts := testGadgetCommandlineUpdateOpts{
		updated:   false,
		isClassic: false,
		grade:     "dangerous",
	}
	s.testGadgetCommandlineUpdateRun(c, sameFiles, sameFiles, "", "", opts)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
	})
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 0)
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineNonUC20(c *C) {
	// arguments are ignored on non UC20
	s.state.Lock()
	s.setupModelWithGadget(c, "pc")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// there is no modeenv either

	s.state.Unlock()
	opts := testGadgetCommandlineUpdateOpts{
		updated:   false,
		isClassic: false,
		grade:     "dangerous",
	}
	s.testGadgetCommandlineUpdateRun(c,
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			// old gadget does not carry command line arguments
		},
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.extra", "args from new gadget"},
		},
		"", "", opts)
}

func (s *deviceMgrGadgetSuite) TestGadgetCommandlineUpdateUndo(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupUC20ModelWithGadget(c, "pc", "dangerous")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
	}
	c.Assert(m.Write(), IsNil)

	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from old gadget",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	currentSi := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{currentSi}),
		Current:  currentSi.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, currentSi, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"cmdline.extra", "args from old gadget"},
	})
	updateSi := *currentSi
	updateSi.Revision = snap.R(34)
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, &updateSi, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"cmdline.extra", "args from new gadget"},
	})

	tsk := s.state.NewTask("update-gadget-cmdline", "update gadget command line")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &updateSi,
		Type:     snap.TypeGadget,
	})
	// XXX: The "update-gadget-cmdline" task does not support both Do and Undo in the same boot, so
	// currently we must ensure this is marked as a boundary when running as the only task in a change
	// and we want to ensure Undo can run. This should be properly handled so the below Boundary call is not
	// necessary.
	restart.MarkTaskAsRestartBoundary(tsk, restart.RestartBoundaryDirectionDo|restart.RestartBoundaryDirectionUndo)
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tsk)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(tsk)
	chg.AddTask(terr)
	chg.Set("system-restart-immediate", true)
	s.state.Unlock()

	restartCount := 0
	s.restartObserve = func() {
		// we want to observe restarts and mangle modeenv like
		// devicemanager boot handling would do
		restartCount++
		m, err := boot.ReadModeenv("")
		c.Assert(err, IsNil)
		switch restartCount {
		case 1:
			c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from new gadget",
			})
			m.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from new gadget"}
		case 2:
			c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from new gadget",
				"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
			})
			m.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget"}
		default:
			c.Fatalf("unexpected restart %v", restartCount)
		}
		c.Assert(m.Write(), IsNil)
	}

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	restarting, rt := restart.Pending(s.state)
	c.Check(restarting, Equals, true)
	c.Check(rt, Equals, restart.RestartSystemNow)

	// simulate restart for the 'do' path
	s.mockRestartAndSettle(c, s.state, chg)

	restarting, rt = restart.Pending(s.state)
	c.Check(restarting, Equals, true)
	c.Check(rt, Equals, restart.RestartSystemNow)

	// simulate restart for the 'undo' path
	s.mockRestartAndSettle(c, s.state, chg)

	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, "(?s)cannot perform the following tasks.*total undo.*")
	c.Check(tsk.Status(), Equals, state.UndoneStatus)
	log := tsk.Log()
	c.Assert(log, HasLen, 4)
	c.Check(log[0], Matches, ".* Updated kernel command line")
	c.Check(log[1], Matches, ".* INFO Task set to wait until a system restart allows to continue")
	c.Check(log[2], Matches, ".* Reverted kernel command line change")
	c.Check(log[3], Matches, ".* INFO Task set to wait until a system restart allows to continue")
	// update was applied and then undone
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow, restart.RestartSystemNow})
	c.Check(restartCount, Equals, 2)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "console=ttyS0 console=tty1 panic=-1 args from old gadget",
	})
	// 2 calls, one to set the new arguments, and one to reset them back
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 2)
}

func (s *deviceMgrGadgetSuite) TestGadgetCommandlineClassicWithModesUpdateUndo(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupClassicWithModesModel(c, "pc")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from old gadget",
	}
	c.Assert(m.Write(), IsNil)

	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from old gadget",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	currentSi := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{currentSi}),
		Current:  currentSi.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, currentSi, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"cmdline.extra", "args from old gadget"},
	})
	updateSi := *currentSi
	updateSi.Revision = snap.R(34)
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, &updateSi, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"cmdline.extra", "args from new gadget"},
	})

	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		// We simulate the modeenv we would have after a reboot
		m, err := boot.ReadModeenv("")
		c.Assert(err, IsNil)
		m.CurrentKernelCommandLines = []string{"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from new gadget"}
		c.Assert(m.Write(), IsNil)
		return errors.New("error out")
	}
	// FIXME the handler will be around for other tests, not sure if there
	// is a way to remove it.
	s.o.TaskRunner().AddHandler("error-save-mode-trigger", erroringHandler, nil)

	tsk := s.state.NewTask("update-gadget-cmdline", "update gadget command line")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &updateSi,
		Type:     snap.TypeGadget,
	})
	terr := s.state.NewTask("error-save-mode-trigger", "provoking total undo")
	terr.WaitFor(tsk)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(tsk)
	chg.AddTask(terr)
	chg.Set("system-restart-immediate", true)
	s.state.Unlock()

	restartCount := 0
	s.restartObserve = func() {
		// should not be called for classic with modes
		restartCount++
	}

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Ensure that "gadget-restart-required" was set
	var restartRequired bool
	c.Check(chg.Get("gadget-restart-required", &restartRequired), IsNil)
	c.Check(restartRequired, Equals, true)

	// Expect the change to be in wait status at this point, as a restart
	// will have been requested
	c.Check(tsk.Status(), Equals, state.WaitStatus)
	c.Check(chg.Status(), Equals, state.WaitStatus)
	// Restart and re-run to completion
	s.mockRestartAndSettle(c, s.state, chg)

	log := tsk.Log()
	c.Assert(log, HasLen, 4)
	c.Check(log[0], Matches, ".* Updated kernel command line")
	c.Check(log[1], Matches, ".* INFO Task set to wait until a system restart allows to continue")
	c.Check(log[2], Matches, ".* Reverted kernel command line change")
	c.Check(log[3], Matches, ".* Skipped automatic system restart on classic system when undoing changes back to previous state")

	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, "(?s)cannot perform the following tasks.*total undo.*")
	c.Check(tsk.Status(), Equals, state.UndoneStatus)

	// update was applied and then undone, but no automatic restarts happened
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(restartCount, Equals, 0)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "console=ttyS0 console=tty1 panic=-1 args from old gadget",
	})
	// 2 calls, one to set the new arguments, and one to reset them back
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 2)
}

func (s *deviceMgrGadgetSuite) TestGadgetCommandlineUpdateNoChangeNoRebootsUndo(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupUC20ModelWithGadget(c, "pc", "dangerous")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
	}
	c.Assert(m.Write(), IsNil)

	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "args from gadget",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	currentSi := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{currentSi}),
		Current:  currentSi.Revision,
		Active:   true,
	})
	sameFiles := [][]string{
		{"meta/gadget.yaml", gadgetYaml},
		{"cmdline.extra", "args from gadget"},
	}
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, currentSi, sameFiles)
	updateSi := *currentSi
	updateSi.Revision = snap.R(34)
	// identical content, just a revision bump
	snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, &updateSi, sameFiles)

	tsk := s.state.NewTask("update-gadget-cmdline", "update gadget command line")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &updateSi,
		Type:     snap.TypeGadget,
	})
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tsk)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(tsk)
	chg.AddTask(terr)
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), ErrorMatches, "(?s)cannot perform the following tasks.*total undo.*")
	c.Check(tsk.Status(), Equals, state.UndoneStatus)
	// there was nothing to update and thus nothing to undo
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 0)
	// modeenv wasn't changed
	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
	})
}

func (s *deviceMgrGadgetSuite) TestUpdateGadgetCommandlineWithFullArgs(c *C) {
	bootloader.Force(s.managedbl)
	s.state.Lock()
	s.setupUC20ModelWithGadget(c, "pc", "dangerous")
	s.mockModeenvForMode(c, "run")
	devicestate.SetBootOkRan(s.mgr, true)
	s.state.Set("seeded", true)

	// mimic system state
	m, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 extra args",
	}
	c.Assert(m.Write(), IsNil)
	err = s.managedbl.SetBootVars(map[string]string{
		"snapd_extra_cmdline_args": "extra args",
		"snapd_full_cmdline_args":  "",
	})
	c.Assert(err, IsNil)
	s.managedbl.SetBootVarsCalls = 0

	s.state.Unlock()

	opts := testGadgetCommandlineUpdateOpts{
		updated:   true,
		isClassic: false,
		grade:     "dangerous",
	}
	s.testGadgetCommandlineUpdateRun(c,
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.extra", "extra args"},
		},
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"cmdline.full", "full args"},
		},
		"", "Updated kernel command line", opts)

	m, err = boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 extra args",
		// gadget arguments are picked up for the candidate command line
		"snapd_recovery_mode=run full args",
	})
	c.Check(s.managedbl.SetBootVarsCalls, Equals, 1)
	vars, err := s.managedbl.GetBootVars("snapd_extra_cmdline_args", "snapd_full_cmdline_args")
	c.Assert(err, IsNil)
	// bootenv was cleared
	c.Assert(vars, DeepEquals, map[string]string{
		"snapd_extra_cmdline_args": "",
		"snapd_full_cmdline_args":  "full args",
	})
}
