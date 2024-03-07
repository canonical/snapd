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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type NetworkStatusSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&NetworkStatusSuite{
	iface: builtin.MustInterface("network-status"),
})

func (s *NetworkStatusSuite) SetUpSuite(c *C) {
	const coreProviderYaml = `name: core
type: os
version: 0
slots:
  network-status:
`
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, coreProviderYaml, nil, "network-status")

	const consumerYaml = `name: consumer
version: 1.0
apps:
  app:
    command: foo
    plugs: [network-status]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "network-status")
}

func (s *NetworkStatusSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "network-status")
}

func (s *NetworkStatusSuite) TestAppArmorConnectedPlug(c *C) {
	// If the slot is provided by a snap, access is restricted to the snap's label
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label=unconfined)`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.freedesktop.portal.NetworkMonitor")
}

func (s *NetworkStatusSuite) TestAppArmorConnectedSlot(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *NetworkStatusSuite) TestAppArmorPermanentSlot(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *NetworkStatusSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
