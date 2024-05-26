// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type CalendarServiceInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&CalendarServiceInterfaceSuite{
	iface: builtin.MustInterface("calendar-service"),
})

func (s *CalendarServiceInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
version: 0
type: os
slots:
 calendar-service:
  interface: calendar-service
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreYaml, nil, "calendar-service")

	consumerYaml := `name: consumer
version: 0
apps:
 app:
  plugs: [calendar-service]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "calendar-service")
}

func (s *CalendarServiceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "calendar-service")
}

func (s *CalendarServiceInterfaceSuite) TestSanitize(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *CalendarServiceInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.Source`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.Calendar`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.CalendarFactory`)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=org.gnome.evolution.dataserver.CalendarView`)
}

func (s *CalendarServiceInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, !(release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04"))
	c.Check(si.Summary, Equals, "allows communication with Evolution Data Service Calendar")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "calendar-service")
}

func (s *CalendarServiceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
