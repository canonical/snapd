// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package export_test

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/export"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendSuite struct {
	ifacetest.BackendSuite
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &export.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityExport)
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"mediated-export"})
}

func (s *backendSuite) mockSlot(c *C, yaml string, slotName string) (*interfaces.SnapAppSet, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, nil)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.Repo.AddAppSet(set)
	c.Assert(err, IsNil)

	if slotInfo, ok := info.Slots[slotName]; ok {
		return set, slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func (s *backendSuite) mockPlug(c *C, yaml string, plugName string) (*interfaces.SnapAppSet, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, nil)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.Repo.AddAppSet(set)
	c.Assert(err, IsNil)

	if plugInfo, ok := info.Plugs[plugName]; ok {
		return set, plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

const exportProvider = `name: provider
version: 0
type: app
slots:
  some-driver-libs:
`

const exportConsumer = `name: snapd
version: 0
type: snapd
plugs:
  some-driver-libs:
apps:
  app:
    plugs: [some-driver-libs]
`

// TestSetupNoConnectionsIsNoop verifies that Setup() succeeds and does
// nothing when the system snap has no connections using the export backend.
func (s *backendSuite) TestSetupNoConnectionsIsNoop(c *C) {
	s.Iface.InterfaceName = "some-driver-libs"
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, _ := s.mockPlug(c, exportConsumer, "some-driver-libs")
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{},
		interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)
}

// TestConnectedPlugCallbackInvoked verifies that Setup() causes the
// interface's ExportConnectedPlug callback to be invoked for each connected
// slot, and that it stops being invoked once disconnected.
func (s *backendSuite) TestConnectedPlugCallbackInvoked(c *C) {
	var calledForSlots []string
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		calledForSlots = append(calledForSlots, slot.Snap().InstanceName())
		return nil
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")

	// Not connected yet: Setup builds a spec with no connections, so the
	// callback must not fire.
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{},
		interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)
	c.Check(calledForSlots, HasLen, 0)

	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{},
		interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)
	c.Check(calledForSlots, DeepEquals, []string{"provider"})

	calledForSlots = nil
	c.Assert(s.Repo.Disconnect(plugInfo.Snap.InstanceName(), plugInfo.Name,
		slotInfo.Snap.InstanceName(), slotInfo.Name), IsNil)
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{},
		interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)
	c.Check(calledForSlots, HasLen, 0)
}

// TestConnectedPlugCallbackError verifies that an error returned by the
// interface's ExportConnectedPlug callback propagates out of Setup().
func (s *backendSuite) TestConnectedPlugCallbackError(c *C) {
	boom := fmt.Errorf("boom")
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return boom
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")

	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	err = s.Backend.Setup(appSet, interfaces.ConfinementOptions{},
		interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil)
	c.Assert(err, ErrorMatches, `cannot obtain export specification for snap "snapd": boom`)
}
