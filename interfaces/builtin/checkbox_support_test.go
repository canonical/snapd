// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type CheckboxSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const checkboxSupportCoreYaml = `name: core
version: 0
type: os
slots:
  checkbox-support:
`

const checkboxSupportConsumerYaml = `name: consumer
version: 0
apps:
  app:
    plugs: [checkbox-support]
`

var _ = Suite(&CheckboxSupportInterfaceSuite{
	iface: builtin.MustInterface("checkbox-support"),
})

func (s *CheckboxSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, checkboxSupportConsumerYaml, nil, "checkbox-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, checkboxSupportCoreYaml, nil, "checkbox-support")
}

func (s *CheckboxSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "checkbox-support")
}

func (s *CheckboxSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *CheckboxSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "StartTransientUnit")
}

func (s *CheckboxSupportInterfaceSuite) TestUdevSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.ControlsDeviceCgroup(), Equals, false)
}

func (s *CheckboxSupportInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, "allows checkbox to execute arbitrary system tests")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "checkbox-support")
}
