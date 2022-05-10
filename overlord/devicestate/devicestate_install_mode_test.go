// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"bytes"
	"compress/gzip"
	"crypto"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type deviceMgrInstallModeSuite struct {
	deviceMgrBaseSuite

	ConfigureTargetSystemOptsPassed []*sysconfig.Options
	ConfigureTargetSystemErr        error
}

var _ = Suite(&deviceMgrInstallModeSuite{})

func (s *deviceMgrInstallModeSuite) findInstallSystem() *state.Change {
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "install-system" {
			return chg
		}
	}
	return nil
}

func (s *deviceMgrInstallModeSuite) SetUpTest(c *C) {
	classic := false
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)

	// restore dirs after os-release mock is cleaned up
	s.AddCleanup(func() { dirs.SetRootDir(dirs.GlobalRootDir) })
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	// reload directory paths to match our mocked os-release
	dirs.SetRootDir(dirs.GlobalRootDir)

	s.ConfigureTargetSystemOptsPassed = nil
	s.ConfigureTargetSystemErr = nil
	restore := devicestate.MockSysconfigConfigureTargetSystem(func(mod *asserts.Model, opts *sysconfig.Options) error {
		c.Check(mod, NotNil)
		s.ConfigureTargetSystemOptsPassed = append(s.ConfigureTargetSystemOptsPassed, opts)
		return s.ConfigureTargetSystemErr
	})
	s.AddCleanup(restore)

	restore = devicestate.MockSecbootCheckTPMKeySealingSupported(func() error {
		return fmt.Errorf("TPM not available")
	})
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)

	fakeJournalctl := testutil.MockCommand(c, "journalctl", "")
	s.AddCleanup(fakeJournalctl.Restore)
}

const (
	pcSnapID       = "pcididididididididididididididid"
	pcKernelSnapID = "pckernelidididididididididididid"
	core20SnapID   = "core20ididididididididididididid"
)

func (s *deviceMgrInstallModeSuite) makeMockInstalledPcGadget(c *C, installDeviceHook string, gadgetDefaultsYaml string) {
	si := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
		SnapID:   pcKernelSnapID,
	}
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})
	kernelInfo := snaptest.MockSnapWithFiles(c, "name: pc-kernel\ntype: kernel", si, nil)
	kernelFn := snaptest.MakeTestSnapWithFiles(c, "name: pc-kernel\ntype: kernel\nversion: 1.0", nil)
	err := os.Rename(kernelFn, kernelInfo.MountFile())
	c.Assert(err, IsNil)

	si = &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   pcSnapID,
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})

	files := [][]string{
		{"meta/gadget.yaml", uc20gadgetYamlWithSave + gadgetDefaultsYaml},
	}
	if installDeviceHook != "" {
		files = append(files, []string{"meta/hooks/install-device", installDeviceHook})
	}
	snaptest.MockSnapWithFiles(c, "name: pc\ntype: gadget", si, files)

	si = &snap.SideInfo{
		RealName: "core20",
		Revision: snap.R(2),
		SnapID:   core20SnapID,
	}
	snapstate.Set(s.state, "core20", &snapstate.SnapState{
		SnapType: "base",
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
		Active:   true,
	})
	snaptest.MockSnapWithFiles(c, "name: core20\ntype: base", si, nil)
}

func (s *deviceMgrInstallModeSuite) makeMockInstallModel(c *C, grade string) *asserts.Model {
	mockModel := s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        grade,
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              pcKernelSnapID,
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              pcSnapID,
				"type":            "gadget",
				"default-channel": "20",
			}},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
		// no serial in install mode
	})

	return mockModel
}

type encTestCase struct {
	tpm               bool
	bypass            bool
	encrypt           bool
	trustedBootloader bool
}

var (
	dataEncryptionKey = keys.EncryptionKey{'d', 'a', 't', 'a', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	saveKey           = keys.EncryptionKey{'s', 'a', 'v', 'e', 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
)

func (s *deviceMgrInstallModeSuite) doRunChangeTestWithEncryption(c *C, grade string, tc encTestCase) error {
	restore := release.MockOnClassic(false)
	defer restore()
	bootloaderRootdir := c.MkDir()

	var brGadgetRoot, brDevice string
	var brOpts install.Options
	var installRunCalled int
	var installSealingObserver gadget.ContentObserver
	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, obs gadget.ContentObserver, pertTimings timings.Measurer) (*install.InstalledSystemSideData, error) {
		// ensure we can grab the lock here, i.e. that it's not taken
		s.state.Lock()
		s.state.Unlock()

		c.Check(mod.Grade(), Equals, asserts.ModelGrade(grade))

		brGadgetRoot = gadgetRoot
		brDevice = device
		brOpts = options
		installSealingObserver = obs
		installRunCalled++
		var keyForRole map[string]keys.EncryptionKey
		if tc.encrypt {
			keyForRole = map[string]keys.EncryptionKey{
				gadget.SystemData: dataEncryptionKey,
				gadget.SystemSave: saveKey,
			}
		}
		return &install.InstalledSystemSideData{
			KeyForRole: keyForRole,
		}, nil
	})
	defer restore()

	restore = devicestate.MockSecbootCheckTPMKeySealingSupported(func() error {
		if tc.tpm {
			return nil
		} else {
			return fmt.Errorf("TPM not available")
		}
	})
	defer restore()

	if tc.trustedBootloader {
		tab := bootloadertest.Mock("trusted", bootloaderRootdir).WithTrustedAssets()
		tab.TrustedAssetsList = []string{"trusted-asset"}
		bootloader.Force(tab)
		s.AddCleanup(func() { bootloader.Force(nil) })

		err := os.MkdirAll(boot.InitramfsUbuntuSeedDir, 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSeedDir, "trusted-asset"), nil, 0644)
		c.Assert(err, IsNil)
	}

	s.state.Lock()
	mockModel := s.makeMockInstallModel(c, grade)
	s.makeMockInstalledPcGadget(c, "", "")
	s.state.Unlock()

	bypassEncryptionPath := filepath.Join(boot.InitramfsUbuntuSeedDir, ".force-unencrypted")
	if tc.bypass {
		err := os.MkdirAll(filepath.Dir(bypassEncryptionPath), 0755)
		c.Assert(err, IsNil)
		f, err := os.Create(bypassEncryptionPath)
		c.Assert(err, IsNil)
		f.Close()
	} else {
		os.RemoveAll(bypassEncryptionPath)
	}

	bootMakeBootableCalled := 0
	restore = devicestate.MockBootMakeSystemRunnable(func(model *asserts.Model, bootWith *boot.BootableSet, seal *boot.TrustedAssetsInstallObserver) error {
		c.Check(model, DeepEquals, mockModel)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemDir, Matches, "/systems/20191218")
		c.Check(bootWith.UnpackedGadgetDir, Equals, filepath.Join(dirs.SnapMountDir, "pc/1"))
		if tc.encrypt {
			c.Check(seal, NotNil)
		} else {
			c.Check(seal, IsNil)
		}
		bootMakeBootableCalled++
		return nil
	})
	defer restore()

	modeenv := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191218",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)
	devicestate.SetSystemMode(s.mgr, "install")

	// normally done by snap-bootstrap
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)

	s.settle(c)

	// the install-system change is created
	s.state.Lock()
	defer s.state.Unlock()
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	if err := installSystem.Err(); err != nil {
		// we failed, no further checks needed
		return err
	}

	c.Assert(installSystem.Status(), Equals, state.DoneStatus)

	// in the right way
	c.Assert(brGadgetRoot, Equals, filepath.Join(dirs.SnapMountDir, "/pc/1"))
	c.Assert(brDevice, Equals, "")
	if tc.encrypt {
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount:          true,
			EncryptionType: secboot.EncryptionTypeLUKS,
		})
	} else {
		c.Assert(brOpts, DeepEquals, install.Options{
			Mount: true,
		})
	}
	if tc.encrypt {
		// inteface is not nil
		c.Assert(installSealingObserver, NotNil)
		// we expect a very specific type
		trustedInstallObserver, ok := installSealingObserver.(*boot.TrustedAssetsInstallObserver)
		c.Assert(ok, Equals, true, Commentf("unexpected type: %T", installSealingObserver))
		c.Assert(trustedInstallObserver, NotNil)
	} else {
		c.Assert(installSealingObserver, IsNil)
	}

	c.Assert(installRunCalled, Equals, 1)
	c.Assert(bootMakeBootableCalled, Equals, 1)
	c.Assert(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	return nil
}

func (s *deviceMgrInstallModeSuite) TestInstallTaskErrors(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, fmt.Errorf("The horror, The horror")
	})
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(cannot install system: The horror, The horror\)`)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallExpTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 2)
	setupRunSystemTask := tasks[0]
	restartSystemToRunModeTask := tasks[1]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")

	// setup-run-system has no pre-reqs
	c.Assert(setupRunSystemTask.WaitTasks(), HasLen, 0)

	// restart-system-to-run-mode has a pre-req of setup-run-system
	waitTasks := restartSystemToRunModeTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Assert(waitTasks[0].ID(), Equals, setupRunSystemTask.ID())

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
}

func (s *deviceMgrInstallModeSuite) TestInstallRestoresPreseedArtifact(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	var applyPreseedCalled int
	restoreApplyPreseed := devicestate.MockMaybeApplyPreseededData(func(st *state.State, ubuntuSeedDir, sysLabel, writableDir string) (bool, error) {
		applyPreseedCalled++
		c.Check(ubuntuSeedDir, Equals, filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed"))
		c.Check(sysLabel, Equals, "20200105")
		c.Check(writableDir, Equals, filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data"))
		return true, nil
	})
	defer restoreApplyPreseed()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=20200105\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
	c.Check(applyPreseedCalled, Equals, 1)
}

func (s *deviceMgrInstallModeSuite) TestInstallRestoresPreseedArtifactError(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	var applyPreseedCalled int
	restoreApplyPreseed := devicestate.MockMaybeApplyPreseededData(func(st *state.State, ubuntuSeedDir, sysLabel, writableDir string) (bool, error) {
		applyPreseedCalled++
		return false, fmt.Errorf("boom")
	})
	defer restoreApplyPreseed()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=20200105\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, "cannot perform the following tasks:\\n- Ensure next boot to run mode \\(boom\\)")

	c.Check(s.restartRequests, HasLen, 0)
	c.Check(applyPreseedCalled, Equals, 1)
}

type fakeSeed struct {
	modeSnaps      []*seed.Snap
	essentialSnaps []*seed.Snap
}

func (fakeSeed) LoadAssertions(db asserts.RODatabase, commitTo func(*asserts.Batch) error) error {
	return nil
}

func (fakeSeed) Model() *asserts.Model {
	return nil
}

func (fakeSeed) Brand() (*asserts.Account, error) {
	return nil, nil
}

func (fakeSeed) LoadEssentialMeta(essentialTypes []snap.Type, tm timings.Measurer) error {
	return nil
}

func (fakeSeed) LoadEssentialMetaWithSnapHandler([]snap.Type, seed.SnapHandler, timings.Measurer) error {
	return nil
}

func (fakeSeed) LoadMeta(string, seed.SnapHandler, timings.Measurer) error {
	return nil
}

func (fakeSeed) UsesSnapdSnap() bool {
	return true
}

func (fakeSeed) SetParallelism(n int) {}

func (f *fakeSeed) EssentialSnaps() []*seed.Snap {
	return f.essentialSnaps
}

func (f *fakeSeed) ModeSnaps(mode string) ([]*seed.Snap, error) {
	return f.modeSnaps, nil
}

func (f *fakeSeed) NumSnaps() int {
	return 0
}

func (f *fakeSeed) Iter(func(sn *seed.Snap) error) error {
	return nil
}

func (fs *fakeSeed) LoadAutoImportAssertion(commitTo func(*asserts.Batch) error) error {
	return nil
}

func (s *deviceMgrInstallModeSuite) mockPreseedAssertion(c *C, brandID, modelName, series, preseedAsPath, sysLabel string, digest string, snaps []interface{}) {
	headers := map[string]interface{}{
		"type":              "preseed",
		"authority-id":      brandID,
		"series":            series,
		"brand-id":          brandID,
		"model":             modelName,
		"system-label":      sysLabel,
		"artifact-sha3-384": digest,
		"timestamp":         time.Now().UTC().Format(time.RFC3339),
		"snaps":             snaps,
	}

	signer := s.brands.Signing(brandID)
	preseedAs, err := signer.Sign(asserts.PreseedType, headers, nil, "")
	if err != nil {
		panic(err)
	}

	f, err := os.Create(preseedAsPath)
	defer f.Close()
	c.Assert(err, IsNil)
	enc := asserts.NewEncoder(f)
	c.Assert(enc.Encode(preseedAs), IsNil)

	// other-brand account key needs to be explicitly added to the serialized preseed assertion
	// if needed by some of the unhappy-scenario tests (normally my-brand is used).
	if brandID == "other-brand" {
		for _, as := range s.brands.AccountsAndKeys("other-brand") {
			c.Assert(enc.Encode(as), IsNil)
		}
	}
}

func (s *deviceMgrInstallModeSuite) setupCore20Seed(ts *seedtest.TestingSeed20, c *C) *asserts.Model {
	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
        - name: ubuntu-seed
          role: system-seed
          type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
          size: 1G
        - name: ubuntu-data
          role: system-data
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          size: 2G
`
	makeSnap := func(yamlKey string) {
		var files [][]string
		if yamlKey == "pc=20" {
			files = append(files, []string{"meta/gadget.yaml", gadgetYaml})
		}
		ts.MakeAssertedSnap(c, seedtest.SampleSnapYaml[yamlKey], files, snap.R(1), "canonical", ts.StoreSigning.Database)
	}

	makeSnap("snapd")
	makeSnap("pc-kernel=20")
	makeSnap("core20")
	makeSnap("pc=20")
	optSnapPath := snaptest.MakeTestSnapWithFiles(c, seedtest.SampleSnapYaml["optional20-a"], nil)

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              ts.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              ts.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   ts.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core20",
				"id":   ts.AssertedSnapID("core20"),
				"type": "base",
			}},
	}

	return ts.MakeSeed(c, "20220401", "my-brand", "my-model", model, []*seedwriter.OptionsSnap{{Path: optSnapPath}})
}

type dumpDirContents struct {
	c   *C
	dir string
}

func (d *dumpDirContents) CheckCommentString() string {
	cmd := exec.Command("find", d.dir)
	data, err := cmd.CombinedOutput()
	d.c.Assert(err, IsNil)
	return fmt.Sprintf("writable dir contents:\n%s", data)
}

func (s *deviceMgrInstallModeSuite) TestMaybeApplyPreseededData(c *C) {
	st := s.state

	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	ubuntuSeedDir := dirs.SnapSeedDir
	sysLabel := "20220401"
	writableDir := filepath.Join(c.MkDir(), "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")

	restore := seed.MockTrusted(s.storeSigning.Trusted)
	defer restore()

	// now create a minimal uc20 seed dir with snaps/assertions
	ss := &seedtest.SeedSnaps{
		StoreSigning: s.storeSigning,
		Brands:       s.brands,
	}

	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: *ss,
		SeedDir:   ubuntuSeedDir,
	}

	model := s.setupCore20Seed(seed20, c)

	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(preseedArtifact, nil, 0644), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "snaps"), 0755), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)

	st.Lock()
	defer st.Unlock()

	c.Assert(devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
		// no serial in install mode
	}), IsNil)

	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	assertstatetest.AddMany(st, model)

	snaps := []interface{}{
		map[string]interface{}{"name": "snapd", "id": seed20.AssertedSnapID("snapd"), "revision": "1"},
		map[string]interface{}{"name": "core20", "id": seed20.AssertedSnapID("core20"), "revision": "1"},
		map[string]interface{}{"name": "pc-kernel", "id": seed20.AssertedSnapID("pc-kernel"), "revision": "1"},
		map[string]interface{}{"name": "pc", "id": seed20.AssertedSnapID("pc"), "revision": "1"},
		map[string]interface{}{"name": "optional20-a"},
	}
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")
	s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, digest, snaps)

	// set a specific mod time on one of the snaps to verify it's preserved when the blob gets copied.
	pastTime, err := time.Parse(time.RFC3339, "2020-01-01T10:00:00Z")
	c.Assert(err, IsNil)
	c.Assert(os.Chtimes(filepath.Join(ubuntuSeedDir, "snaps", "snapd_1.snap"), pastTime, pastTime), IsNil)

	// restore root dir, otherwise paths referencing GlobalRootDir, such as from placeInfo.MountFile() get confused
	// in the test.
	dirs.SetRootDir("/")
	preseeded, err := devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
	c.Assert(err, IsNil)
	c.Check(preseeded, Equals, true)

	c.Check(mockTarCmd.Calls(), DeepEquals, [][]string{
		{"tar", "--extract", "--preserve-permissions", "--preserve-order", "--gunzip", "--directory", writableDir, "-f", preseedArtifact},
	})

	for _, seedSnap := range []struct {
		name string
		blob string
	}{
		{"snapd/1", "snapd_1.snap"},
		{"core20/1", "core20_1.snap"},
		{"pc-kernel/1", "pc-kernel_1.snap"},
		{"pc/1", "pc_1.snap"},
		{"optional20-a/x1", "optional20-a_x1.snap"},
	} {
		c.Assert(osutil.FileExists(filepath.Join(writableDir, "/snap", seedSnap.name)), Equals, true, &dumpDirContents{c, writableDir})
		c.Assert(osutil.FileExists(filepath.Join(writableDir, dirs.SnapBlobDir, seedSnap.blob)), Equals, true, &dumpDirContents{c, writableDir})
	}

	// verify that modtime of the copied snap blob was preserved
	finfo, err := os.Stat(filepath.Join(writableDir, dirs.SnapBlobDir, "snapd_1.snap"))
	c.Assert(err, IsNil)
	c.Check(finfo.ModTime().Equal(pastTime), Equals, true)
}

func (s *deviceMgrInstallModeSuite) TestMaybeApplyPreseededDataSnapMismatch(c *C) {
	st := s.state

	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	snapPath1 := filepath.Join(dirs.GlobalRootDir, "essential-snap_1.snap")
	snapPath2 := filepath.Join(dirs.GlobalRootDir, "mode-snap_3.snap")
	c.Assert(ioutil.WriteFile(snapPath1, nil, 0644), IsNil)
	c.Assert(ioutil.WriteFile(snapPath2, nil, 0644), IsNil)

	restore := devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{
			essentialSnaps: []*seed.Snap{{Path: snapPath1, SideInfo: &snap.SideInfo{RealName: "essential-snap", Revision: snap.R(1), SnapID: "id111111111111111111111111111111"}}},
			modeSnaps: []*seed.Snap{{Path: snapPath2, SideInfo: &snap.SideInfo{RealName: "mode-snap", Revision: snap.R(3), SnapID: "id222222222222222222222222222222"}},
				{Path: snapPath2, SideInfo: &snap.SideInfo{RealName: "mode-snap2"}}},
		}, nil
	})
	defer restore()

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(preseedArtifact, nil, 0644), IsNil)

	st.Lock()
	defer st.Unlock()
	model := s.makeMockInstallModel(c, "dangerous")

	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")

	for _, tc := range []struct {
		snapName string
		rev      string
		snapID   string
		err      string
	}{
		{"essential-snap", "2", "id111111111111111111111111111111", `snap "essential-snap" has wrong revision 1 \(expected: 2\)`},
		{"essential-snap", "1", "id000000000000000000000000000000", `snap "essential-snap" has wrong snap id "id111111111111111111111111111111" \(expected: "id000000000000000000000000000000"\)`},
		{"mode-snap", "4", "id222222222222222222222222222222", `snap "mode-snap" has wrong revision 3 \(expected: 4\)`},
		{"mode-snap", "3", "id000000000000000000000000000000", `snap "mode-snap" has wrong snap id "id222222222222222222222222222222" \(expected: "id000000000000000000000000000000"\)`},
		{"mode-snap2", "3", "id000000000000000000000000000000", `snap "mode-snap2" has wrong revision unset \(expected: 3\)`},
		{"extra-snap", "1", "id000000000000000000000000000000", `seed has 3 snaps but 4 snaps are required by preseed assertion`},
	} {

		preseedAsSnaps := []interface{}{
			map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
			map[string]interface{}{"name": "mode-snap", "id": "id222222222222222222222222222222", "revision": "3"},
			map[string]interface{}{"name": "mode-snap2"},
		}

		var found bool
		for i, ps := range preseedAsSnaps {
			if ps.(map[string]interface{})["name"] == tc.snapName {
				preseedAsSnaps[i] = map[string]interface{}{"name": tc.snapName, "id": tc.snapID, "revision": tc.rev}
				found = true
				break
			}
		}
		if !found {
			preseedAsSnaps = append(preseedAsSnaps, map[string]interface{}{"name": tc.snapName, "id": tc.snapID, "revision": tc.rev})
		}

		s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, digest, preseedAsSnaps)
		_, err = devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
		c.Assert(err, ErrorMatches, tc.err)
	}

	// mode-snap is presend in the seed but missing in the preseed assertion; add other-snap to preseed assertion
	// to satisfy the check for number of snaps.
	preseedAsSnaps := []interface{}{
		map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
		map[string]interface{}{"name": "other-snap", "id": "id333222222222222222222222222222", "revision": "2"},
		map[string]interface{}{"name": "mode-snap2"},
	}
	s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, digest, preseedAsSnaps)
	_, err = devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
	c.Assert(err, ErrorMatches, `snap "mode-snap" not present in the preseed assertion`)
}

func (s *deviceMgrInstallModeSuite) TestMaybeApplyPreseededSysLabelMismatch(c *C) {
	st := s.state

	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	snapPath1 := filepath.Join(dirs.GlobalRootDir, "essential-snap_1.snap")
	c.Assert(ioutil.WriteFile(snapPath1, nil, 0644), IsNil)

	restore := devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{
			essentialSnaps: []*seed.Snap{{Path: snapPath1, SideInfo: &snap.SideInfo{RealName: "essential-snap", Revision: snap.R(1)}}},
		}, nil
	})
	defer restore()

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(preseedArtifact, nil, 0644), IsNil)

	st.Lock()
	defer st.Unlock()
	model := s.makeMockInstallModel(c, "dangerous")

	snaps := []interface{}{
		map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
	}
	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")
	s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, "wrong-label", digest, snaps)

	_, err = devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
	c.Assert(err, ErrorMatches, `preseed assertion system label "wrong-label" doesn't match system label "20220105"`)
}

func (s *deviceMgrInstallModeSuite) TestMaybeApplyPreseededDataWrongDigest(c *C) {
	st := s.state

	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	snapPath1 := filepath.Join(dirs.GlobalRootDir, "essential-snap_1.snap")
	c.Assert(ioutil.WriteFile(snapPath1, nil, 0644), IsNil)

	restore := devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{
			essentialSnaps: []*seed.Snap{{Path: snapPath1, SideInfo: &snap.SideInfo{RealName: "essential-snap", Revision: snap.R(1)}}},
		}, nil
	})
	defer restore()

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(preseedArtifact, nil, 0644), IsNil)

	st.Lock()
	defer st.Unlock()
	model := s.makeMockInstallModel(c, "dangerous")

	snaps := []interface{}{
		map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
	}

	wrongDigest := "DGOnW4ReT30BEH2FLkwkhcUaUKqqlPxhmV5xu-6YOirDcTgxJkrbR_traaaY1fAE"
	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")
	s.mockPreseedAssertion(c, model.BrandID(), model.Model(), "16", preseedAsPath, sysLabel, wrongDigest, snaps)

	_, err := devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
	c.Assert(err, ErrorMatches, `invalid preseed artifact digest`)
}

func (s *deviceMgrInstallModeSuite) TestMaybeApplyPreseededModelMismatch(c *C) {
	st := s.state

	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	snapPath1 := filepath.Join(dirs.GlobalRootDir, "essential-snap_1.snap")
	c.Assert(ioutil.WriteFile(snapPath1, nil, 0644), IsNil)

	restore := devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{
			essentialSnaps: []*seed.Snap{{Path: snapPath1, SideInfo: &snap.SideInfo{RealName: "essential-snap", Revision: snap.R(1)}}},
		}, nil
	})
	defer restore()

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(preseedArtifact, nil, 0644), IsNil)

	st.Lock()
	defer st.Unlock()

	s.brands.Register("other-brand", brandPrivKey3, map[string]interface{}{
		"display-name": "other publisher",
	})

	model := s.makeMockInstallModel(c, "dangerous")

	snaps := []interface{}{
		map[string]interface{}{"name": "essential-snap", "id": "id111111111111111111111111111111", "revision": "1"},
	}

	sha3_384, _, err := osutil.FileDigest(preseedArtifact, crypto.SHA3_384)
	c.Assert(err, IsNil)
	digest, err := asserts.EncodeDigest(crypto.SHA3_384, sha3_384)
	c.Assert(err, IsNil)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")

	for _, tc := range []struct {
		brandID   string
		modelName string
		series    string
		err       string
	}{
		{"other-brand", model.Model(), "16", `preseed assertion brand "other-brand" doesn't match model brand "my-brand"`},
		{model.BrandID(), "other-model", "16", `preseed assertion model "other-model" doesn't match the model "my-model"`},
		{model.BrandID(), model.Model(), "99", `preseed assertion series "99" doesn't match model series "16"`},
	} {
		s.mockPreseedAssertion(c, tc.brandID, tc.modelName, tc.series, preseedAsPath, sysLabel, digest, snaps)
		_, err := devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
		c.Assert(err, ErrorMatches, tc.err)
	}
}

func (s *deviceMgrInstallModeSuite) TestMaybeApplyPreseededAssertionMissing(c *C) {
	st := s.state

	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	snapPath1 := filepath.Join(dirs.GlobalRootDir, "essential-snap_1.snap")
	c.Assert(ioutil.WriteFile(snapPath1, nil, 0644), IsNil)

	restore := devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{
			essentialSnaps: []*seed.Snap{{Path: snapPath1, SideInfo: &snap.SideInfo{RealName: "essential-snap", Revision: snap.R(1)}}},
		}, nil
	})
	defer restore()

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	preseedArtifact := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed.tgz")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	c.Assert(ioutil.WriteFile(preseedArtifact, nil, 0644), IsNil)

	st.Lock()
	defer st.Unlock()

	s.makeMockInstallModel(c, "dangerous")

	_, err := devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
	c.Assert(err, ErrorMatches, `cannot read preseed assertion:.*`)

	preseedAsPath := filepath.Join(ubuntuSeedDir, "systems", sysLabel, "preseed")
	// empty "preseed" assertion file
	c.Assert(ioutil.WriteFile(preseedAsPath, nil, 0644), IsNil)

	_, err = devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
	c.Assert(err, ErrorMatches, `internal error: preseed assertion file is present but preseed assertion not found`)
}

func (s *deviceMgrInstallModeSuite) TestMaybeApplyPreseededNoopIfNoArtifact(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockTarCmd := testutil.MockCommand(c, "tar", "")
	defer mockTarCmd.Restore()

	ubuntuSeedDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed")
	sysLabel := "20220105"
	writableDir := filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-data/system-data")
	c.Assert(os.MkdirAll(filepath.Join(ubuntuSeedDir, "systems", sysLabel), 0755), IsNil)
	c.Assert(os.MkdirAll(writableDir, 0755), IsNil)
	preseeded, err := devicestate.MaybeApplyPreseededData(st, ubuntuSeedDir, sysLabel, writableDir)
	c.Assert(err, IsNil)
	c.Check(preseeded, Equals, false)
	c.Check(mockTarCmd.Calls(), HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithInstallDeviceHookExpTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return nil, nil
	})
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 3)
	setupRunSystemTask := tasks[0]
	installDevice := tasks[1]
	restartSystemToRunModeTask := tasks[2]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")
	c.Assert(installDevice.Kind(), Equals, "run-hook")

	// setup-run-system has no pre-reqs
	c.Assert(setupRunSystemTask.WaitTasks(), HasLen, 0)

	// install-device has a pre-req of setup-run-system
	waitTasks := installDevice.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Assert(waitTasks[0].ID(), Equals, setupRunSystemTask.ID())

	// install-device restart-task references to restart-system-to-run-mode
	var restartTask string
	err = installDevice.Get("restart-task", &restartTask)
	c.Assert(err, IsNil)
	c.Check(restartTask, Equals, restartSystemToRunModeTask.ID())

	// restart-system-to-run-mode has a pre-req of install-device
	waitTasks = restartSystemToRunModeTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Assert(waitTasks[0].ID(), Equals, installDevice.ID())

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	c.Assert(hooksCalled, HasLen, 1)
	c.Assert(hooksCalled[0].HookName(), Equals, "install-device")
}

func (s *deviceMgrInstallModeSuite) testInstallWithInstallDeviceHookSnapctlReboot(c *C, arg string, rst restart.RestartType) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		c.Assert(ctx.HookName(), Equals, "install-device")

		// snapctl reboot --halt
		_, _, err := ctlcmd.Run(ctx, []string{"reboot", arg}, 0)
		return nil, err
	})
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)

	// we did end up requesting the right shutdown
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{rst})
}

func (s *deviceMgrInstallModeSuite) TestInstallWithInstallDeviceHookSnapctlRebootHalt(c *C) {
	s.testInstallWithInstallDeviceHookSnapctlReboot(c, "--halt", restart.RestartSystemHaltNow)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithInstallDeviceHookSnapctlRebootPoweroff(c *C) {
	s.testInstallWithInstallDeviceHookSnapctlReboot(c, "--poweroff", restart.RestartSystemPoweroffNow)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithBrokenInstallDeviceHookUnhappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return []byte("hook exited broken"), fmt.Errorf("hook broken")
	})
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "install-device-hook-content", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `cannot perform the following tasks:
- Run install-device hook \(run hook \"install-device\": hook exited broken\)`)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 3)
	setupRunSystemTask := tasks[0]
	installDevice := tasks[1]
	restartSystemToRunModeTask := tasks[2]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")
	c.Assert(installDevice.Kind(), Equals, "run-hook")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")

	// install-device is in Error state
	c.Assert(installDevice.Status(), Equals, state.ErrorStatus)

	// setup-run-system is in Done (it has no undo handler)
	c.Assert(setupRunSystemTask.Status(), Equals, state.DoneStatus)

	// restart-system-to-run-mode is in Hold
	c.Assert(restartSystemToRunModeTask.Status(), Equals, state.HoldStatus)

	// we didn't request a restart since restartsystemToRunMode didn't run
	c.Check(s.restartRequests, HasLen, 0)

	c.Assert(hooksCalled, HasLen, 1)
	c.Assert(hooksCalled[0].HookName(), Equals, "install-device")
}

func (s *deviceMgrInstallModeSuite) TestInstallSetupRunSystemTaskNoRestarts(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")

	// also set the system as installed so that the install-system change
	// doesn't get automatically added and we can craft our own change with just
	// the setup-run-system task and not with the restart-system-to-run-mode
	// task
	devicestate.SetInstalledRan(s.mgr, true)

	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// make sure there is no install-system change that snuck in underneath us
	installSystem := s.findInstallSystem()
	c.Check(installSystem, IsNil)

	t := s.state.NewTask("setup-run-system", "setup run system")
	chg := s.state.NewChange("install-system", "install the system")
	chg.AddTask(t)

	// now let the change run
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// now we should have the install-system change
	installSystem = s.findInstallSystem()
	c.Check(installSystem, Not(IsNil))
	c.Check(installSystem.Err(), IsNil)

	tasks := installSystem.Tasks()
	c.Assert(tasks, HasLen, 1)
	setupRunSystemTask := tasks[0]

	c.Assert(setupRunSystemTask.Kind(), Equals, "setup-run-system")

	// we did not request a restart (since that is done in restart-system-to-run-mode)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallModeNotInstallmodeNoChg(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()
	devicestate.SetSystemMode(s.mgr, "")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is *not* created (not in install mode)
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallModeNotClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.state.Lock()
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is *not* created (we're on classic)
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerous(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{tpm: false, bypass: false, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousWithTPM(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousWithTPMBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{tpm: true, bypass: true, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSigned(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{tpm: false, bypass: false, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSignedWithTPM(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSignedBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "signed", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecured(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{tpm: false, bypass: false, encrypt: false})
	c.Assert(err, ErrorMatches, "(?s).*cannot encrypt device storage as mandated by model grade secured:.*TPM not available.*")
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredWithTPM(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousEncryptionWithTPMNoTrustedAssets(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: false,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallDangerousNoEncryptionWithTrustedAssets(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "dangerous", encTestCase{
		tpm: false, bypass: false, encrypt: false, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredWithTPMAndSave(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)
	c.Check(filepath.Join(boot.InstallHostFDEDataDir, "ubuntu-save.key"), testutil.FileEquals, []byte(saveKey))
	marker, err := ioutil.ReadFile(filepath.Join(boot.InstallHostFDEDataDir, "marker"))
	c.Assert(err, IsNil)
	c.Check(marker, HasLen, 32)
	c.Check(filepath.Join(boot.InstallHostFDESaveDir, "marker"), testutil.FileEquals, marker)
}

func (s *deviceMgrInstallModeSuite) TestInstallSecuredBypassEncryption(c *C) {
	err := s.doRunChangeTestWithEncryption(c, "secured", encTestCase{tpm: false, bypass: true, encrypt: false})
	c.Assert(err, ErrorMatches, "(?s).*cannot encrypt device storage as mandated by model grade secured:.*TPM not available.*")
}

func (s *deviceMgrInstallModeSuite) TestInstallBootloaderVarSetFails(c *C) {
	restore := devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Check(options.EncryptionType, Equals, secboot.EncryptionTypeNone)
		// no keys set
		return &install.InstalledSystemSideData{}, nil
	})
	defer restore()

	restore = devicestate.MockBootEnsureNextBootToRunMode(func(systemLabel string) error {
		c.Check(systemLabel, Equals, "1234")
		// no keys set
		return fmt.Errorf("bootloader goes boom")
	})
	defer restore()

	restore = devicestate.MockSecbootCheckTPMKeySealingSupported(func() error { return fmt.Errorf("no encrypted soup for you") })
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\nrecovery_system=1234"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `cannot perform the following tasks:
- Ensure next boot to run mode \(bootloader goes boom\)`)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) testInstallEncryptionValidityChecks(c *C, errMatch string) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockSecbootCheckTPMKeySealingSupported(func() error { return nil })
	defer restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, errMatch)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallEncryptionValidityChecksNoKeys(c *C) {
	restore := devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Check(options.EncryptionType, Equals, secboot.EncryptionTypeLUKS)
		// no keys set
		return &install.InstalledSystemSideData{}, nil
	})
	defer restore()
	s.testInstallEncryptionValidityChecks(c, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(internal error: system encryption keys are unset\)`)
}

func (s *deviceMgrInstallModeSuite) TestInstallEncryptionValidityChecksNoSystemDataKey(c *C) {
	restore := devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		c.Check(options.EncryptionType, Equals, secboot.EncryptionTypeLUKS)
		// no keys set
		return &install.InstalledSystemSideData{
			// empty map
			KeyForRole: map[string]keys.EncryptionKey{},
		}, nil
	})
	defer restore()
	s.testInstallEncryptionValidityChecks(c, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(internal error: system encryption keys are unset\)`)
}

func (s *deviceMgrInstallModeSuite) mockInstallModeChange(c *C, modelGrade, gadgetDefaultsYaml string) *asserts.Model {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	s.state.Lock()
	mockModel := s.makeMockInstallModel(c, modelGrade)
	s.makeMockInstalledPcGadget(c, "", gadgetDefaultsYaml)
	s.state.Unlock()
	c.Check(mockModel.Grade(), Equals, asserts.ModelGrade(modelGrade))

	restore = devicestate.MockBootMakeSystemRunnable(func(model *asserts.Model, bootWith *boot.BootableSet, seal *boot.TrustedAssetsInstallObserver) error {
		return nil
	})
	defer restore()

	modeenv := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191218",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)
	devicestate.SetSystemMode(s.mgr, "install")

	// normally done by snap-bootstrap
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)

	s.settle(c)

	return mockModel
}

func (s *deviceMgrInstallModeSuite) TestInstallModeRunSysconfig(c *C) {
	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system change is created
	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)

	// and sysconfig.ConfigureTargetSystem was run exactly once
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  boot.InstallHostWritableDir,
			GadgetDir:      filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})

	// and the special dirs in _writable_defaults were created
	for _, dir := range []string{"/etc/udev/rules.d/", "/etc/modules-load.d/", "/etc/modprobe.d/"} {
		fullDir := filepath.Join(sysconfig.WritableDefaultsDir(boot.InstallHostWritableDir), dir)
		c.Assert(fullDir, testutil.FilePresent)
	}
}

func (s *deviceMgrInstallModeSuite) TestInstallModeRunSysconfigErr(c *C) {
	s.ConfigureTargetSystemErr = fmt.Errorf("error from sysconfig.ConfigureTargetSystem")
	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	// the install-system was run but errorred as specified in the above mock
	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(error from sysconfig.ConfigureTargetSystem\)`)
	// and sysconfig.ConfigureTargetSystem was run exactly once
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  boot.InstallHostWritableDir,
			GadgetDir:      filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeSupportsCloudInitInDangerous(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = ioutil.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	s.mockInstallModeChange(c, "dangerous", "")

	// and did tell sysconfig about the cloud-init files
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  true,
			CloudInitSrcDir: filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d"),
			TargetRootDir:   boot.InstallHostWritableDir,
			GadgetDir:       filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeSupportsCloudInitGadgetAndSeedConfigSigned(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = ioutil.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	// we also have gadget cloud init too
	gadgetDir := filepath.Join(dirs.SnapMountDir, "pc/1/")
	err = os.MkdirAll(gadgetDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), nil, 0644)
	c.Assert(err, IsNil)

	s.mockInstallModeChange(c, "signed", "")

	// sysconfig is told about both configs
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  true,
			TargetRootDir:   boot.InstallHostWritableDir,
			GadgetDir:       filepath.Join(dirs.SnapMountDir, "pc/1/"),
			CloudInitSrcDir: cloudCfg,
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeSupportsCloudInitBothGadgetAndUbuntuSeedDangerous(c *C) {
	// pretend we have a cloud-init config on the seed partition
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = ioutil.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	// we also have gadget cloud init too
	gadgetDir := filepath.Join(dirs.SnapMountDir, "pc/1/")
	err = os.MkdirAll(gadgetDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), nil, 0644)
	c.Assert(err, IsNil)

	s.mockInstallModeChange(c, "dangerous", "")

	// and did tell sysconfig about the cloud-init files
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  true,
			CloudInitSrcDir: filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d"),
			TargetRootDir:   boot.InstallHostWritableDir,
			GadgetDir:       filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeSignedNoUbuntuSeedCloudInit(c *C) {
	// pretend we have no cloud-init config anywhere
	s.mockInstallModeChange(c, "signed", "")

	// we didn't pass any cloud-init src dir but still left cloud-init enabled
	// if for example a CI-DATA USB drive was provided at runtime
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  boot.InstallHostWritableDir,
			GadgetDir:      filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeSecuredGadgetCloudConfCloudInit(c *C) {
	// pretend we have a cloud.conf from the gadget
	gadgetDir := filepath.Join(dirs.SnapMountDir, "pc/1/")
	err := os.MkdirAll(gadgetDir, 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetDir, "cloud.conf"), nil, 0644)
	c.Assert(err, IsNil)

	err = s.doRunChangeTestWithEncryption(c, "secured", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)

	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  boot.InstallHostWritableDir,
			GadgetDir:      filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeSecuredNoUbuntuSeedCloudInit(c *C) {
	// pretend we have a cloud-init config on the seed partition with some files
	cloudCfg := filepath.Join(boot.InitramfsUbuntuSeedDir, "data/etc/cloud/cloud.cfg.d")
	err := os.MkdirAll(cloudCfg, 0755)
	c.Assert(err, IsNil)
	for _, mockCfg := range []string{"foo.cfg", "bar.cfg"} {
		err = ioutil.WriteFile(filepath.Join(cloudCfg, mockCfg), []byte(fmt.Sprintf("%s config", mockCfg)), 0644)
		c.Assert(err, IsNil)
	}

	err = s.doRunChangeTestWithEncryption(c, "secured", encTestCase{
		tpm: true, bypass: false, encrypt: true, trustedBootloader: true,
	})
	c.Assert(err, IsNil)

	// we did tell sysconfig about the ubuntu-seed cloud config dir because it
	// exists, but it is up to sysconfig to use the model to determine to ignore
	// the files
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit:  false,
			TargetRootDir:   boot.InstallHostWritableDir,
			GadgetDir:       filepath.Join(dirs.SnapMountDir, "pc/1/"),
			CloudInitSrcDir: cloudCfg,
		},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeWritesModel(c *C) {
	// pretend we have a cloud-init config on the seed partition
	model := s.mockInstallModeChange(c, "dangerous", "")

	var buf bytes.Buffer
	err := asserts.NewEncoder(&buf).Encode(model)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// and was run successfully
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)

	c.Check(filepath.Join(boot.InitramfsUbuntuBootDir, "device/model"), testutil.FileEquals, buf.String())
}

func (s *deviceMgrInstallModeSuite) testInstallGadgetNoSave(c *C) {
	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	info, err := snapstate.CurrentInfo(s.state, "pc")
	c.Assert(err, IsNil)
	// replace gadget yaml with one that has no ubuntu-save
	c.Assert(uc20gadgetYaml, Not(testutil.Contains), "ubuntu-save")
	err = ioutil.WriteFile(filepath.Join(info.MountDir(), "meta/gadget.yaml"), []byte(uc20gadgetYaml), 0644)
	c.Assert(err, IsNil)
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithEncryptionValidatesGadgetErr(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, fmt.Errorf("unexpected call")
	})
	defer restore()

	// pretend we have a TPM
	restore = devicestate.MockSecbootCheckTPMKeySealingSupported(func() error { return nil })
	defer restore()

	s.testInstallGadgetNoSave(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), ErrorMatches, `(?ms)cannot perform the following tasks:
- Setup system for run mode \(cannot use gadget: gadget does not support encrypted data: required partition with system-save role is missing\)`)
	// no restart request on failure
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestInstallWithoutEncryptionValidatesGadgetWithoutSaveHappy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	// pretend we have a TPM
	restore = devicestate.MockSecbootCheckTPMKeySealingSupported(func() error { return fmt.Errorf("TPM2 not available") })
	defer restore()

	s.testInstallGadgetNoSave(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)
	c.Check(s.restartRequests, HasLen, 1)
}

func (s *deviceMgrInstallModeSuite) TestInstallCheckEncrypted(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	mockModel := s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: mockModel}

	for _, tc := range []struct {
		hasFDESetupHook      bool
		fdeSetupHookFeatures string

		hasTPM         bool
		encryptionType secboot.EncryptionType
	}{
		// unhappy: no tpm, no hook
		{false, "[]", false, secboot.EncryptionTypeNone},
		// happy: either tpm or hook or both
		{false, "[]", true, secboot.EncryptionTypeLUKS},
		{true, "[]", false, secboot.EncryptionTypeLUKS},
		{true, "[]", true, secboot.EncryptionTypeLUKS},
		// happy but device-setup hook
		{true, `["device-setup"]`, true, secboot.EncryptionTypeDeviceSetupHook},
	} {
		hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
			ctx.Lock()
			defer ctx.Unlock()
			ctx.Set("fde-setup-result", []byte(fmt.Sprintf(`{"features":%s}`, tc.fdeSetupHookFeatures)))
			return nil, nil
		}
		rhk := hookstate.MockRunHook(hookInvoke)
		defer rhk()

		if tc.hasFDESetupHook {
			makeInstalledMockKernelSnap(c, st, kernelYamlWithFdeSetup)
		} else {
			makeInstalledMockKernelSnap(c, st, kernelYamlNoFdeSetup)
		}
		restore := devicestate.MockSecbootCheckTPMKeySealingSupported(func() error {
			if tc.hasTPM {
				return nil
			}
			return fmt.Errorf("tpm says no")
		})
		defer restore()

		encryptionType, err := devicestate.DeviceManagerCheckEncryption(s.mgr, st, deviceCtx)
		c.Assert(err, IsNil)
		c.Check(encryptionType, Equals, tc.encryptionType, Commentf("%v", tc))
	}
}

func (s *deviceMgrInstallModeSuite) TestInstallCheckEncryptedStorageSafety(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := devicestate.MockSecbootCheckTPMKeySealingSupported(func() error { return nil })
	defer restore()

	var testCases = []struct {
		grade, storageSafety string

		expectedEncryption bool
	}{
		// we don't test unset here because the assertion assembly
		// will ensure it has a default
		{"dangerous", "prefer-unencrypted", false},
		{"dangerous", "prefer-encrypted", true},
		{"dangerous", "encrypted", true},
		{"signed", "prefer-unencrypted", false},
		{"signed", "prefer-encrypted", true},
		{"signed", "encrypted", true},
		// secured+prefer-{,un}encrypted is an error at the
		// assertion level already so cannot be tested here
		{"secured", "encrypted", true},
	}
	for _, tc := range testCases {
		mockModel := s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
			"display-name":   "my model",
			"architecture":   "amd64",
			"base":           "core20",
			"grade":          tc.grade,
			"storage-safety": tc.storageSafety,
			"snaps": []interface{}{
				map[string]interface{}{
					"name":            "pc-kernel",
					"id":              pcKernelSnapID,
					"type":            "kernel",
					"default-channel": "20",
				},
				map[string]interface{}{
					"name":            "pc",
					"id":              pcSnapID,
					"type":            "gadget",
					"default-channel": "20",
				}},
		})
		deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: mockModel}

		encryptionType, err := devicestate.DeviceManagerCheckEncryption(s.mgr, s.state, deviceCtx)
		c.Assert(err, IsNil)
		encrypt := (encryptionType != secboot.EncryptionTypeNone)
		c.Check(encrypt, Equals, tc.expectedEncryption)
	}
}

func (s *deviceMgrInstallModeSuite) TestInstallCheckEncryptedErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := devicestate.MockSecbootCheckTPMKeySealingSupported(func() error { return fmt.Errorf("tpm says no") })
	defer restore()

	var testCases = []struct {
		grade, storageSafety string

		expectedErr string
	}{
		// we don't test unset here because the assertion assembly
		// will ensure it has a default
		{
			"dangerous", "encrypted",
			"cannot encrypt device storage as mandated by encrypted storage-safety model option: tpm says no",
		}, {
			"signed", "encrypted",
			"cannot encrypt device storage as mandated by encrypted storage-safety model option: tpm says no",
		}, {
			"secured", "",
			"cannot encrypt device storage as mandated by model grade secured: tpm says no",
		}, {
			"secured", "encrypted",
			"cannot encrypt device storage as mandated by model grade secured: tpm says no",
		},
	}
	for _, tc := range testCases {
		mockModel := s.makeModelAssertionInState(c, "my-brand", "my-model", map[string]interface{}{
			"display-name":   "my model",
			"architecture":   "amd64",
			"base":           "core20",
			"grade":          tc.grade,
			"storage-safety": tc.storageSafety,
			"snaps": []interface{}{
				map[string]interface{}{
					"name":            "pc-kernel",
					"id":              pcKernelSnapID,
					"type":            "kernel",
					"default-channel": "20",
				},
				map[string]interface{}{
					"name":            "pc",
					"id":              pcSnapID,
					"type":            "gadget",
					"default-channel": "20",
				}},
		})
		deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: mockModel}
		_, err := devicestate.DeviceManagerCheckEncryption(s.mgr, s.state, deviceCtx)
		c.Check(err, ErrorMatches, tc.expectedErr, Commentf("%s %s", tc.grade, tc.storageSafety))
	}
}

func (s *deviceMgrInstallModeSuite) TestInstallCheckEncryptedFDEHook(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	makeInstalledMockKernelSnap(c, st, kernelYamlWithFdeSetup)

	for _, tc := range []struct {
		hookOutput  string
		expectedErr string

		encryptionType secboot.EncryptionType
	}{
		// invalid json
		{"xxx", `cannot parse hook output "xxx": invalid character 'x' looking for beginning of value`, secboot.EncryptionTypeNone},
		// no output is invalid
		{"", `cannot parse hook output "": unexpected end of JSON input`, secboot.EncryptionTypeNone},
		// specific error
		{`{"error":"failed"}`, `cannot use hook: it returned error: failed`, secboot.EncryptionTypeNone},
		{`{}`, `cannot use hook: neither "features" nor "error" returned`, secboot.EncryptionTypeNone},
		// valid
		{`{"features":[]}`, "", secboot.EncryptionTypeLUKS},
		{`{"features":["a"]}`, "", secboot.EncryptionTypeLUKS},
		{`{"features":["a","b"]}`, "", secboot.EncryptionTypeLUKS},
		// features must be list of strings
		{`{"features":[1]}`, `cannot parse hook output ".*": json: cannot unmarshal number into Go struct.*`, secboot.EncryptionTypeNone},
		{`{"features":1}`, `cannot parse hook output ".*": json: cannot unmarshal number into Go struct.*`, secboot.EncryptionTypeNone},
		{`{"features":"1"}`, `cannot parse hook output ".*": json: cannot unmarshal string into Go struct.*`, secboot.EncryptionTypeNone},
		// valid and switches to "device-setup"
		{`{"features":["device-setup"]}`, "", secboot.EncryptionTypeDeviceSetupHook},
		{`{"features":["a","device-setup","b"]}`, "", secboot.EncryptionTypeDeviceSetupHook},
	} {
		hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
			ctx.Lock()
			defer ctx.Unlock()
			ctx.Set("fde-setup-result", []byte(tc.hookOutput))
			return nil, nil
		}
		rhk := hookstate.MockRunHook(hookInvoke)
		defer rhk()

		et, err := devicestate.DeviceManagerCheckFDEFeatures(s.mgr, st)
		if tc.expectedErr != "" {
			c.Check(err, ErrorMatches, tc.expectedErr, Commentf("%v", tc))
		} else {
			c.Check(err, IsNil, Commentf("%v", tc))
			c.Check(et, Equals, tc.encryptionType, Commentf("%v", tc))
		}
	}
}

var checkEncryptionModelHeaders = map[string]interface{}{
	"display-name": "my model",
	"architecture": "amd64",
	"base":         "core20",
	"grade":        "dangerous",
	"snaps": []interface{}{
		map[string]interface{}{
			"name":            "pc-kernel",
			"id":              pcKernelSnapID,
			"type":            "kernel",
			"default-channel": "20",
		},
		map[string]interface{}{
			"name":            "pc",
			"id":              pcSnapID,
			"type":            "gadget",
			"default-channel": "20",
		}},
}

func (s *deviceMgrInstallModeSuite) TestInstallCheckEncryptedErrorsLogsTPM(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := devicestate.MockSecbootCheckTPMKeySealingSupported(func() error {
		return fmt.Errorf("tpm says no")
	})
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	mockModel := s.makeModelAssertionInState(c, "my-brand", "my-model", checkEncryptionModelHeaders)
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: mockModel}
	_, err := devicestate.DeviceManagerCheckEncryption(s.mgr, s.state, deviceCtx)
	c.Check(err, IsNil)
	c.Check(logbuf.String(), Matches, "(?s).*: not encrypting device storage as checking TPM gave: tpm says no\n")
}

func (s *deviceMgrInstallModeSuite) TestInstallCheckEncryptedErrorsLogsHook(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	mockModel := s.makeModelAssertionInState(c, "my-brand", "my-model", checkEncryptionModelHeaders)
	// mock kernel installed but no hook or handle so checkEncryption
	// will fail
	makeInstalledMockKernelSnap(c, s.state, kernelYamlWithFdeSetup)

	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: mockModel}
	_, err := devicestate.DeviceManagerCheckEncryption(s.mgr, s.state, deviceCtx)
	c.Check(err, IsNil)
	c.Check(logbuf.String(), Matches, "(?s).*: not encrypting device storage as querying kernel fde-setup hook did not succeed:.*\n")
}

func (s *deviceMgrInstallModeSuite) TestInstallHappyLogfiles(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallRun(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, _ gadget.ContentObserver, _ timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	mockedSnapCmd := testutil.MockCommand(c, "snap", `
echo "mock output of: $(basename "$0") $*"
`)
	defer mockedSnapCmd.Restore()

	err := ioutil.WriteFile(filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/modeenv"),
		[]byte("mode=install\n"), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	// pretend we are seeding
	chg := s.state.NewChange("seed", "just for testing")
	chg.AddTask(s.state.NewTask("test-task", "the change needs a task"))
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "install")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Check(installSystem.Err(), IsNil)
	c.Check(s.restartRequests, HasLen, 1)

	// logs are created
	c.Check(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/install-mode.log.gz"), testutil.FilePresent)
	timingsPath := filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/install-timings.txt.gz")
	c.Check(timingsPath, testutil.FilePresent)

	f, err := os.Open(timingsPath)
	c.Assert(err, IsNil)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	c.Assert(err, IsNil)
	content, err := ioutil.ReadAll(gz)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, `---- Output of: snap changes
mock output of: snap changes

---- Output of snap debug timings --ensure=seed
mock output of: snap debug timings --ensure=seed

---- Output of snap debug timings --ensure=install-system
mock output of: snap debug timings --ensure=install-system
`)

	// and the right commands are run
	c.Check(mockedSnapCmd.Calls(), DeepEquals, [][]string{
		{"snap", "changes"},
		{"snap", "debug", "timings", "--ensure=seed"},
		{"snap", "debug", "timings", "--ensure=install-system"},
	})
}

func (s *deviceMgrInstallModeSuite) TestInstallModeWritesTimesyncdClockHappy(c *C) {
	now := time.Now()
	restore := devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	clockTsInSrc := filepath.Join(dirs.GlobalRootDir, "/var/lib/systemd/timesync/clock")
	c.Assert(os.MkdirAll(filepath.Dir(clockTsInSrc), 0755), IsNil)
	c.Assert(ioutil.WriteFile(clockTsInSrc, nil, 0644), IsNil)
	// a month old timestamp file
	c.Assert(os.Chtimes(clockTsInSrc, now.AddDate(0, -1, 0), now.AddDate(0, -1, 0)), IsNil)

	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// installation was successful
	c.Check(installSystem.Err(), IsNil)
	c.Check(installSystem.Status(), Equals, state.DoneStatus)

	clockTsInDst := filepath.Join(boot.InstallHostWritableDir, "/var/lib/systemd/timesync/clock")
	fi, err := os.Stat(clockTsInDst)
	c.Assert(err, IsNil)
	c.Check(fi.ModTime().Round(time.Second), Equals, now.Round(time.Second))
	c.Check(fi.Size(), Equals, int64(0))
}

func (s *deviceMgrInstallModeSuite) TestInstallModeWritesTimesyncdClockErr(c *C) {
	now := time.Now()
	restore := devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	if os.Geteuid() == 0 {
		c.Skip("the test cannot be executed by the root user")
	}

	clockTsInSrc := filepath.Join(dirs.GlobalRootDir, "/var/lib/systemd/timesync/clock")
	c.Assert(os.MkdirAll(filepath.Dir(clockTsInSrc), 0755), IsNil)
	c.Assert(ioutil.WriteFile(clockTsInSrc, nil, 0644), IsNil)

	timesyncDirInDst := filepath.Join(boot.InstallHostWritableDir, "/var/lib/systemd/timesync/")
	c.Assert(os.MkdirAll(timesyncDirInDst, 0755), IsNil)
	c.Assert(os.Chmod(timesyncDirInDst, 0000), IsNil)
	defer os.Chmod(timesyncDirInDst, 0755)

	s.mockInstallModeChange(c, "dangerous", "")

	s.state.Lock()
	defer s.state.Unlock()

	installSystem := s.findInstallSystem()
	c.Assert(installSystem, NotNil)

	// install failed copying the timestamp
	c.Check(installSystem.Err(), ErrorMatches, `(?s).*\(cannot seed timesyncd clock: cannot copy clock:.*Permission denied.*`)
	c.Check(installSystem.Status(), Equals, state.ErrorStatus)
}

type resetTestCase struct {
	noSave bool
	tpm    bool
}

func (s *deviceMgrInstallModeSuite) doRunFactoryResetChange(c *C, model *asserts.Model, tc resetTestCase) error {
	restore := release.MockOnClassic(false)
	defer restore()

	// inject trusted keys
	defer sysdb.InjectTrusted([]asserts.Assertion{s.storeSigning.TrustedKey})()

	var brGadgetRoot, brDevice string
	var brOpts install.Options
	var installFactoryResetCalled int
	var installSealingObserver gadget.ContentObserver
	restore = devicestate.MockInstallFactoryReset(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, obs gadget.ContentObserver, pertTimings timings.Measurer) (*install.InstalledSystemSideData, error) {
		// ensure we can grab the lock here, i.e. that it's not taken
		s.state.Lock()
		s.state.Unlock()

		c.Check(mod.Grade(), Equals, model.Grade())

		brGadgetRoot = gadgetRoot
		brDevice = device
		brOpts = options
		installFactoryResetCalled++
		// TODO encryption
		return &install.InstalledSystemSideData{}, nil
	})
	defer restore()

	restore = devicestate.MockSecbootCheckTPMKeySealingSupported(func() error {
		if tc.tpm {
			return nil
		} else {
			return fmt.Errorf("TPM not available")
		}
	})
	defer restore()

	s.state.Lock()
	s.makeMockInstalledPcGadget(c, "", "")
	s.state.Unlock()

	bootMakeBootableCalled := 0
	restore = devicestate.MockBootMakeSystemRunnable(func(makeRunnableModel *asserts.Model, bootWith *boot.BootableSet, seal *boot.TrustedAssetsInstallObserver) error {
		c.Check(makeRunnableModel, DeepEquals, model)
		c.Check(bootWith.KernelPath, Matches, ".*/var/lib/snapd/snaps/pc-kernel_1.snap")
		c.Check(bootWith.BasePath, Matches, ".*/var/lib/snapd/snaps/core20_2.snap")
		c.Check(bootWith.RecoverySystemDir, Matches, "/systems/20191218")
		c.Check(bootWith.UnpackedGadgetDir, Equals, filepath.Join(dirs.SnapMountDir, "pc/1"))
		c.Check(seal, IsNil)
		bootMakeBootableCalled++
		return nil
	})
	defer restore()

	if !tc.noSave {
		restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir))
		defer restore()
	}

	modeenv := boot.Modeenv{
		Mode:           "factory-reset",
		RecoverySystem: "20191218",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)
	devicestate.SetSystemMode(s.mgr, "factory-reset")

	// normally done by snap-bootstrap when booting info factory-reset
	err := os.MkdirAll(boot.InitramfsUbuntuBootDir, 0755)
	c.Assert(err, IsNil)
	if !tc.noSave {
		// since there is no save, there is no mount and no target
		// directory created by systemd-mount
		err = os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
		c.Assert(err, IsNil)
	}

	s.settle(c)

	// the factory-reset change is created
	s.state.Lock()
	defer s.state.Unlock()
	factoryReset := s.findFactoryReset()
	c.Assert(factoryReset, NotNil)

	// and was run successfully
	if err := factoryReset.Err(); err != nil {
		// we failed, no further checks needed
		return err
	}

	c.Assert(factoryReset.Status(), Equals, state.DoneStatus)

	c.Assert(installFactoryResetCalled, Equals, 1)
	c.Assert(brGadgetRoot, Equals, filepath.Join(dirs.SnapMountDir, "/pc/1"))
	c.Assert(brDevice, Equals, "")
	c.Assert(brOpts, DeepEquals, install.Options{
		Mount: true,
	})
	c.Assert(installSealingObserver, IsNil)
	c.Assert(bootMakeBootableCalled, Equals, 1)
	c.Assert(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	return nil
}

func makeDeviceSerialAssertionInDir(c *C, where string, storeStack *assertstest.StoreStack, brands *assertstest.SigningAccounts, model *asserts.Model, key asserts.PrivateKey, serialN string) *asserts.Serial {
	encDevKey, err := asserts.EncodePublicKey(key.PublicKey())
	c.Assert(err, IsNil)
	serial, err := brands.Signing(model.BrandID()).Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            model.BrandID(),
		"model":               model.Model(),
		"serial":              serialN,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": key.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	kp, err := asserts.OpenFSKeypairManager(where)
	c.Assert(err, IsNil)
	c.Assert(kp.Put(key), IsNil)
	bs, err := asserts.OpenFSBackstore(where)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         storeStack.Trusted,
		OtherPredefined: storeStack.Generic,
	})
	c.Assert(err, IsNil)

	b := asserts.NewBatch(nil)
	c.Logf("root key ID: %v", storeStack.RootSigning.KeyID)
	c.Assert(b.Add(storeStack.StoreAccountKey("")), IsNil)
	c.Assert(b.Add(brands.AccountKey(model.BrandID())), IsNil)
	c.Assert(b.Add(brands.Account(model.BrandID())), IsNil)
	c.Assert(b.Add(serial), IsNil)
	c.Assert(b.Add(model), IsNil)
	c.Assert(b.CommitTo(db, nil), IsNil)
	return serial.(*asserts.Serial)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetNoEncryptionHappyFull(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// for debug timinigs
	mockedSnapCmd := testutil.MockCommand(c, "snap", `
echo "mock output of: $(basename "$0") $*"
`)
	defer mockedSnapCmd.Restore()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)
	// and it has some content
	serial := makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
		model, devKey, "serial-1234")

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// verify that the serial assertion has been restored
	assertsInResetSystem := filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/assertions")
	bs, err := asserts.OpenFSBackstore(assertsInResetSystem)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)
	ass, err := db.FindMany(asserts.SerialType, map[string]string{
		"brand-id":            serial.BrandID(),
		"model":               serial.Model(),
		"device-key-sha3-384": serial.DeviceKey().ID(),
	})
	c.Assert(err, IsNil)
	c.Assert(ass, HasLen, 1)
	asSerial, _ := ass[0].(*asserts.Serial)
	c.Assert(asSerial, NotNil)
	c.Assert(asSerial, DeepEquals, serial)

	kp, err := asserts.OpenFSKeypairManager(assertsInResetSystem)
	c.Assert(err, IsNil)
	_, err = kp.Get(serial.DeviceKey().ID())
	// the key will not have been found, as this is a device with ubuntu-save
	// and key is stored on that partition
	c.Assert(asserts.IsKeyNotFound(err), Equals, true)
	// which we verify here
	kpInSave, err := asserts.OpenFSKeypairManager(boot.InstallHostDeviceSaveDir)
	c.Assert(err, IsNil)
	_, err = kpInSave.Get(serial.DeviceKey().ID())
	c.Assert(err, IsNil)

	logsPath := filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/factory-reset-mode.log.gz")
	c.Check(logsPath, testutil.FilePresent)
	timingsPath := filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data/var/log/factory-reset-timings.txt.gz")
	c.Check(timingsPath, testutil.FilePresent)
	// and the right commands are run
	c.Check(mockedSnapCmd.Calls(), DeepEquals, [][]string{
		{"snap", "changes"},
		{"snap", "debug", "timings", "--ensure=seed"},
		{"snap", "debug", "timings", "--ensure=factory-reset"},
	})
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetSerialsWithoutKey(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	kp, err := asserts.OpenFSKeypairManager(boot.InstallHostDeviceSaveDir)
	c.Assert(err, IsNil)
	// generate the serial assertions
	for i := 0; i < 5; i++ {
		key, _ := assertstest.GenerateKey(testKeyLength)
		makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
			model, key, fmt.Sprintf("serial-%d", i))
		// remove the key such that the assert cannot be used
		c.Assert(kp.Delete(key.PublicKey().ID()), IsNil)
	}

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// nothing has been restored in the assertions dir
	matches, err := filepath.Glob(filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/assertions/*/*"))
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetNoSerials(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// nothing has been restored in the assertions dir
	matches, err := filepath.Glob(filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/assertions/*/*"))
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetNoSave(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// no ubuntu-save directory, what makes the whole process behave like reinstall

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err := s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm:    false,
		noSave: true,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// nothing has been restored in the assertions dir as nothing was there
	// to begin with
	matches, err := filepath.Glob(filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/assertions/*/*"))
	c.Assert(err, IsNil)
	c.Assert(matches, HasLen, 0)

	// and we logged why nothing was restored from save
	c.Check(logbuf.String(), testutil.Contains, "not restoring from save, ubuntu-save not mounted")
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetPreviouslyEncrypted(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save and there is an encryption marker file
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde/marker"), nil, 0644)
	c.Assert(err, IsNil)

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		// no TPM
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, ErrorMatches, `(?s).*cannot perform factory reset using different encryption, the original system was encrypted\)`)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetPreviouslyUnencrypted(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save but there is no encryption marker
	err := os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSaveDir, "device/fde"), 0755)
	c.Assert(err, IsNil)

	// no serials, no device keys
	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		// no TPM
		tpm: true,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, ErrorMatches, `(?s).*cannot perform factory reset using different encryption, the original system was unencrypted\)`)
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetSerialManyOneValid(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	kp, err := asserts.OpenFSKeypairManager(boot.InstallHostDeviceSaveDir)
	c.Assert(err, IsNil)
	// generate some invalid the serial assertions
	for i := 0; i < 5; i++ {
		key, _ := assertstest.GenerateKey(testKeyLength)
		makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
			model, key, fmt.Sprintf("serial-%d", i))
		// remove the key such that the assert cannot be used
		c.Assert(kp.Delete(key.PublicKey().ID()), IsNil)
	}
	serial := makeDeviceSerialAssertionInDir(c, boot.InstallHostDeviceSaveDir, s.storeSigning, s.brands,
		model, devKey, "serial-1234")

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// verify that only one serial assertion has been restored
	assertsInResetSystem := filepath.Join(boot.InstallHostWritableDir, "var/lib/snapd/assertions")
	bs, err := asserts.OpenFSBackstore(assertsInResetSystem)
	c.Assert(err, IsNil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       bs,
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)
	ass, err := db.FindMany(asserts.SerialType, map[string]string{
		"brand-id":            serial.BrandID(),
		"model":               serial.Model(),
		"device-key-sha3-384": serial.DeviceKey().ID(),
	})
	c.Assert(err, IsNil)
	c.Assert(ass, HasLen, 1)
	asSerial, _ := ass[0].(*asserts.Serial)
	c.Assert(asSerial, NotNil)
	c.Assert(asSerial, DeepEquals, serial)
}

func (s *deviceMgrInstallModeSuite) findFactoryReset() *state.Change {
	for _, chg := range s.state.Changes() {
		if chg.Kind() == "factory-reset" {
			return chg
		}
	}
	return nil
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetExpectedTasks(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	restore = devicestate.MockInstallFactoryReset(func(mod gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, obs gadget.ContentObserver, pertTimings timings.Measurer) (*install.InstalledSystemSideData, error) {
		return nil, nil
	})
	defer restore()

	m := boot.Modeenv{
		Mode:           "factory-reset",
		RecoverySystem: "1234",
	}
	c.Assert(m.WriteTo(""), IsNil)

	s.state.Lock()
	s.makeMockInstallModel(c, "dangerous")
	s.makeMockInstalledPcGadget(c, "", "")
	devicestate.SetSystemMode(s.mgr, "factory-reset")
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	factoryReset := s.findFactoryReset()
	c.Assert(factoryReset, NotNil)
	c.Check(factoryReset.Err(), IsNil)

	tasks := factoryReset.Tasks()
	c.Assert(tasks, HasLen, 2)
	factoryResetTask := tasks[0]
	restartSystemToRunModeTask := tasks[1]

	c.Assert(factoryResetTask.Kind(), Equals, "factory-reset-run-system")
	c.Assert(restartSystemToRunModeTask.Kind(), Equals, "restart-system-to-run-mode")

	// factory-reset has no pre-reqs
	c.Assert(factoryResetTask.WaitTasks(), HasLen, 0)

	// restart-system-to-run-mode has a pre-req of factory-reset
	waitTasks := restartSystemToRunModeTask.WaitTasks()
	c.Assert(waitTasks, HasLen, 1)
	c.Assert(waitTasks[0].ID(), Equals, factoryResetTask.ID())

	// we did request a restart through restartSystemToRunModeTask
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetRunSysconfig(c *C) {
	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	// and sysconfig.ConfigureTargetSystem was run exactly once
	c.Assert(s.ConfigureTargetSystemOptsPassed, DeepEquals, []*sysconfig.Options{
		{
			AllowCloudInit: true,
			TargetRootDir:  boot.InstallHostWritableDir,
			GadgetDir:      filepath.Join(dirs.SnapMountDir, "pc/1/"),
		},
	})

	// and the special dirs in _writable_defaults were created
	for _, dir := range []string{"/etc/udev/rules.d/", "/etc/modules-load.d/", "/etc/modprobe.d/"} {
		fullDir := filepath.Join(sysconfig.WritableDefaultsDir(boot.InstallHostWritableDir), dir)
		c.Assert(fullDir, testutil.FilePresent)
	}
}

func (s *deviceMgrInstallModeSuite) TestFactoryResetWritesTimesyncdClock(c *C) {
	now := time.Now()
	restore := devicestate.MockTimeNow(func() time.Time { return now })
	defer restore()

	clockTsInSrc := filepath.Join(dirs.GlobalRootDir, "/var/lib/systemd/timesync/clock")
	c.Assert(os.MkdirAll(filepath.Dir(clockTsInSrc), 0755), IsNil)
	c.Assert(ioutil.WriteFile(clockTsInSrc, nil, 0644), IsNil)
	// a month old timestamp file
	c.Assert(os.Chtimes(clockTsInSrc, now.AddDate(0, -1, 0), now.AddDate(0, -1, 0)), IsNil)

	s.state.Lock()
	model := s.makeMockInstallModel(c, "dangerous")
	s.state.Unlock()

	// pretend snap-bootstrap mounted ubuntu-save
	err := os.MkdirAll(boot.InitramfsUbuntuSaveDir, 0755)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	err = s.doRunFactoryResetChange(c, model, resetTestCase{
		tpm: false,
	})
	c.Logf("logs:\n%v", logbuf.String())
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	clockTsInDst := filepath.Join(boot.InstallHostWritableDir, "/var/lib/systemd/timesync/clock")
	fi, err := os.Stat(clockTsInDst)
	c.Assert(err, IsNil)
	c.Check(fi.ModTime().Round(time.Second), Equals, now.Round(time.Second))
	c.Check(fi.Size(), Equals, int64(0))
}
