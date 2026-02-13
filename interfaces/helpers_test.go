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
	snap1 *interfaces.SnapAppSet
	snap2 *interfaces.SnapAppSet
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

	snap1Info := snaptest.MockSnap(c, snapYaml1, &snap.SideInfo{Revision: snap.R(1)})
	snap2Info := snaptest.MockSnap(c, snapYaml2, &snap.SideInfo{Revision: snap.R(1)})

	snap1AppSet, err := interfaces.NewSnapAppSet(snap1Info, nil)
	c.Assert(err, IsNil)
	s.snap1 = snap1AppSet

	snap2AppSet, err := interfaces.NewSnapAppSet(snap2Info, nil)
	c.Assert(err, IsNil)
	s.snap2 = snap2AppSet
}

func (s *HelpersSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("/")
}

func (s *HelpersSuite) TestSetupManyRunsSetupManyIfImplemented(c *C) {
	confinementOpts := func(snapName string) interfaces.ConfinementOptions {
		return interfaces.ConfinementOptions{}
	}

	setupContext := func(snapName string) interfaces.SetupContext {
		if snapName == "some-snap" {
			return interfaces.SetupContext{Reason: interfaces.SnapSetupReasonConnectedSlotProviderUpdate}
		}
		return interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOwnUpdate}
	}

	setupCalls := 0
	setupManyCalls := 0

	backend := &ifacetest.TestSecurityBackendSetupMany{
		TestSecurityBackend: ifacetest.TestSecurityBackend{BackendName: "fake",
			SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository) error {
				setupCalls++
				return nil
			},
		},
		SetupManyCallback: func(appSets []*interfaces.SnapAppSet,
			confinement func(snapName string) interfaces.ConfinementOptions,
			sctx func(snapName string) interfaces.SetupContext,
			repo *interfaces.Repository, tm timings.Measurer,
		) []error {
			c.Assert(appSets, HasLen, 2)
			c.Check(appSets[0].Info().SnapName(), Equals, "some-snap")
			c.Check(appSets[1].Info().SnapName(), Equals, "other-snap")
			c.Assert(sctx, NotNil)
			c.Check(sctx(appSets[0].Info().SnapName()),
				DeepEquals, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonConnectedSlotProviderUpdate})
			c.Check(sctx(appSets[1].Info().SnapName()),
				DeepEquals, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOwnUpdate})
			setupManyCalls++
			return nil
		},
	}

	errs := interfaces.SetupMany(s.repo, backend, []*interfaces.SnapAppSet{s.snap1, s.snap2}, confinementOpts, setupContext, s.tm)
	c.Check(errs, HasLen, 0)
	c.Check(setupManyCalls, Equals, 1)
	c.Check(setupCalls, Equals, 0)
}

func (s *HelpersSuite) TestSetupManyRunsSetupIfSetupManyNotImplemented(c *C) {
	setupCalls := 0
	var confinementOptsCalls []string
	var setupContextCalls []string

	backend := &ifacetest.TestSecurityBackend{
		BackendName: "fake",
		SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository) error {
			setupCalls++
			return nil
		},
	}

	confinementOpts := func(snapName string) interfaces.ConfinementOptions {
		confinementOptsCalls = append(confinementOptsCalls, snapName)
		return interfaces.ConfinementOptions{}
	}

	setupContext := func(snapName string) interfaces.SetupContext {
		setupContextCalls = append(setupContextCalls, snapName)
		return interfaces.SetupContext{
			Reason: interfaces.SnapSetupReasonOther,
		}
	}

	errs := interfaces.SetupMany(s.repo, backend, []*interfaces.SnapAppSet{s.snap1, s.snap2},
		confinementOpts, setupContext, s.tm)
	c.Check(errs, HasLen, 0)
	c.Check(setupCalls, Equals, 2)
	c.Check(confinementOptsCalls, DeepEquals, []string{"some-snap", "other-snap"})
	c.Check(setupContextCalls, DeepEquals, []string{"some-snap", "other-snap"})
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
			SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository) error {
				setupCalls++
				return nil
			},
		},
		SetupManyCallback: func(appSets []*interfaces.SnapAppSet,
			confinement func(snapName string) interfaces.ConfinementOptions,
			sctx func(snapName string) interfaces.SetupContext,
			repo *interfaces.Repository, tm timings.Measurer,
		) []error {
			c.Check(appSets, HasLen, 2)
			setupManyCalls++
			return []error{fmt.Errorf("error1"), fmt.Errorf("error2")}
		},
	}

	errs := interfaces.SetupMany(s.repo, backend, []*interfaces.SnapAppSet{s.snap1, s.snap2}, confinementOpts, func(snapName string) interfaces.SetupContext {
		return interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}
	}, s.tm)
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
		SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository) error {
			setupCalls++
			return fmt.Errorf("error %d", setupCalls)
		},
	}

	errs := interfaces.SetupMany(s.repo, backend, []*interfaces.SnapAppSet{s.snap1, s.snap2}, confinementOpts, func(snapName string) interfaces.SetupContext {
		return interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}
	}, s.tm)
	c.Check(errs, HasLen, 2)
	c.Check(setupCalls, Equals, 2)
}

func (s *HelpersSuite) TestApplyDelayedNotImplemented(c *C) {
	backend := &ifacetest.TestSecurityBackend{
		BackendName: "fake",
		SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository) error {
			panic("unexpected call")
		},
	}

	err := interfaces.ApplyDelayedEffects(s.repo, backend, s.snap1, nil, s.tm)
	c.Check(err, IsNil)

	err = interfaces.ApplyDelayedEffects(s.repo, backend, s.snap1, []interfaces.DelayedSideEffect{
		{"eff-1", "some eff"},
	}, s.tm)
	c.Check(err, ErrorMatches, "internal error: calling apply delayed effects for unsupported backend \"fake\"")
}

func (s *HelpersSuite) TestApplyDelayedHappy(c *C) {
	var applyDelayedCalls [][]interfaces.DelayedSideEffect

	backend := &ifacetest.TestSecurityBackendDelayedEffects{
		TestSecurityBackend: ifacetest.TestSecurityBackend{
			BackendName: "fake",
			SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository) error {
				panic("unexpected call")
			},
		},

		ApplyDelayedEffectsCallback: func(appSet *interfaces.SnapAppSet, eff []interfaces.DelayedSideEffect) error {
			applyDelayedCalls = append(applyDelayedCalls, eff)
			return nil
		},
	}

	err := interfaces.ApplyDelayedEffects(s.repo, backend, s.snap1, nil, s.tm)
	c.Check(err, IsNil)
	c.Check(applyDelayedCalls, HasLen, 0)
	err = interfaces.ApplyDelayedEffects(s.repo, backend, s.snap1, []interfaces.DelayedSideEffect{
		{"eff-1", "eff 1"},
		{"eff-2", "eff 2"},
	}, s.tm)
	c.Check(err, IsNil)
	c.Check(applyDelayedCalls, DeepEquals, [][]interfaces.DelayedSideEffect{{
		{"eff-1", "eff 1"},
		{"eff-2", "eff 2"},
	}})
}

func (s *HelpersSuite) TestApplyDelayedErrs(c *C) {
	var applyDelayedCalls [][]interfaces.DelayedSideEffect

	backend := &ifacetest.TestSecurityBackendDelayedEffects{
		TestSecurityBackend: ifacetest.TestSecurityBackend{
			BackendName: "fake",
			SetupCallback: func(appSet *interfaces.SnapAppSet, opts interfaces.ConfinementOptions, sctx interfaces.SetupContext, repo *interfaces.Repository) error {
				panic("unexpected call")
			},
		},

		ApplyDelayedEffectsCallback: func(appSet *interfaces.SnapAppSet, eff []interfaces.DelayedSideEffect) error {
			applyDelayedCalls = append(applyDelayedCalls, eff)
			for _, w := range eff {
				if w.ID == "eff-2" {
					return fmt.Errorf("mock error")
				}
			}
			return nil
		},
	}

	err := interfaces.ApplyDelayedEffects(s.repo, backend, s.snap1, []interfaces.DelayedSideEffect{
		{"eff-1", "eff 1"},
		{"eff-2", "eff 2"},
	}, s.tm)
	c.Check(err, ErrorMatches, "mock error")
	c.Check(applyDelayedCalls, DeepEquals, [][]interfaces.DelayedSideEffect{{
		{"eff-1", "eff 1"},
		{"eff-2", "eff 2"},
	}})
}
