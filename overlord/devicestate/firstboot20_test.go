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
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type firstBoot20Suite struct {
	firstBootBaseTest

	snapYaml map[string]string

	// TestingSeed20 helps populating seeds (it provides
	// MakeAssertedSnap, MakeSeed) for tests.
	*seedtest.TestingSeed20
}

var _ = Suite(&firstBoot20Suite{})

func (s *firstBoot20Suite) SetUpTest(c *C) {
	s.snapYaml = seedtest.SampleSnapYaml

	s.TestingSeed20 = &seedtest.TestingSeed20{}

	s.setupBaseTest(c, &s.TestingSeed20.SeedSnaps)

	// don't start the overlord here so that we can mock different modeenvs
	// later, which is needed by devicestart manager startup with uc20 booting

	s.SeedDir = dirs.SnapSeedDir

	// mock the snap mapper as snapd here
	s.AddCleanup(ifacestate.MockSnapMapper(&ifacestate.CoreSnapdSystemMapper{}))
}

func (s *firstBoot20Suite) setupCore20Seed(c *C, sysLabel string) *asserts.Model {
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
		s.MakeAssertedSnap(c, s.snapYaml[yamlKey], files, snap.R(1), "canonical", s.StoreSigning.Database)
	}

	makeSnap("snapd")
	makeSnap("pc-kernel=20")
	makeSnap("core20")
	makeSnap("pc=20")

	return s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
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
			}},
	}, nil)
}

func (s *firstBoot20Suite) testPopulateFromSeedCore20Happy(c *C, m *boot.Modeenv) {
	c.Assert(m, NotNil, Commentf("missing modeenv test data"))
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// restart overlord to pick up the modeenv
	s.startOverlord(c)

	// XXX some things are not yet completely final/realistic
	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	sysLabel := m.RecoverySystem
	model := s.setupCore20Seed(c, sysLabel)

	bloader := bootloadertest.Mock("mock", c.MkDir()).WithExtractedRunKernelImage()
	bootloader.Force(bloader)
	defer bootloader.Force(nil)

	// since we are in runmode, MakeBootable will already have run from install
	// mode, and extracted the kernel assets for the kernel snap into the
	// bootloader, so set the current kernel there
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r := bloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	opts := devicestate.PopulateStateFromSeedOptions{
		Label: sysLabel,
		Mode:  m.Mode,
	}

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(st, &opts, s.perfTimings)
	c.Assert(err, IsNil)

	checkOrder(c, tsAll, "snapd", "pc-kernel", "core20", "pc")

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
	state.MockRestarting(st, state.RestartUnset)
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
	// check snapd, core18, kernel, gadget
	_, err = snapstate.CurrentInfo(state, "snapd")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "core20")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "pc-kernel")
	c.Check(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "pc")
	c.Check(err, IsNil)

	// ensure required flag is set on all essential snaps
	var snapst snapstate.SnapState
	for _, reqName := range []string{"snapd", "core20", "pc-kernel", "pc"} {
		err = snapstate.Get(state, reqName, &snapst)
		c.Assert(err, IsNil)
		c.Assert(snapst.Required, Equals, true, Commentf("required not set for %v", reqName))
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
		// * 1 from boot.GetCurrentBoot() from WaitRestart after restart
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

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RunMode(c *C) {
	m := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Base:           "core20_1.snap",
	}
	s.testPopulateFromSeedCore20Happy(c, &m)
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20InstallMode(c *C) {
	m := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191019",
		Base:           "core20_1.snap",
	}
	s.testPopulateFromSeedCore20Happy(c, &m)
}

func (s *firstBoot20Suite) TestPopulateFromSeedCore20RecoverMode(c *C) {
	m := boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: "20191020",
		Base:           "core20_1.snap",
	}
	s.testPopulateFromSeedCore20Happy(c, &m)
}
