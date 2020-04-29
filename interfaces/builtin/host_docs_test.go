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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type hostDocsSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&hostDocsSuite{iface: builtin.MustInterface("host-docs")})

const hostDocsConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [host-docs]
`

const hostDocsCoreYaml = `name: core
version: 0
type: os
slots:
  host-docs:
`

func (s *hostDocsSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, hostDocsConsumerYaml, nil, "host-docs")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, hostDocsCoreYaml, nil, "host-docs")
}

func (s *hostDocsSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *hostDocsSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "host-docs")
}

func (s *hostDocsSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *hostDocsSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *hostDocsSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access documentation stored on the host in /usr/share/doc")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/doc/{,**} r,")

	updateNS := spec.UpdateNS()
	c.Check(updateNS, testutil.Contains, "  # Mount host documentation\n")
	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/doc/ -> /usr/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/doc/,\n")
}

func (s *hostDocsSuite) TestMountSpec(c *C) {
	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)

	entries := spec.MountEntries()
	c.Assert(entries, HasLen, 1)
	c.Check(entries[0].Name, Equals, "/var/lib/snapd/hostfs/usr/share/doc")
	c.Check(entries[0].Dir, Equals, "/usr/share/doc")
	c.Check(entries[0].Options, DeepEquals, []string{"bind", "ro"})

	entries = spec.UserMountEntries()
	c.Assert(entries, HasLen, 0)
}

func (s *hostDocsSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to documentation stored on the host`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "host-docs")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *hostDocsSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
