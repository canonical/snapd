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

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
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

func (s *firstBoot20Suite) setupCore20Seed(c *C, sysLabel string) {
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

	s.MakeSeed(c, sysLabel, "my-brand", "my-model", map[string]interface{}{
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

func (s *firstBoot20Suite) TestPopulateFromSeedCore20Happy(c *C) {
	r := boottest.ForceModeenv(dirs.GlobalRootDir, &boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "20191018",
		Kernel:         "pc-kernel_1.snap",
		Base:           "core20_1.snap",
	})
	defer r()

	// restart overlord to pick up the modeenv
	s.startOverlord(c)

	// XXX some things are not yet completely final/realistic
	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	sysLabel := "20191018"
	s.setupCore20Seed(c, sysLabel)

	// XXX Core 20 has multiple bootenvs
	bloader := bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core20_1.snap")
	kernel, err := snap.ParsePlaceInfoFromSnapFileName("pc-kernel_1.snap")
	c.Assert(err, IsNil)
	r = bloader.SetRunKernelImageEnabledKernel(kernel)
	defer r()

	opts := devicestate.PopulateStateFromSeedOptions{
		Label: sysLabel,
		Mode:  "run",
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

	// check that the default device ctx has a Modeenv
	dev, err := devicestate.DeviceCtx(s.overlord.State(), nil, nil)
	c.Assert(err, IsNil)
	c.Assert(dev.HasModeenv(), Equals, true)
}
