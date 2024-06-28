// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type ScsiGenericInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&ScsiGenericInterfaceSuite{
	iface: builtin.MustInterface("scsi-generic"),
})

func (s *ScsiGenericInterfaceSuite) SetUpTest(c *C) {
	var mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [scsi-generic]
`
	var mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 scsi-generic:
  interface: scsi-generic
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "scsi-generic")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "scsi-generic")
}

func (s *ScsiGenericInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "scsi-generic")
}

func (s *ScsiGenericInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *ScsiGenericInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *ScsiGenericInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/dev/sg[0-9]* rw")
}

func (s *ScsiGenericInterfaceSuite) TestUDevSpec(c *C) {
	udevSpec := udev.NewSpecification(s.plug.AppSet())
	c.Assert(udevSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 2)
	c.Assert(udevSpec.Snippets(), testutil.Contains, `# scsi-generic
KERNEL=="sg[0-9]*", TAG+="snap_other_app"`)
	c.Assert(udevSpec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_other_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_other_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *ScsiGenericInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *ScsiGenericInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to SCSI generic driver devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "scsi-generic")
}
