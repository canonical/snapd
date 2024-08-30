// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2024 Canonical Ltd
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

package registrystate_test

import (
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
)

type hookHandlerSuite struct {
	state *state.State

	repo *interfaces.Repository
}

var _ = Suite(&hookHandlerSuite{})

func (s *hookHandlerSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.state = overlord.Mock().State()

	s.state.Lock()
	defer s.state.Unlock()

	s.repo = interfaces.NewRepository()
	ifacerepo.Replace(s.state, s.repo)

	regIface := &ifacetest.TestInterface{InterfaceName: "registry"}
	err := s.repo.AddInterface(regIface)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1
type: os
slots:
  registry-slot:
    interface: registry
`
	info := mockInstalledSnap(c, s.state, coreYaml, "")
	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = s.repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	snapYaml := `name: test-snap
version: 1
type: app
plugs:
  setup:
    interface: registry
    account: my-acc
    view: network/setup-wifi
`

	info = mockInstalledSnap(c, s.state, snapYaml, "")
	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	ref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: "setup"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "registry-slot"},
	}

	_, err = s.repo.Connect(ref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
}

func (s *hookHandlerSuite) TestViewChangedHookPlugConnected(c *C) {
	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "setup-view-changed",
	}
	ctx, err := hookstate.NewContext(nil, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler, ok := hookstate.ViewChangedHandlerGenerator(ctx).(hookstate.Precondition)
	c.Assert(ok, Equals, true)

	condMet, err := handler.Precondition()
	c.Assert(err, IsNil)
	c.Assert(condMet, Equals, true)
}

func (s *hookHandlerSuite) TestViewChangedHookNoPlug(c *C) {
	s.state.Lock()
	s.repo = interfaces.NewRepository()
	ifacerepo.Replace(s.state, s.repo)
	s.state.Unlock()

	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "setup-view-changed",
	}
	ctx, err := hookstate.NewContext(nil, s.state, hooksup, nil, "")
	c.Assert(err, IsNil)

	handler, ok := hookstate.ViewChangedHandlerGenerator(ctx).(hookstate.Precondition)
	c.Assert(ok, Equals, true)

	condMet, err := handler.Precondition()
	c.Assert(err, IsNil)
	c.Assert(condMet, Equals, false)
}
