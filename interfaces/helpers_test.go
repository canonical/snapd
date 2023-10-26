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

package interfaces_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type HelpersSuite struct {
	testutil.BaseTest

	repo  *interfaces.Repository
	snap1 *snap.Info
	snap2 *snap.Info
	tm    timings.Measurer
}

var _ = Suite(&HelpersSuite{})

const snapYaml1 = `
name: some-snap
version: 1
`

const snapYaml2 = `
name: other-snap
version: 2
`

func (s *HelpersSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	tmp := c.MkDir()
	dirs.SetRootDir(tmp)

	s.repo = interfaces.NewRepository()
	s.tm = timings.New(nil)
	s.snap1 = snaptest.MockSnap(c, snapYaml1, &snap.SideInfo{Revision: snap.R(1)})
	s.snap2 = snaptest.MockSnap(c, snapYaml2, &snap.SideInfo{Revision: snap.R(1)})
}

func (s *HelpersSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("/")
}

func (s *HelpersSuite) TestSetupManyRunsSetupManyIfImplemented(c *C) {
	confinementOpts := func(snapName string) interfaces.ConfinementOptions {
		return interfaces.ConfinementOptions{}
	}

	setupCalls := 0
	setupManyCalls := 0

	backend := &ifacetest.TestSecurityBackendSetupMany{
		TestSecurityBackend: ifacetest.TestSecurityBackend{BackendName: "fake",
			SetupCallback: func(snap *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
				setupCalls++
				return nil
			},
		},
		SetupManyCallback: func(snaps []*snap.Info, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
			c.Assert(snaps, HasLen, 2)
			c.Check(snaps[0].SnapName(), Equals, "some-snap")
			c.Check(snaps[1].SnapName(), Equals, "other-snap")
			setupManyCalls++
			return nil
		},
	}

	errs := interfaces.SetupMany(s.repo, backend, []*snap.Info{s.snap1, s.snap2}, confinementOpts, s.tm)
	c.Check(errs, HasLen, 0)
	c.Check(setupManyCalls, Equals, 1)
	c.Check(setupCalls, Equals, 0)
}

func (s *HelpersSuite) TestSetupManyRunsSetupIfSetupManyNotImplemented(c *C) {
	setupCalls := 0
	confinementOptsCalls := 0

	backend := &ifacetest.TestSecurityBackend{
		BackendName: "fake",
		SetupCallback: func(snap *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
			setupCalls++
			return nil
		},
	}

	confinementOpts := func(snapName string) interfaces.ConfinementOptions {
		confinementOptsCalls++
		return interfaces.ConfinementOptions{}
	}

	errs := interfaces.SetupMany(s.repo, backend, []*snap.Info{s.snap1, s.snap2}, confinementOpts, s.tm)
	c.Check(errs, HasLen, 0)
	c.Check(setupCalls, Equals, 2)
	c.Check(confinementOptsCalls, Equals, 2)
}

func (s *HelpersSuite) TestSetupManySetupManyNotOK(c *C) {
	confinementOpts := func(snapName string) interfaces.ConfinementOptions {
		return interfaces.ConfinementOptions{}
	}

	setupCalls := 0
	setupManyCalls := 0

	backend := &ifacetest.TestSecurityBackendSetupMany{
		TestSecurityBackend: ifacetest.TestSecurityBackend{
			BackendName: "fake",
			SetupCallback: func(snap *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
				setupCalls++
				return nil
			},
		},
		SetupManyCallback: func(snaps []*snap.Info, confinement func(snapName string) interfaces.ConfinementOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
			c.Check(snaps, HasLen, 2)
			setupManyCalls++
			return []error{fmt.Errorf("error1"), fmt.Errorf("error2")}
		},
	}

	errs := interfaces.SetupMany(s.repo, backend, []*snap.Info{s.snap1, s.snap2}, confinementOpts, s.tm)
	c.Check(errs, HasLen, 2)
	c.Check(setupManyCalls, Equals, 1)
	c.Check(setupCalls, Equals, 0)
}

func (s *HelpersSuite) TestSetupManySetupNotOK(c *C) {
	confinementOpts := func(snapName string) interfaces.ConfinementOptions {
		return interfaces.ConfinementOptions{}
	}

	setupCalls := 0
	backend := &ifacetest.TestSecurityBackend{
		BackendName: "fake",
		SetupCallback: func(snap *snap.Info, opts interfaces.ConfinementOptions, repo *interfaces.Repository) error {
			setupCalls++
			return fmt.Errorf("error %d", setupCalls)
		},
	}

	errs := interfaces.SetupMany(s.repo, backend, []*snap.Info{s.snap1, s.snap2}, confinementOpts, s.tm)
	c.Check(errs, HasLen, 2)
	c.Check(setupCalls, Equals, 2)
}
