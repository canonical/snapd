// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type PlymouthReplaceInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&PlymouthReplaceInterfaceSuite{
	iface: builtin.MustInterface("plymouth-replace"),
})

func (s *PlymouthReplaceInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 0
apps:
 app:
  command: foo
  daemon: simple
  plugs: [plymouth-replace]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 plymouth-replace:
  interface: plymouth-replace
`

	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "plymouth-replace")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "plymouth-replace")
}

func (s *PlymouthReplaceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "plymouth-replace")
}

func (s *PlymouthReplaceInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *PlymouthReplaceInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *PlymouthReplaceInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/usr/bin/plymouth ixr,`)

	systemdSpec := systemd.NewSpecification(s.plug.AppSet())
	err = systemdSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Check(systemdSpec.DropIns(), DeepEquals, map[string]map[string]string{
		"snap.other.app.service": {
			"plymouth-replace": `
[Unit]
Conflicts=plymouth-quit.service
After=plymouth-quit.service
OnFailure=plymouth-quit.service
Conflicts=getty@tty1.service
After=getty@tty1.service
`,
		},
	})
}

func (s *PlymouthReplaceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
