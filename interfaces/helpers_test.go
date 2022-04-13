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
	setupCalls := 0
	setupManyCalls := 0

	backend := &ifacetest.TestSecurityBackendSetupMany{
		TestSecurityBackend: ifacetest.TestSecurityBackend{BackendName: "fake",
			SetupCallback: func(snapOpts interfaces.SecurityBackendSnapOptions, repo *interfaces.Repository) error {
				setupCalls++
				return nil
			},
		},
		SetupManyCallback: func(snapsOpts []interfaces.SecurityBackendSnapOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
			c.Assert(snapsOpts, HasLen, 2)
			c.Check(snapsOpts[0].SnapInfo.SnapName(), Equals, "some-snap")
			c.Check(snapsOpts[1].SnapInfo.SnapName(), Equals, "other-snap")
			setupManyCalls++
			return nil
		},
	}

	snapsOpts := []interfaces.SecurityBackendSnapOptions{
		{
			SnapInfo: s.snap1,
		},
		{
			SnapInfo: s.snap2,
		},
	}
	errs := interfaces.SetupMany(s.repo, backend, snapsOpts, s.tm)
	c.Check(errs, HasLen, 0)
	c.Check(setupManyCalls, Equals, 1)
	c.Check(setupCalls, Equals, 0)
}

func (s *HelpersSuite) TestSetupManyRunsSetupIfSetupManyNotImplemented(c *C) {
	setupCalls := 0

	backend := &ifacetest.TestSecurityBackend{
		BackendName: "fake",
		SetupCallback: func(snapOpts interfaces.SecurityBackendSnapOptions, repo *interfaces.Repository) error {
			setupCalls++
			return nil
		},
	}

	snapsOpts := []interfaces.SecurityBackendSnapOptions{
		{
			SnapInfo: s.snap1,
		},
		{
			SnapInfo: s.snap2,
		},
	}
	errs := interfaces.SetupMany(s.repo, backend, snapsOpts, s.tm)
	c.Check(errs, HasLen, 0)
	c.Check(setupCalls, Equals, 2)
}

func (s *HelpersSuite) TestSetupManySetupManyNotOK(c *C) {
	setupCalls := 0
	setupManyCalls := 0

	backend := &ifacetest.TestSecurityBackendSetupMany{
		TestSecurityBackend: ifacetest.TestSecurityBackend{
			BackendName: "fake",
			SetupCallback: func(snapOpts interfaces.SecurityBackendSnapOptions, repo *interfaces.Repository) error {
				setupCalls++
				return nil
			},
		},
		SetupManyCallback: func(snapsOpts []interfaces.SecurityBackendSnapOptions, repo *interfaces.Repository, tm timings.Measurer) []error {
			c.Check(snapsOpts, HasLen, 2)
			setupManyCalls++
			return []error{fmt.Errorf("error1"), fmt.Errorf("error2")}
		},
	}

	snapsOpts := []interfaces.SecurityBackendSnapOptions{
		{
			SnapInfo: s.snap1,
		},
		{
			SnapInfo: s.snap2,
		},
	}
	errs := interfaces.SetupMany(s.repo, backend, snapsOpts, s.tm)
	c.Check(errs, HasLen, 2)
	c.Check(setupManyCalls, Equals, 1)
	c.Check(setupCalls, Equals, 0)
}

func (s *HelpersSuite) TestSetupManySetupNotOK(c *C) {
	setupCalls := 0
	backend := &ifacetest.TestSecurityBackend{
		BackendName: "fake",
		SetupCallback: func(snapOpts interfaces.SecurityBackendSnapOptions, repo *interfaces.Repository) error {
			setupCalls++
			return fmt.Errorf("error %d", setupCalls)
		},
	}

	snapsOpts := []interfaces.SecurityBackendSnapOptions{
		{
			SnapInfo: s.snap1,
		},
		{
			SnapInfo: s.snap2,
		},
	}
	errs := interfaces.SetupMany(s.repo, backend, snapsOpts, s.tm)
	c.Check(errs, HasLen, 2)
	c.Check(setupCalls, Equals, 2)
}
