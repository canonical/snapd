// -*- Mode: Go; indent-tabs-mode: t -*-

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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type vitalitySuite struct {
	configcoreSuite
}

var _ = Suite(&vitalitySuite{})

func (s *vitalitySuite) TestConfigureVitalityUnhappyName(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "-invalid-snap-name!yf",
		},
	})
	c.Assert(err, ErrorMatches, `cannot set "resilience.vitality-hint": invalid snap name: ".*"`)
}

func (s *vitalitySuite) TestConfigureVitalityNoSnapd(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "snapd",
		},
	})
	c.Assert(err, ErrorMatches, `cannot set "resilience.vitality-hint": snapd snap vitality cannot be changed`)
}

func (s *vitalitySuite) TestConfigureVitalityhappyName(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "valid-snapname",
		},
	})
	c.Assert(err, IsNil)
	// no snap named "valid-snapname" is installed, so no systemd action
	c.Check(s.systemctlArgs, HasLen, 0)
}

var mockSnapWithService = `name: test-snap
version: 1.0
apps:
 foo:
  daemon: simple
`

func (s *vitalitySuite) TestConfigureVitalityWithValidSnap(c *C) {
	si := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(1)}
	snaptest.MockSnap(c, mockSnapWithService, si)
	s.state.Lock()
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		Active:   true,
		SnapType: "app",
	})
	s.state.Unlock()

	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "unrelated,test-snap",
		},
	})
	c.Assert(err, IsNil)
	svcName := "snap.test-snap.foo.service"
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"is-enabled", "snap.test-snap.foo.service"},
		{"daemon-reload"},
		{"enable", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.foo.service"},
	})
	svcPath := filepath.Join(dirs.SnapServicesDir, svcName)
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898\n")
}

func (s *vitalitySuite) TestConfigureVitalityHintTooMany(c *C) {
	l := make([]string, 101)
	for i := range l {
		l[i] = strconv.Itoa(i)
	}
	manyStr := strings.Join(l, ",")
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": manyStr,
		},
	})
	c.Assert(err, ErrorMatches, `cannot set more than 100 snaps in "resilience.vitality-hint": got 101`)
}

func (s *vitalitySuite) TestConfigureVitalityManySnaps(c *C) {
	for _, snapName := range []string{"snap1", "snap2", "snap3"} {
		si := &snap.SideInfo{RealName: snapName, Revision: snap.R(1)}
		snaptest.MockSnap(c, mockSnapWithService, si)
		s.state.Lock()
		snapstate.Set(s.state, snapName, &snapstate.SnapState{
			Sequence: []*snap.SideInfo{si},
			Current:  snap.R(1),
			Active:   true,
			SnapType: "app",
		})
		s.state.Unlock()
	}

	// snap1,snap2,snap3
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "snap1,snap2,snap3",
		},
	})
	c.Assert(err, IsNil)
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
			Sequence: []*snap.SideInfo{si},
			Current:  snap.R(1),
			Active:   true,
			SnapType: "app",
		})
		s.state.Unlock()
	}

	// snap1,snap2,snap3 switch to snap3,snap1
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"resilience.vitality-hint": "snap1,snap2,snap3",
		},
		changes: map[string]interface{}{
			"resilience.vitality-hint": "snap3,snap1",
		},
	})
	c.Assert(err, IsNil)
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
			Sequence: []*snap.SideInfo{si},
			Current:  snap.R(1),
			Active:   true,
			SnapType: "app",
		})
		s.state.Unlock()
	}

	// first run generates the snap1,snap2 configs
	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "snap1,snap2",
		},
	})
	c.Assert(err, IsNil)
	svcPath := filepath.Join(dirs.SnapServicesDir, "snap.snap1.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-899")
	svcPath = filepath.Join(dirs.SnapServicesDir, "snap.snap2.foo.service")
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-898\n")
	c.Check(s.systemctlArgs, testutil.DeepContains, []string{"start", "snap.snap1.foo.service"})
	c.Check(s.systemctlArgs, testutil.DeepContains, []string{"start", "snap.snap2.foo.service"})
	s.systemctlArgs = nil

	// now we change the configuration and set snap1,snap3
	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"resilience.vitality-hint": "snap1,snap2",
		},
		changes: map[string]interface{}{
			"resilience.vitality-hint": "snap1,snap3",
		},
	})
	c.Assert(err, IsNil)
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
		Sequence: []*snap.SideInfo{si},
		Current:  snap.R(1),
		Active:   false,
		SnapType: "app",
	})
	s.state.Unlock()

	err := configcore.Run(&mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"resilience.vitality-hint": "unrelated,test-snap",
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.systemctlArgs, HasLen, 0)
}
