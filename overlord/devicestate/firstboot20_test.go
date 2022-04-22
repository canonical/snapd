// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type firstBoot20Suite struct {
	firstBootBaseTest

	extraSnapYaml map[string]string

	// TestingSeed20 helps populating seeds (it provides
	// MakeAssertedSnap, MakeSeed) for tests.
	*seedtest.TestingSeed20
}

var (
	allGrades = []asserts.ModelGrade{
		asserts.ModelDangerous,
	}
)

var _ = Suite(&firstBoot20Suite{})

func (s *firstBoot20Suite) SetUpTest(c *C) {
	s.extraSnapYaml = make(map[string]string)

	s.TestingSeed20 = &seedtest.TestingSeed20{}

	s.setupBaseTest(c, &s.TestingSeed20.SeedSnaps)

	// don't start the overlord here so that we can mock different modeenvs
	// later, which is needed by devicestart manager startup with uc20 booting

	s.SeedDir = dirs.SnapSeedDir

	// mock the snap mapper as snapd here
	s.AddCleanup(ifacestate.MockSnapMapper(&ifacestate.CoreSnapdSystemMapper{}))

	r := release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	s.AddCleanup(r)

	// make sure we don't call these by accident
	s.AddCleanup(devicestate.MockOsutilAddUser(func(name string, opts *osutil.AddUserOptions) error {
		c.Fatalf("unexpected add user %q call", name)
		return fmt.Errorf("unexpected add user %q call", name)
	}))
	s.AddCleanup(devicestate.MockOsutilDelUser(func(name string, opts *osutil.DelUserOptions) error {
		c.Fatalf("unexpected del user %q call", name)
		return fmt.Errorf("unexpected del user %q call", name)
	}))
}

func (s *firstBoot20Suite) snapYaml(snp string) string {
	if yml, ok := seedtest.SampleSnapYaml[snp]; ok {
		return yml
	}
	return s.extraSnapYaml[snp]
}

func (s *firstBoot20Suite) setupCore20Seed(c *C, sysLabel string, modelGrade asserts.ModelGrade, extraGadgetYaml string, extraSnaps ...string) *asserts.Model {
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

	gadgetYaml += extraGadgetYaml

	makeSnap := func(yamlKey string) {
		var files [][]string
		if yamlKey == "pc=20" {
			files = append(files, []string{"meta/gadget.yaml", gadgetYaml})
		}
		s.MakeAssertedSnap(c, s.snapYaml(yamlKey), files, snap.R(1), "canonical", s.StoreSigning.Database)
	}

	makeSnap("snapd")
	makeSnap("pc-kernel=20")
	makeSnap("core20")
	makeSnap("pc=20")
	for _, sn := range extraSnaps {
		makeSnap(sn)
	}

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        string(modelGrade),
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   s.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core20",
				"id":   s.AssertedSnapID("core20"),
				"type": "base",
			},
		},
	}

	for _, sn := range extraSnaps {
		name, channel := splitSnapNameWithChannel(sn)
		model["snaps"] = append(model["snaps"].([]interface{}), map[string]interface{}{
			"name":            name,
			"type":            "app",
			"id":              s.AssertedSnapID(name),
			"default-channel": channel,
		})
	}

	return s.MakeSeed(c, sysLabel, "my-brand", "my-model", model, nil)
}

func splitSnapNameWithChannel(sn string) (name, channel string) {
	nameParts := strings.SplitN(sn, "=", 2)
	name = nameParts[0]
	channel = ""
	if len(nameParts) == 2 {
		channel = nameParts[1]
	}
	return name, channel
}

func stripSnapNamesWithChannels(snaps []string) []string {
	names := []string{}
	for _, sn := range snaps {
		name, _ := splitSnapNameWithChannel(sn)
		names = append(names, name)
	}
	return names
}

func checkSnapstateDevModeFlags(c *C, tsAll []*state.TaskSet, snapsWithDevModeFlag ...string) {
	allDevModeSnaps := stripSnapNamesWithChannels(snapsWithDevModeFlag)

	// XXX: mostly same code from checkOrder helper in firstboot_test.go, maybe
	// combine someday?
	matched := 0
	var prevTask *state.Task
	for i, ts := range tsAll {
		task0 := ts.Tasks()[0]
		waitTasks := task0.WaitTasks()
		if i == 0 {
			c.Check(waitTasks, HasLen, 0)
		} else {
			c.Check(waitTasks, testutil.Contains, prevTask)
		}
		prevTask = task0
		if task0.Kind() != "prerequisites" {
			continue
		}
		snapsup, err := snapstate.TaskSnapSetup(task0)
		c.Assert(err, IsNil, Commentf("%#v", task0))
		if strutil.ListContains(allDevModeSnaps, snapsup.InstanceName()) {
			c.Assert(snapsup.DevMode, Equals, true)
			matched++
		} else {
			// it should not have DevMode true
			c.Assert(snapsup.DevMode, Equals, false)
		}
	}
	c.Check(matched, Equals, len(snapsWithDevModeFlag))
}

func (s *firstBoot20Suite) earlySetup(c *C, m *boot.Modeenv, modelGrade asserts.ModelGrade, extraGadgetYaml string, extraSnaps ...string) (model *asserts.Model, bloader *bootloadertest.MockExtractedRunKernelImageBootloader) {
	c.Assert(m, NotNil, Commentf("missing modeenv test data"))
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	sysLabel := m.RecoverySystem
	model = s.setupCore20Seed(c, sysLabel, modelGrade, extraGadgetYaml, extraSnaps...)
	// validity check that our returned model has the expected grade
	c.Assert(model.Grade(), Equals, modelGrade)

	bloader = bootloadertest.Mock("mock", c.MkDir()).WithExtractedRunKernelImage()
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	// since we are in runmode, MakeBootable will already have run from install
	// mode, and extracted the kernel assets for the kernel snap into the
	// bootloader, so set the current kernel there
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetEnabledKernel(kernel)
	s.AddCleanup(r)

	return model, bloader
}

func (s *firstBoot20Suite) testPopulateFromSeedCore20Happy(c *C, m *boot.Modeenv, modelGrade asserts.ModelGrade, extraDevModeSnaps ...string) {
	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	model, bloader := s.earlySetup(c, m, modelGrade, "", extraDevModeSnaps...)
	// create overlord and pick up the modeenv
	s.startOverlord(c)

	opts := devicestate.PopulateStateFromSeedOptions{
		Label: m.RecoverySystem,
		Mode:  m.Mode,
	}

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(st, &opts, s.perfTimings)
	c.Assert(err, IsNil)

	snaps := []string{"snapd", "pc-kernel", "core20", "pc"}
	allDevModeSnaps := stripSnapNamesWithChannels(extraDevModeSnaps)
	if len(extraDevModeSnaps) != 0 {
		snaps = append(snaps, allDevModeSnaps...)
	}
	checkOrder(c, tsAll, snaps...)

	// if the model is dangerous check that the devmode snaps in the model have
	// the flag set in snapstate for DevMode confinement
	// XXX: eventually we may need more complicated checks here and for
	// non-dangerous models only specific snaps may have this flag set
	if modelGrade == asserts.ModelDangerous {
		checkSnapstateDevModeFlags(c, tsAll, allDevModeSnaps...)
	}

	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	c.Assert(chg.Err(), IsNil)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	// run change until it wants to restart
	st.Unlock()
	err = s.overlord.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)

	// at this point the system is "restarting", pretend the restart has
	// happened
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	restart.MockPending(st, restart.RestartUnset)
	st.Unlock()
	err = s.overlord.Settle(settleTimeout)
	st.Lock()
	c.Assert(err, IsNil)
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%s", chg.Err()))

	// verify
	f, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	state, err := state.ReadState(nil, f)
	c.Assert(err, IsNil)

	state.Lock()
	defer state.Unlock()
	// check snapd, core20, kernel, gadget
	_, err = snapstate.CurrentInfo(state, "snapd")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "core20")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "pc-kernel")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "pc")
	c.Check(err, IsNil)

	// No kernel extraction happens during seeding, the kernel is already
	// there either from ubuntu-image or from "install" mode.
	c.Check(bloader.ExtractKernelAssetsCalls, HasLen, 0)

	// ensure required flag is set on all essential snaps
	var snapst snapstate.SnapState
	for _, reqName := range []string{"snapd", "core20", "pc-kernel", "pc"} {
		err = snapstate.Get(state, reqName, &snapst)
		c.Assert(err, IsNil)
		c.Assert(snapst.Required, Equals, true, Commentf("required not set for %v", reqName))

		if m.Mode == "run" {
			// also ensure that in run mode none of the snaps are installed as
			// symlinks, they must be copied onto ubuntu-data
			files, err := filepath.Glob(filepath.Join(dirs.SnapBlobDir, reqName+"_*.snap"))
			c.Assert(err, IsNil)
			c.Assert(files, HasLen, 1)
			c.Assert(osutil.IsSymlink(files[0]), Equals, false)
		}
	}

	// the right systemd commands were run
	c.Check(sysdLog, testutil.DeepContains, []string{"start", "usr-lib-snapd.mount"})

	// and ensure state is now considered seeded
	var seeded bool
	err = state.Get("seeded", &seeded)
	c.Assert(err, IsNil)
	c.Check(seeded, Equals, true)

	// check we set seed-time
	var seedTime time.Time
	err = state.Get("seed-time", &seedTime)
	c.Assert(err, IsNil)
	c.Check(seedTime.IsZero(), Equals, false)

	// check that we removed recovery_system from modeenv
	m2, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	if m.Mode == "run" {
		// recovery system is cleared in run mode
		c.Assert(m2.RecoverySystem, Equals, "")
	} else {
		// but kept intact in other modes
		c.Assert(m2.RecoverySystem, Equals, m.RecoverySystem)
	}
	c.Assert(m2.Base, Equals, m.Base)
	c.Assert(m2.Mode, Equals, m.Mode)
	// Note that we don't check CurrentKernels in the modeenv, even though in a
	// real first boot that would also be set here, because setting that is done
	// in the snapstate manager, not the devicestate manager

	// check that the default device ctx has a Modeenv
	dev, err := devicestate.DeviceCtx(s.overlord.State(), nil, nil)
	c.Assert(err, IsNil)
	c.Assert(dev.HasModeenv(), Equals, true)

	// check that we marked the boot successful with bootstate20 methods, namely
	// that we called SetNext, which since it was called on the kernel we
	// already booted from, we should only have checked what the current kernel
	// is

	if m.Mode == "run" {
		// only relevant in run mode

		// the 3 calls here are :
		// * 1 from MarkBootSuccessful() from ensureBootOk() before we restart
		// * 1 from boot.SetNextBoot() from LinkSnap() from doInstall() from InstallPath() from
		//     installSeedSnap() after restart
		// * 1 from boot.GetCurrentBoot() from FinishRestart after restart
		_, numKernelCalls := bloader.GetRunKernelImageFunctionSnapCalls("Kernel")
		c.Assert(numKernelCalls, Equals, 3)
	}
	actual, _ := bloader.GetRunKernelImageFunctionSnapCalls("EnableKernel")
	c.Assert(actual, HasLen, 0)
	actual, _ = bloader.GetRunKernelImageFunctionSnapCalls("DisableTryKernel")
	c.Assert(actual, HasLen, 0)
	actual, _ = bloader.GetRunKernelImageFunctionSnapCalls("EnableTryKernel")
	c.Assert(actual, HasLen, 0)

	var whatseeded []devicestate.SeededSystem
	err = state.Get("seeded-systems", &whatseeded)
	if m.Mode == "run" {
		c.Assert(err, IsNil)
		c.Assert(whatseeded, DeepEquals, []devicestate.SeededSystem{{
			System:    m.RecoverySystem,
			Model:     "my-model",
			BrandID:   "my-brand",
			Revision:  model.Revision(),
			Timestamp: model.Timestamp(),
			SeedTime:  seedTime,
		}})
	} else {
		c.Assert(err, NotNil)
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RunModeDangerousWithDevmode(c *C) {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}
	s.testPopulateFromSeedCore20Happy(c, &m, asserts.ModelDangerous, "test-devmode=20")
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RunMode(c *C) {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade)
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20InstallMode(c *C) {
	m := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191019",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade)
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RecoverMode(c *C) {
	m := boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: "20191020",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade)
	}
}

func (s *firstBoot20Suite) TestLoadDeviceSeedCore20(c *C) {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}

	s.earlySetup(c, &m, "signed", "")

	o, err := overlord.New(nil)
	c.Assert(err, IsNil)
	st := o.State()

	st.Lock()
	defer st.Unlock()

	deviceSeed, err := devicestate.LoadDeviceSeed(st, m.RecoverySystem)
	c.Assert(err, IsNil)

	c.Check(deviceSeed.Model().BrandID(), Equals, "my-brand")
	c.Check(deviceSeed.Model().Model(), Equals, "my-model")
	c.Check(deviceSeed.Model().Base(), Equals, "core20")

	// verify that the model was added
	db := assertstate.DB(st)
	as, err := db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "my-brand",
		"model":    "my-model",
	})
	c.Assert(err, IsNil)
	c.Check(as, DeepEquals, deviceSeed.Model())

	// inconsistent seed request
	_, err = devicestate.LoadDeviceSeed(st, "20210201")
	c.Assert(err, ErrorMatches, `internal error: requested inconsistent device seed: 20210201 \(was 20191018\)`)
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RunModeUserServiceTasks(c *C) {
	// check that this is test is still valid
	// TODO: have a test for an early config option that is not an
	// experimental flag
	c.Assert(features.UserDaemons.IsEnabledWhenUnset(), Equals, false, Commentf("user-daemons is not experimental anymore, this test is not useful anymore"))

	s.extraSnapYaml["user-daemons1"] = `name: user-daemons1
version: 1.0
type: app
base: core20

apps:
  foo:
    daemon: simple
    daemon-scope: user
`
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}

	defaultsGadgetYaml := `
defaults:
   system:
      experimental:
        user-daemons: true
`

	s.earlySetup(c, &m, "signed", defaultsGadgetYaml, "user-daemons1")

	// create a new overlord and pick up the modeenv
	// this overlord will use the proper EarlyConfig implementation
	o, err := overlord.New(nil)
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()
	c.Assert(o.StartUp(), IsNil)

	st := o.State()
	st.Lock()
	defer st.Unlock()

	// early config set the flag to enabled
	tr := config.NewTransaction(st)
	enabled, _ := features.Flag(tr, features.UserDaemons)
	c.Check(enabled, Equals, true)

	opts := devicestate.PopulateStateFromSeedOptions{
		Label: m.RecoverySystem,
		Mode:  m.Mode,
	}

	_, err = devicestate.PopulateStateFromSeedImpl(st, &opts, s.perfTimings)
	c.Assert(err, IsNil)
}

func (s *firstBoot20Suite) TestUsersCreateAutomaticIsAvailableEarly(c *C) {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}

	defaultsGadgetYaml := `
defaults:
   system:
      users:
        create.automatic: false
`

	s.earlySetup(c, &m, "signed", defaultsGadgetYaml)

	// create a new overlord and pick up the modeenv
	// this overlord will use the proper EarlyConfig implementation
	o, err := overlord.New(nil)
	c.Assert(err, IsNil)
	o.InterfaceManager().DisableUDevMonitor()
	c.Assert(o.StartUp(), IsNil)

	st := o.State()
	st.Lock()
	defer st.Unlock()

	// early config in StartUp made the option available already
	tr := config.NewTransaction(st)
	var enabled bool
	err = tr.Get("core", "users.create.automatic", &enabled)
	c.Assert(err, IsNil)
	c.Check(enabled, Equals, false)
}

func (s *firstBoot20Suite) TestAutoImportAssertionsFromSeedSecuredNoAutoImport(c *C) {
	s.testAutoImportAssertionsFromSeedNoUser(c, asserts.ModelSecured, false, 0, false)
}

func (s *firstBoot20Suite) TestAutoImportAssertionsFromSeedDangerousNoAutoImport(c *C) {
	s.testAutoImportAssertionsFromSeedNoUser(c, asserts.ModelDangerous, false, 0, false)
}

func (s *firstBoot20Suite) TestAutoImportAssertionsFromSeedSecured(c *C) {
	s.testAutoImportAssertionsFromSeedNoUser(c, asserts.ModelSecured, true, 0, false)
}

func (s *firstBoot20Suite) TestAutoImportAssertionsFromSeedDangerous(c *C) {
	s.testAutoImportAssertionsFromSeedNoUser(c, asserts.ModelDangerous, true, 1, false)
}

func (s *firstBoot20Suite) TestAutoImportAssertionsFromSeedDangerousAddUserErr(c *C) {
	s.testAutoImportAssertionsFromSeedNoUser(c, asserts.ModelDangerous, true, 1, true)
}

// helper to test handling of auto-import assertion
// grade: grade of model used in the test
// withSUAssertion: if auto import assertion should be present
// addUserCalled: how many times is mocked OsutilAddUser expected to be called
// addUserFail: should mocked OsutilAddUser fail
func (s *firstBoot20Suite) testAutoImportAssertionsFromSeedNoUser(c *C, grade asserts.ModelGrade, withSUAssertion bool, addUserCalled int, addUserFail bool) {
	systemLabel := "20191018"
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: systemLabel,
		Base:           "core20_1.snap",
	}

	mockUserHome := c.MkDir()

	called := 0
	created := map[string]bool{}
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
		called++
		if addUserFail {
			return fmt.Errorf("not-today")
		} else {
			c.Check(username, check.Equals, "guy")
			c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
			c.Check(opts.Sudoer, check.Equals, true)
			c.Check(opts.Password, check.Equals, "$6$salt$hash")
			c.Check(opts.ForcePasswordChange, check.Equals, false)
			created[username] = true
			return nil
		}
	})()

	// make sure we report user as non-existing until created
	defer devicestate.MockUserLookup(func(username string) (*user.User, error) {
		if created[username] {
			return mkUserLookup(mockUserHome)(username)
		}
		return nil, fmt.Errorf("not created yet")
	})()

	s.populateFromSeedCore20(c, &m, withSUAssertion, grade)
	// ensure AddUser was called addUserCalled times
	c.Check(called, check.Equals, addUserCalled)
	// check created user
	if addUserCalled > 0 && !addUserFail {
		c.Check(created["guy"], check.Equals, true)
	} else {
		c.Check(created["guy"], check.Equals, false)
	}
}

// helper to create minimal test seed
func (s *firstBoot20Suite) populateFromSeedCore20(c *C, m *boot.Modeenv, withSUAssertion bool, modelGrade asserts.ModelGrade, extraDevModeSnaps ...string) {
	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	s.earlySetup(c, m, modelGrade, "", extraDevModeSnaps...)
	// create overlord and pick up the modeenv
	s.startOverlord(c)

	opts := devicestate.PopulateStateFromSeedOptions{
		Label: m.RecoverySystem,
		Mode:  m.Mode,
	}

	if withSUAssertion {
		s.writeValidAutoImportAssertion(c, m.RecoverySystem)
	}

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	_, err := devicestate.PopulateStateFromSeedImpl(st, &opts, s.perfTimings)
	c.Assert(err, IsNil)
}

func (s *firstBoot20Suite) writeValidAutoImportAssertion(c *C, sysLabel string) error {
	systemUsers := []map[string]interface{}{goodUser}
	// write system user asseerion to system seed root
	autoImportAssert := filepath.Join(s.SeedDir, "systems", sysLabel, "auto-import.assert")
	f, err := os.OpenFile(autoImportAssert, os.O_CREATE|os.O_WRONLY, 0644)
	c.Assert(err, IsNil)
	defer f.Close()
	enc := asserts.NewEncoder(f)
	c.Assert(enc, NotNil)

	for _, suMap := range systemUsers {
		su, err := s.Brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, IsNil)
		su = su.(*asserts.SystemUser)
		err = enc.Encode(su)
		c.Assert(err, IsNil)
	}

	return nil
}
