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

package builtin_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type desktopLaunchSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&desktopLaunchSuite{
	iface: builtin.MustInterface("desktop-launch"),
})

const desktopLaunchConsumerYaml = `
name: other
version: 0
apps:
 app:
    command: foo
    plugs: [desktop-launch]
`

const desktopLaunchCoreYaml = `name: core
version: 0
type: os
slots:
  desktop-launch:
`

func (s *desktopLaunchSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, desktopLaunchConsumerYaml, nil, "desktop-launch")
	s.slot, s.slotInfo = MockConnectedSlot(c, desktopLaunchCoreYaml, nil, "desktop-launch")
}

func (s *desktopLaunchSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "desktop-launch")
}

func (s *desktopLaunchSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *desktopLaunchSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *desktopLaunchSuite) TestConnectedPlugSnippet(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `Can identify and launch other snaps.`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `member={OpenDesktopEntry,OpenDesktopEntry2}`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `peer=(label=unconfined),`)
}

func (s *desktopLaunchSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *desktopLaunchSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows snaps to identify and launch desktop applications in (or from) other snaps`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "desktop-launch")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "desktop-launch")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "deny-auto-connection: true")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "allow-installation: false")
}

func (s *desktopLaunchSuite) TestDesktopLaunchAndDesktopLegacy(c *C) {
	const desktopLaunchConsumerYamlWithLaunchAndLegacy = `
name: other
version: 0
apps:
 app:
    command: foo
    plugs: [desktop-launch, desktop-legacy]
`

	plug, _ := MockConnectedPlug(c, desktopLaunchConsumerYamlWithLaunchAndLegacy, nil, "desktop-launch")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	err = apparmorSpec.AddConnectedPlug(builtin.MustInterface("desktop-legacy"), plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), Not(testutil.Contains), "# Explicitly deny access to other snap's desktop files")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "Description: Can access common desktop legacy methods.")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "Description: Can identify and launch other snaps.")
}
