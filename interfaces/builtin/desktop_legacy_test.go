// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

type DesktopLegacyInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&DesktopLegacyInterfaceSuite{
	iface: builtin.MustInterface("desktop-legacy"),
})

const desktopLegacyConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [desktop-legacy]
`

const desktopLegacyCoreYaml = `name: core
version: 0
type: os
slots:
  desktop-legacy:
`

func (s *DesktopLegacyInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, desktopLegacyConsumerYaml, nil, "desktop-legacy")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, desktopLegacyCoreYaml, nil, "desktop-legacy")
}

func (s *DesktopLegacyInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "desktop-legacy")
}

func (s *DesktopLegacyInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *DesktopLegacyInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *DesktopLegacyInterfaceSuite) TestAppArmorSpec(c *C) {
	// connected plug to core slot
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access common desktop legacy methods")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/dbus-accessibility-strict>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(addr="@/tmp/ibus/dbus-*"),`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/mimeinfo.cache r,`)

	// getDesktopFileRules() rules
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `# This leaks the names of snaps with desktop files`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/var/lib/snapd/desktop/applications/ r,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/var/lib/snapd/desktop/applications/@{SNAP_INSTANCE_DESKTOP}_*.desktop r,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/@{SNAP_INSTANCE_DESKTOP}[^_.]*.desktop r,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/[^c]* r,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `deny /var/lib/snapd/desktop/applications/consume[^r]* r,`)

	// connected plug to core slot
	appSet, err = interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *DesktopLegacyInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows privileged access to desktop legacy methods`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "desktop-legacy")
}

func (s *DesktopLegacyInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
