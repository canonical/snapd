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
	rootdir := dirs.GlobalRootDir
	svcName := "snap.test-snap.foo.service"
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"--root", rootdir, "enable", "snap.test-snap.foo.service"},
		{"daemon-reload"},
		{"--root", rootdir, "is-enabled", "snap.test-snap.foo.service"},
		{"start", "snap.test-snap.foo.service"},
	})
	svcPath := filepath.Join(dirs.SnapServicesDir, svcName)
	c.Check(svcPath, testutil.FileContains, "\nOOMScoreAdjust=-897\n")
}
