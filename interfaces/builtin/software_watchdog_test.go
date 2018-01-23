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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type softwareWatchdogSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&softwareWatchdogSuite{
	iface: builtin.MustInterface("software-watchdog"),
})

const softwareWatchdogMockSlotSnapInfoYaml = `name: software-watchdog
version: 1.0
type: os
slots:
  software-watchdog:
    interface: software-watchdog
`
const softwareWatchdogMockPlugSnapInfoYaml = `name: software-watchdog-client
version: 1.0
apps:
 app2:
  command: foo
  plugs: [software-watchdog]
`

func (s *softwareWatchdogSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = builtin.MockConnectedSlot(c, softwareWatchdogMockSlotSnapInfoYaml, nil, "software-watchdog")
	s.plug, s.plugInfo = builtin.MockConnectedPlug(c, softwareWatchdogMockPlugSnapInfoYaml, nil, "software-watchdog")
}

func (s *softwareWatchdogSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "software-watchdog")
}

func (s *softwareWatchdogSuite) TestBeforePrepareSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	nonOsSoftwareWatchdogSlotSnapInfoYaml := `name: non-os-software-watchdog
version: 1.0
slots:
  software-watchdog:
    interface: software-watchdog
`
	si := builtin.MockSlot(c, nonOsSoftwareWatchdogSlotSnapInfoYaml, nil, "software-watchdog")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, si), ErrorMatches,
		"software-watchdog slots are reserved for the core snap")
}

func (s *softwareWatchdogSuite) TestBeforePreparePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *softwareWatchdogSuite) TestAppArmorConnectedPlugNotifySocketDefault(c *C) {
	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return ""
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.software-watchdog-client.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.software-watchdog-client.app2"), testutil.Contains, "\n/run/systemd/notify w,")
}

func (s *softwareWatchdogSuite) TestAppArmorConnectedPlugNotifySocketEnv(c *C) {
	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return "/foo/bar"
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.software-watchdog-client.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.software-watchdog-client.app2"), testutil.Contains, "\n/foo/bar w,")
}

func (s *softwareWatchdogSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
