// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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
	"strconv"
	"strings"
	"time"

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
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type firstBoot20Suite struct {
	firstBootBaseTest

	extraSnapYaml         map[string]string
	extraSnapModelDetails map[string]map[string]interface{}

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
	s.extraSnapModelDetails = make(map[string]map[string]interface{})

	s.TestingSeed20 = &seedtest.TestingSeed20{}

	s.setupBaseTest(c, &s.TestingSeed20.SeedSnaps)

	// don't start the overlord here so that we can mock different modeenvs
	// later, which is needed by devicestart manager startup with uc20 booting

	s.SeedDir = dirs.SnapSeedDir

	// mock the snap mapper as snapd here
	s.AddCleanup(ifacestate.MockSnapMapper(&ifacestate.CoreSnapdSystemMapper{}))

	r := release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	s.AddCleanup(r)
}

func (s *firstBoot20Suite) snapYaml(snp string) string {
	if yml, ok := seedtest.SampleSnapYaml[snp]; ok {
		return yml
	}
	return s.extraSnapYaml[snp]
}

type core20SeedOptions struct {
	sysLabel        string
	modelGrade      asserts.ModelGrade
	kernelAndGadget bool
	extraGadgetYaml string
	valsets         []string
	withComps       bool
}

func (s *firstBoot20Suite) setupCore20LikeSeed(c *C, opts core20SeedOptions, extraSnaps ...string) *asserts.Model {
	var gadgetYaml string
	if opts.kernelAndGadget {
		gadgetYaml = `
volumes:
    volume-id:
        bootloader: grub
        structure:
        - name: ubuntu-seed
          role: system-seed
          type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
          size: 1G
        - name: ubuntu-boot
          role: system-boot
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          size: 100M
        - name: ubuntu-data
          role: system-data
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          size: 2G
`

		gadgetYaml += opts.extraGadgetYaml
	}

	makeSnap := func(yamlKey string) {
		var files [][]string
		if yamlKey == "pc=20" {
			files = append(files, []string{"meta/gadget.yaml", gadgetYaml})
		} else if yamlKey == "snapd" {
			// XXX make SnapManager.ensureVulnerableSnapConfineVersionsRemovedOnClassic happy
			files = append(files, []string{"/usr/lib/snapd/info", "VERSION=2.55"})
		}
		s.MakeAssertedSnap(c, s.snapYaml(yamlKey), files, snap.R(1), "canonical", s.StoreSigning.Database)
	}

	makeSnap("snapd")
	makeSnap("core20")
	if opts.kernelAndGadget {
		makeSnap("pc-kernel=20")
		makeSnap("pc=20")
	}
	for _, sn := range extraSnaps {
		makeSnap(sn)
	}
	if opts.withComps {
		comRevs := map[string]snap.Revision{
			"comp1": snap.R(22),
			"comp2": snap.R(33),
		}
		s.SeedSnaps.MakeAssertedSnapWithComps(c, seedtest.SampleSnapYaml["required20"], nil,
			snap.R(21), comRevs, "canonical", s.StoreSigning.Database)
	}

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        string(opts.modelGrade),
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

	if release.OnClassic {
		model["classic"] = "true"
		model["distribution"] = "ubuntu"
		if !opts.kernelAndGadget {
			snaps := model["snaps"].([]interface{})
			reducedSnaps := []interface{}{}
			for _, s := range snaps {
				ms := s.(map[string]interface{})
				if ms["type"] == "kernel" || ms["type"] == "gadget" {
					continue
				}
				reducedSnaps = append(reducedSnaps, s)
			}
			model["snaps"] = reducedSnaps
		}
	} else {
		c.Assert(opts.kernelAndGadget, Equals, true)
	}

	for _, sn := range extraSnaps {
		name, channel := splitSnapNameWithChannel(sn)
		snapEntry := map[string]interface{}{
			"name":            name,
			"type":            "app",
			"id":              s.AssertedSnapID(name),
			"default-channel": channel,
		}
		for h, v := range s.extraSnapModelDetails[name] {
			snapEntry[h] = v
		}
		model["snaps"] = append(model["snaps"].([]interface{}), snapEntry)
	}
	if opts.withComps {
		snapWithComps := "required20"
		snapEntry := map[string]interface{}{
			"name":            snapWithComps,
			"type":            "app",
			"id":              s.AssertedSnapID(snapWithComps),
			"default-channel": "latest/stable",
			"components": map[string]interface{}{
				"comp1": "required",
				"comp2": "required",
			},
		}
		model["snaps"] = append(model["snaps"].([]interface{}), snapEntry)
	}

	for _, vs := range opts.valsets {
		keys := strings.Split(vs, "/")
		vsEntry := map[string]interface{}{
			"account-id": keys[0],
			"name":       keys[1],
			"sequence":   keys[2],
			"mode":       "enforce",
		}
		if _, ok := model["validation-sets"]; !ok {
			model["validation-sets"] = []interface{}{vsEntry}
		} else {
			model["validation-sets"] = append(model["validation-sets"].([]interface{}), vsEntry)
		}
	}

	return s.MakeSeed(c, opts.sysLabel, "my-brand", "my-model", model, nil)
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

func (s *firstBoot20Suite) updateModel(c *C, sysLabel string, model *asserts.Model, modelUpdater func(c *C, headers map[string]interface{})) *asserts.Model {
	if modelUpdater != nil {
		hdrs := model.Headers()
		modelUpdater(c, hdrs)
		model = s.Brands.Model(model.BrandID(), model.Model(), hdrs)
		modelFn := filepath.Join(s.SeedDir, "systems", sysLabel, "model")
		seedtest.WriteAssertions(modelFn, model)
	}
	return model
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

func (s *firstBoot20Suite) earlySetup(c *C, m *boot.Modeenv, modelGrade asserts.ModelGrade, extraGadgetYaml string, opts populateFromSeedCore20Opts) (model *asserts.Model, bloader *bootloadertest.MockExtractedRunKernelImageBootloader) {
	c.Assert(m, NotNil, Commentf("missing modeenv test data"))
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	model = s.setupCore20LikeSeed(c, core20SeedOptions{
		sysLabel:        m.RecoverySystem,
		modelGrade:      modelGrade,
		kernelAndGadget: true,
		extraGadgetYaml: extraGadgetYaml,
		withComps:       opts.withComps,
	}, opts.extraDevModeSnaps...)
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

type populateFromSeedCore20Opts struct {
	extraDevModeSnaps []string
	withComps         bool
}

func (s *firstBoot20Suite) testPopulateFromSeedCore20Happy(c *C, m *boot.Modeenv, modelGrade asserts.ModelGrade, opts populateFromSeedCore20Opts) {
	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	model, bloader := s.earlySetup(c, m, modelGrade, "", opts)
	// create overlord and pick up the modeenv
	s.startOverlord(c)

	mgr := s.overlord.DeviceManager()

	c.Check(devicestate.SaveAvailable(mgr), Equals, m.Mode == "run")

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(mgr, s.perfTimings)
	c.Assert(err, IsNil)

	snaps := []string{"snapd", "pc-kernel", "core20", "pc"}
	allDevModeSnaps := stripSnapNamesWithChannels(opts.extraDevModeSnaps)
	if len(opts.extraDevModeSnaps) != 0 {
		snaps = append(snaps, allDevModeSnaps...)
	}
	compsSnap := "required20"
	if opts.withComps && m.Mode == "run" {
		snaps = append(snaps, compsSnap)
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
	if opts.withComps && m.Mode == "run" {
		_, err := snapstate.CurrentInfo(state, compsSnap)
		c.Check(err, IsNil)
		var snapst snapstate.SnapState
		err = snapstate.Get(state, compsSnap, &snapst)
		c.Assert(err, IsNil)
		c.Assert(snapst.Required, Equals, true)
		c.Check(snapst.TrackingChannel, Equals, "latest/stable")

		cref1 := naming.NewComponentRef(compsSnap, "comp1")
		cref2 := naming.NewComponentRef(compsSnap, "comp2")
		cinfos, err := snapst.CurrentComponentInfos()
		c.Assert(err, IsNil)
		c.Assert(len(cinfos), Equals, 2)
		cinfo1, err := snapst.CurrentComponentInfo(cref1)
		c.Assert(err, IsNil)
		c.Assert(cinfo1, DeepEquals,
			snap.NewComponentInfo(cref1, snap.TestComponent, "1.0", "", "", "",
				snap.NewComponentSideInfo(cref1, snap.R(22))))
		cinfo2, err := snapst.CurrentComponentInfo(cref2)
		c.Assert(err, IsNil)
		c.Assert(cinfo2, DeepEquals,
			snap.NewComponentInfo(cref2, snap.TestComponent, "2.0", "", "", "",
				snap.NewComponentSideInfo(cref2, snap.R(33))))
	}

	// No kernel extraction happens during seeding, the kernel is already
	// there either from ubuntu-image or from "install" mode.
	c.Check(bloader.ExtractKernelAssetsCalls, HasLen, 0)

	namesToChannel := make(map[string]string)
	for _, sn := range model.EssentialSnaps() {
		ch, err := channel.Full(sn.DefaultChannel)
		c.Assert(err, IsNil)
		namesToChannel[sn.Name] = ch
	}

	// ensure required flag is set on all essential snaps and all of their
	// channels got set properly
	var snapst snapstate.SnapState
	for _, reqName := range []string{"snapd", "core20", "pc-kernel", "pc"} {
		err = snapstate.Get(state, reqName, &snapst)
		c.Assert(err, IsNil)
		c.Assert(snapst.Required, Equals, true, Commentf("required not set for %v", reqName))

		c.Check(snapst.TrackingChannel, Equals, namesToChannel[reqName])

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
	sysdLogChecker := testutil.DeepContains
	if release.OnClassic {
		sysdLogChecker = Not(testutil.DeepContains)
	}
	c.Check(sysdLog, sysdLogChecker, []string{"start", "usr-lib-snapd.mount"})

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
	c.Assert(dev.IsCoreBoot(), Equals, true)

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

		var defaultRecoverySystem devicestate.DefaultRecoverySystem
		c.Assert(state.Get("default-recovery-system", &defaultRecoverySystem), IsNil)
		c.Check(defaultRecoverySystem, Equals, devicestate.DefaultRecoverySystem{
			System:          m.RecoverySystem,
			Model:           "my-model",
			BrandID:         "my-brand",
			Timestamp:       model.Timestamp(),
			Revision:        model.Revision(),
			TimeMadeDefault: seedTime,
		})
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
	s.testPopulateFromSeedCore20Happy(c, &m, asserts.ModelDangerous,
		populateFromSeedCore20Opts{extraDevModeSnaps: []string{"test-devmode=20"}})
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RunMode(c *C) {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade, populateFromSeedCore20Opts{})
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RunModeWithComps(c *C) {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20241018",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade,
			populateFromSeedCore20Opts{withComps: true})
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20InstallMode(c *C) {
	m := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191019",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade, populateFromSeedCore20Opts{})
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20InstallModeWithComps(c *C) {
	m := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20241018",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade,
			populateFromSeedCore20Opts{withComps: true})
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RecoverMode(c *C) {
	m := boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: "20191020",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade, populateFromSeedCore20Opts{})
	}
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RecoverModeWithComps(c *C) {
	m := boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: "20241018",
		Base:           "core20_1.snap",
	}
	for _, grade := range allGrades {
		s.testPopulateFromSeedCore20Happy(c, &m, grade,
			populateFromSeedCore20Opts{withComps: true})
	}
}

func (s *firstBoot20Suite) TestLoadDeviceSeedCore20(c *C) {
	r := devicestate.MockCreateAllKnownSystemUsers(func(state *state.State, assertDb asserts.RODatabase, model *asserts.Model, serial *asserts.Serial, sudoer bool) ([]*devicestate.CreatedUser, error) {
		err := errors.New("unexpected call to CreateAllSystemUsers")
		c.Error(err)
		return nil, err
	})
	defer r()

	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}

	s.earlySetup(c, &m, "signed", "", populateFromSeedCore20Opts{})

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
	c.Check(deviceSeed.Model().Grade(), Equals, asserts.ModelSigned)

	// verify that the model was added
	db := assertstate.DB(st)
	as, err := db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "my-brand",
		"model":    "my-model",
	})
	c.Assert(err, IsNil)
	c.Check(as, DeepEquals, deviceSeed.Model())
}

func (s *firstBoot20Suite) testProcessAutoImportAssertions(c *C, withAutoImportAssertion bool) error {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}

	s.earlySetup(c, &m, "dangerous", "", populateFromSeedCore20Opts{})

	if withAutoImportAssertion {
		seedtest.WriteValidAutoImportAssertion(c, s.Brands, s.SeedDir, m.RecoverySystem, 0644)
	}

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
	c.Check(deviceSeed.Model().Grade(), Equals, asserts.ModelDangerous)

	commitTo := func(batch *asserts.Batch) error {
		return assertstate.AddBatch(st, batch, nil)
	}
	db := assertstate.DB(st)
	return devicestate.ProcessAutoImportAssertions(st, deviceSeed, db, commitTo)
}

func (s *firstBoot20Suite) TestLoadDeviceSeedCore20DangerousNoAutoImport(c *C) {
	r := devicestate.MockCreateAllKnownSystemUsers(func(state *state.State, assertDb asserts.RODatabase, model *asserts.Model, serial *asserts.Serial, sudoer bool) ([]*devicestate.CreatedUser, error) {
		err := errors.New("unexpected call to CreateAllSystemUsers")
		c.Error(err)
		return nil, err
	})
	defer r()

	err := s.testProcessAutoImportAssertions(c, false)

	c.Check(err.Error(), testutil.Contains, `no such file or directory`)
}

func (s *firstBoot20Suite) TestLoadDeviceSeedCore20DangerousAutoImportUserCreateFail(c *C) {
	var calledcreateAllUsers = false
	r := devicestate.MockCreateAllKnownSystemUsers(func(state *state.State, assertDb asserts.RODatabase, model *asserts.Model, serial *asserts.Serial, sudoer bool) ([]*devicestate.CreatedUser, error) {
		calledcreateAllUsers = true
		return nil, errors.New("User already exists")
	})
	defer r()

	err := s.testProcessAutoImportAssertions(c, true)

	c.Check(calledcreateAllUsers, Equals, true)
	c.Check(err.Error(), testutil.Contains, "User already exists")
}

func (s *firstBoot20Suite) TestLoadDeviceSeedCore20DangerousAutoImport(c *C) {
	var calledcreateAllUsers = false
	r := devicestate.MockCreateAllKnownSystemUsers(func(state *state.State, assertDb asserts.RODatabase, model *asserts.Model, serial *asserts.Serial, sudoer bool) ([]*devicestate.CreatedUser, error) {
		calledcreateAllUsers = true
		var createdUsers []*devicestate.CreatedUser
		return createdUsers, nil
	})
	defer r()

	err := s.testProcessAutoImportAssertions(c, true)

	c.Check(calledcreateAllUsers, Equals, true)
	c.Assert(err, IsNil)
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

	s.earlySetup(c, &m, "signed", defaultsGadgetYaml,
		populateFromSeedCore20Opts{extraDevModeSnaps: []string{"user-daemons1"}})

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

	_, err = devicestate.PopulateStateFromSeedImpl(o.DeviceManager(), s.perfTimings)
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

	s.earlySetup(c, &m, "signed", defaultsGadgetYaml, populateFromSeedCore20Opts{})

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

func (s *firstBoot20Suite) TestPopulateFromSeedClassicWithModesRunMode(c *C) {
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "20.04"})()
	// XXX this shouldn't be needed
	defer release.MockOnClassic(true)()
	c.Assert(release.OnClassic, Equals, true)

	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
		Classic:        true,
	}
	s.testPopulateFromSeedCore20Happy(c, &m, asserts.ModelSigned, populateFromSeedCore20Opts{})
}

func (s *firstBoot20Suite) TestPopulateFromSeedClassicWithModesRunModeNoKernelAndGadget(c *C) {
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "20.04"})()
	// XXX this shouldn't be needed
	defer release.MockOnClassic(true)()
	c.Assert(release.OnClassic, Equals, true)

	m := &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
		Classic:        true,
	}
	modelGrade := asserts.ModelSigned

	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	err := m.WriteTo("")
	c.Assert(err, IsNil)

	model := s.setupCore20LikeSeed(c, core20SeedOptions{
		sysLabel:        m.RecoverySystem,
		modelGrade:      modelGrade,
		kernelAndGadget: false,
	})
	// validity check that our returned model has the expected grade
	c.Assert(model.Grade(), Equals, modelGrade)

	s.startOverlord(c)

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings)
	c.Assert(err, IsNil)

	snaps := []string{"snapd", "core20"}
	checkOrder(c, tsAll, snaps...)

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

	// ensure required flag is set on all essential snaps
	var snapst snapstate.SnapState
	for _, reqName := range []string{"snapd", "core20"} {
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
	c.Check(sysdLog, Not(testutil.DeepContains), []string{"start", "usr-lib-snapd.mount"})

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
	c.Assert(dev.IsCoreBoot(), Equals, false)

	var whatseeded []devicestate.SeededSystem
	err = state.Get("seeded-systems", &whatseeded)
	c.Assert(err, IsNil)
	c.Assert(whatseeded, DeepEquals, []devicestate.SeededSystem{{
		System:    m.RecoverySystem,
		Model:     "my-model",
		BrandID:   "my-brand",
		Revision:  model.Revision(),
		Timestamp: model.Timestamp(),
		SeedTime:  seedTime,
	}})
}

func (s *firstBoot20Suite) testPopulateFromSeedClassicWithModesRunModeNoKernelAndGadgetClassicSnap(c *C, modelGrade asserts.ModelGrade, modelUpdater func(*C, map[string]interface{}), expectedErr string) {
	defer release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "20.04"})()
	// re-init rootdirs required after MockReleaseInfo to ensure
	// dirs.SnapMountDir is set to /snap on e.g. fedora
	dirs.SetRootDir(dirs.GlobalRootDir)
	// XXX this shouldn't be needed
	defer release.MockOnClassic(true)()
	c.Assert(release.OnClassic, Equals, true)

	m := &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20221129",
		Base:           "core20_1.snap",
		Classic:        true,
	}

	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	err := m.WriteTo("")
	c.Assert(err, IsNil)

	s.extraSnapYaml["classic-installer"] = `name: classic-installer
version: 1.0
type: app
base: core20
confinement: classic

apps:
  inst:
    daemon: simple
`

	sysLabel := m.RecoverySystem
	model := s.setupCore20LikeSeed(c, core20SeedOptions{
		sysLabel:        sysLabel,
		modelGrade:      modelGrade,
		kernelAndGadget: false,
	}, "classic-installer")
	// validity check that our returned model has the expected grade
	c.Assert(model.Grade(), Equals, modelGrade)

	s.updateModel(c, sysLabel, model, modelUpdater)

	s.startOverlord(c)

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings)
	if expectedErr != "" {
		c.Check(err, ErrorMatches, expectedErr)
		return
	} else {
		c.Assert(err, IsNil)
	}

	snaps := []string{"snapd", "core20", "classic-installer"}
	checkOrder(c, tsAll, snaps...)

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

	// ensure required flag is set on all essential snaps
	var snapst snapstate.SnapState
	for _, reqName := range []string{"snapd", "core20"} {
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
	c.Check(sysdLog, Not(testutil.DeepContains), []string{"start", "usr-lib-snapd.mount"})

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
	c.Assert(dev.IsCoreBoot(), Equals, false)

	var whatseeded []devicestate.SeededSystem
	err = state.Get("seeded-systems", &whatseeded)
	c.Assert(err, IsNil)
	c.Assert(whatseeded, DeepEquals, []devicestate.SeededSystem{{
		System:    m.RecoverySystem,
		Model:     "my-model",
		BrandID:   "my-brand",
		Revision:  model.Revision(),
		Timestamp: model.Timestamp(),
		SeedTime:  seedTime,
	}})
}

func (s *firstBoot20Suite) TestPopulateFromSeedClassicWithModesDangerousRunModeNoKernelAndGadgetClassicSnap(c *C) {
	// classic snaps are implicitly allowed and seeded for dangerous
	// classic models
	s.extraSnapModelDetails["classic-installer"] = map[string]interface{}{
		"modes": []interface{}{"run"},
	}

	s.testPopulateFromSeedClassicWithModesRunModeNoKernelAndGadgetClassicSnap(c, asserts.ModelDangerous, nil, "")
}

func (s *firstBoot20Suite) TestPopulateFromSeedClassicWithModesSignedRunModeNoKernelAndGadgetClassicSnap(c *C) {
	// classic snaps must be declared explicitly for non-dangerous models
	s.extraSnapModelDetails["classic-installer"] = map[string]interface{}{
		"classic": "true",
		"modes":   []interface{}{"run"},
	}

	s.testPopulateFromSeedClassicWithModesRunModeNoKernelAndGadgetClassicSnap(c, asserts.ModelSigned, nil, "")
}

func (s *firstBoot20Suite) TestPopulateFromSeedClassicWithModesSignedRunModeNoKernelAndGadgetClassicSnapImplicitFails(c *C) {
	// classic snaps must be declared explicitly for non-dangerous models,
	// not doing so results in a seeding error

	// to evade the seedwriter checks to test the firstboot ones
	// create the system with model grade dangerous and then
	// switch/rewrite the model to be grade signed
	s.extraSnapModelDetails["classic-installer"] = map[string]interface{}{
		"modes": []interface{}{"run"},
	}

	switchToSigned := func(_ *C, modHeaders map[string]interface{}) {
		modHeaders["grade"] = string(asserts.ModelSigned)
	}

	s.testPopulateFromSeedClassicWithModesRunModeNoKernelAndGadgetClassicSnap(c, asserts.ModelDangerous, switchToSigned, `snap "classic-installer" requires classic confinement`)
}

func (s *firstBoot20Suite) testPopulateFromSeedCore20ValidationSetTracking(c *C, mode string, valSets []string) *state.Change {
	s.extraSnapYaml["some-snap"] = `name: some-snap
version: 1.0
type: app
base: core20
`

	m := boot.Modeenv{
		Mode:           mode,
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	model := s.setupCore20LikeSeed(c, core20SeedOptions{
		sysLabel:        m.RecoverySystem,
		modelGrade:      "signed",
		kernelAndGadget: true,
		valsets:         valSets,
	}, "some-snap")
	// validity check that our returned model has the expected grade
	c.Assert(model.Grade(), Equals, asserts.ModelSigned)

	bloader := bootloadertest.Mock("mock", c.MkDir()).WithExtractedRunKernelImage()
	bootloader.Force(bloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetEnabledKernel(kernel)
	s.AddCleanup(r)

	s.startOverlord(c)

	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	// run the firstboot code
	tsAll, err := devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings)
	c.Assert(err, IsNil)

	// ensure the validation-set tracking task is present
	tsEnd := tsAll[len(tsAll)-1]
	if mode == "run" {
		c.Assert(tsEnd.Tasks(), HasLen, 2)
		t := tsEnd.Tasks()[0]
		c.Check(t.Kind(), Equals, "enforce-validation-sets")

		expectedSeqs := make(map[string]int)
		expectedVss := make(map[string][]string)
		for _, vs := range valSets {
			tokens := strings.Split(vs, "/")
			c.Check(tokens, HasLen, 3)
			seq, err := strconv.Atoi(tokens[2])
			c.Assert(err, IsNil)
			key := fmt.Sprintf("%s/%s/%s", release.Series, tokens[0], tokens[1])
			expectedSeqs[key] = seq
			expectedVss[key] = append([]string{release.Series}, tokens...)
		}

		// verify that the correct data is provided to the task
		var pinnedSeqs map[string]int
		err = t.Get("pinned-sequence-numbers", &pinnedSeqs)
		c.Assert(err, IsNil)
		c.Check(pinnedSeqs, DeepEquals, expectedSeqs)
		var vsKeys map[string][]string
		err = t.Get("validation-set-keys", &vsKeys)
		c.Assert(err, IsNil)
		c.Check(vsKeys, DeepEquals, expectedVss)
	} else {
		c.Assert(tsEnd.Tasks(), HasLen, 1)
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
	return chg
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20ValidationSetTrackingHappy(c *C) {
	vsa, err := s.StoreSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "base-set",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       s.AssertedSnapID("pc-kernel"),
				"presence": "required",
				"revision": "1",
			},
			map[string]interface{}{
				"name":     "pc",
				"id":       s.AssertedSnapID("pc"),
				"presence": "required",
				"revision": "1",
			},
			map[string]interface{}{
				"name":     "some-snap",
				"id":       s.AssertedSnapID("some-snap"),
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.StoreSigning.Add(vsa)
	c.Assert(err, IsNil)

	chg := s.testPopulateFromSeedCore20ValidationSetTracking(c, "run", []string{"canonical/base-set/1"})

	s.overlord.State().Lock()
	defer s.overlord.State().Unlock()
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%s", chg.Err()))

	// Ensure that we are now tracking the validation-set
	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.overlord.State(), "canonical", "base-set", &tr)
	c.Assert(err, IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: "canonical",
		Name:      "base-set",
		Mode:      assertstate.Enforce,
		Current:   1,
		PinnedAt:  1,
	})
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20ValidationSetTrackingNotAddedInInstallMode(c *C) {
	vsa, err := s.StoreSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "base-set",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       s.AssertedSnapID("pc-kernel"),
				"presence": "required",
				"revision": "1",
			},
			map[string]interface{}{
				"name":     "pc",
				"id":       s.AssertedSnapID("pc"),
				"presence": "required",
				"revision": "1",
			},
			map[string]interface{}{
				"name":     "some-snap",
				"id":       s.AssertedSnapID("some-snap"),
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.StoreSigning.Add(vsa)
	c.Assert(err, IsNil)

	chg := s.testPopulateFromSeedCore20ValidationSetTracking(c, "install", []string{"canonical/base-set/1"})

	s.overlord.State().Lock()
	defer s.overlord.State().Unlock()
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%s", chg.Err()))

	// Ensure no validation-sets are tracked
	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.overlord.State(), "canonical", "base-set", &tr)
	c.Assert(err, ErrorMatches, `no state entry for key "validation-sets"`)
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20ValidationSetTrackingFailsUnmetCriterias(c *C) {
	vsb, err := s.StoreSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "base-set",
		"sequence":     "2",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       s.AssertedSnapID("my-snap"),
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.StoreSigning.Add(vsb)
	c.Assert(err, IsNil)

	chg := s.testPopulateFromSeedCore20ValidationSetTracking(c, "run", []string{"canonical/base-set/2"})

	s.overlord.State().Lock()
	defer s.overlord.State().Unlock()
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err().Error(), testutil.Contains, "my-snap (required at revision 1 by sets canonical/base-set))")
}
