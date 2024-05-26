// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2020 Canonical Ltd
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

package configcore_test

import (
	"path/filepath"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/servicestate/servicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/systemdtest"
	"github.com/snapcore/snapd/testutil"
)

type vitalitySuite struct {
	configcoreSuite
}

var _ = Suite(&vitalitySuite{})

func (s *vitalitySuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	uc18model := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "canonical",
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
		"base":         "core18",
	}).(*asserts.Model)

	s.AddCleanup(snapstatetest.MockDeviceModel(uc18model))
}

func (s *vitalitySuite) TestConfigureVitalityUnhappyName(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "-invalid-snap-name!yf",
		},
	}))
	c.Assert(err, ErrorMatches, `cannot set "resilience.vitality-hint": invalid snap name: ".*"`)
}

func (s *vitalitySuite) TestConfigureVitalityNoSnapd(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "snapd",
		},
	}))
	c.Assert(err, ErrorMatches, `cannot set "resilience.vitality-hint": snapd snap vitality cannot be changed`)
}

func (s *vitalitySuite) TestConfigureVitalityhappyName(c *C) {
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "valid-snapname",
		},
	}))

	// no snap named "valid-snapname" is installed, so no systemd action
	c.Check(s.systemctlArgs, HasLen, 0)
}

var mockSnapWithService = `name: test-snap
version: 1.0
apps:
 foo:
  daemon: simple
`

func (s *vitalitySuite) TestConfigureVitalityWithValidSnapUC16(c *C) {
	uc16model := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "canonical",
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
	}).(*asserts.Model)

	defer snapstatetest.MockDeviceModel(uc16model)()

	s.testConfigureVitalityWithValidSnap(c, false)
}

func (s *vitalitySuite) TestConfigureVitalityWithValidSnapUC18(c *C) {
	s.testConfigureVitalityWithValidSnap(c, true)
}

func (s *vitalitySuite) testConfigureVitalityWithValidSnap(c *C, uc18 bool) {
	si := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(1)}
	snaptest.MockSnap(c, mockSnapWithService, si)
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})
	s.state.Unlock()
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "unrelated,test-snap",
		},
	}))

	svcName := "snap.test-snap.foo.service"
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.foo.service"},
		{"--no-reload", "enable", "snap.test-snap.foo.service"},
		{"daemon-reload"},
		{"start", "snap.test-snap.foo.service"},
	})
	svcPath := filepath.Join(dirs.SnapServicesDir, svcName)
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898\n")
	if uc18 {
		c.Check(svcPath, testutil.FileContains, "\nWants=usr-lib-snapd.mount\n")
	}
}

func (s *vitalitySuite) TestConfigureVitalityWithQuotaGroup(c *C) {
	r := systemd.MockSystemdVersion(248, nil)
	defer r()

	r = servicestate.EnsureQuotaUsability()
	defer r()

	si := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(1)}
	snaptest.MockSnap(c, mockSnapWithService, si)
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})

	// CreateQuota is calling "systemctl.Restart", which needs to be mocked
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) (buf []byte, err error) {
		s.systemctlArgs = append(s.systemctlArgs, cmd)
		if out := systemdtest.HandleMockAllUnitsActiveOutput(cmd, nil); out != nil {
			return out, nil
		}

		if cmd[0] == "show" {
			return []byte("ActiveState=inactive\n"), nil
		}
		return nil, nil
	})
	s.AddCleanup(systemctlRestorer)
	mylog.

		// make a new quota group with this snap in it
		Check(servicestatetest.MockQuotaInState(s.state, "foogroup", "", []string{"test-snap"}, nil,
			quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeMiB).Build()))


	s.state.Unlock()
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "unrelated,test-snap",
		},
	}))

	svcName := "snap.test-snap.foo.service"
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"daemon-reload"},
		{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.foo.service"},
		{"--no-reload", "enable", "snap.test-snap.foo.service"},
		{"daemon-reload"},
		{"start", "snap.test-snap.foo.service"},
	})
	svcPath := filepath.Join(dirs.SnapServicesDir, svcName)
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898\n")
	c.Check(svcPath, testutil.FileContains, "\nSlice=snap.foogroup.slice\n")
}

func (s *vitalitySuite) TestConfigureVitalityHintTooMany(c *C) {
	l := make([]string, 101)
	for i := range l {
		l[i] = strconv.Itoa(i)
	}
	manyStr := strings.Join(l, ",")
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": manyStr,
		},
	}))
	c.Assert(err, ErrorMatches, `cannot set more than 100 snaps in "resilience.vitality-hint": got 101`)
}

func (s *vitalitySuite) TestConfigureVitalityManySnaps(c *C) {
	for _, snapName := range []string{"snap1", "snap2", "snap3"} {
		si := &snap.SideInfo{RealName: snapName, Revision: snap.R(1)}
		snaptest.MockSnap(c, mockSnapWithService, si)
		s.state.Lock()
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  snap.R(1),
			Active:   true,
			SnapType: "app",
		})
		s.state.Unlock()
	}
	mylog.

		// snap1,snap2,snap3
		Check(configcore.Run(classicDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"resilience.vitality-hint": "snap1,snap2,snap3",
			},
		}))

	// test
	svcPath := filepath.Join(dirs.SnapServicesDir, "snap.snap1.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-899\n")
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap2.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898\n")
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap3.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-897\n")
}

func (s *vitalitySuite) TestConfigureVitalityManySnapsDelta(c *C) {
	for _, snapName := range []string{"snap1", "snap2", "snap3"} {
		si := &snap.SideInfo{RealName: snapName, Revision: snap.R(1)}
		snaptest.MockSnap(c, mockSnapWithService, si)
		s.state.Lock()
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  snap.R(1),
			Active:   true,
			SnapType: "app",
		})
		s.state.Unlock()
	}
	mylog.

		// snap1,snap2,snap3 switch to snap3,snap1
		Check(configcore.Run(classicDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"resilience.vitality-hint": "snap1,snap2,snap3",
			},
			changes: map[string]interface{}{
				"resilience.vitality-hint": "snap3,snap1",
			},
		}))

	// test that snap1,snap3 got the new rank
	svcPath := filepath.Join(dirs.SnapServicesDir, "snap.snap1.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898")
	// and that snap2 no longer has a OOMScoreAdjust setting
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap2.foo.service")
	c.Check(svcPath, Not(testutil.FileContains), "\nOOMScoreAdjust=")
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap3.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-899\n")
}

func (s *vitalitySuite) TestConfigureVitalityManySnapsOneRemovedOneUnchanged(c *C) {
	for _, snapName := range []string{"snap1", "snap2", "snap3"} {
		si := &snap.SideInfo{RealName: snapName, Revision: snap.R(1)}
		snaptest.MockSnap(c, mockSnapWithService, si)
		s.state.Lock()
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
			Current:  snap.R(1),
			Active:   true,
			SnapType: "app",
		})
		s.state.Unlock()
	}
	mylog.

		// first run generates the snap1,snap2 configs
		Check(configcore.Run(classicDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"resilience.vitality-hint": "snap1,snap2",
			},
		}))

	svcPath := filepath.Join(dirs.SnapServicesDir, "snap.snap1.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-899")
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap2.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898\n")
	c.Check(s.systemctlArgs, testutil.DeepContains, []string{"start", "snap.snap1.foo.service"})
	c.Check(s.systemctlArgs, testutil.DeepContains, []string{"start", "snap.snap2.foo.service"})
	s.systemctlArgs = nil
	mylog.

		// now we change the configuration and set snap1,snap3
		Check(configcore.Run(classicDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"resilience.vitality-hint": "snap1,snap2",
			},
			changes: map[string]interface{}{
				"resilience.vitality-hint": "snap1,snap3",
			},
		}))

	// test that snap1 is unchanged
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap1.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-899")
	// and that snap2 no longer has a OOMScoreAdjust setting
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap2.foo.service")
	c.Check(svcPath, Not(testutil.FileContains), "\nOOMScoreAdjust=")
	// snap3 got added
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap3.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898\n")

	// ensure that snap1 did not get started again (it is unchanged)
	c.Check(s.systemctlArgs, Not(testutil.DeepContains), []string{"start", "snap.snap1.foo.service"})
	// snap2 changed (no OOMScoreAdjust anymore) so needs restart
	c.Check(s.systemctlArgs, testutil.DeepContains, []string{"start", "snap.snap2.foo.service"})
	// snap3 changed so needs restart
	c.Check(s.systemctlArgs, testutil.DeepContains, []string{"start", "snap.snap3.foo.service"})
}

func (s *vitalitySuite) TestConfigureVitalityNotActiveSnap(c *C) {
	si := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(1)}
	snaptest.MockSnap(c, mockSnapWithService, si)
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		Active:   false,
		SnapType: "app",
	})
	s.state.Unlock()
	mylog.Check(configcore.Run(classicDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "unrelated,test-snap",
		},
	}))

	c.Check(s.systemctlArgs, HasLen, 0)
}
