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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type systemPackagesDocSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&systemPackagesDocSuite{iface: builtin.MustInterface("system-packages-doc")})

const systemPackagesDocConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [system-packages-doc]
`

const systemPackagesDocCoreYaml = `name: core
version: 0
type: os
slots:
  system-packages-doc:
`

func (s *systemPackagesDocSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, systemPackagesDocConsumerYaml, nil, "system-packages-doc")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, systemPackagesDocCoreYaml, nil, "system-packages-doc")
}

func (s *systemPackagesDocSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *systemPackagesDocSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "system-packages-doc")
}

func (s *systemPackagesDocSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *systemPackagesDocSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *systemPackagesDocSuite) TestAppArmorSpec(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: can access documentation of system packages.")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/doc/{,**} r,")

	updateNS := spec.UpdateNS()
	c.Check(updateNS, testutil.Contains, "  # Mount documentation of system packages\n")
	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/doc/ -> /usr/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/doc/,\n")
}

func (s *systemPackagesDocSuite) TestMountSpec(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

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

func (s *systemPackagesDocSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to documentation of system packages`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "system-packages-doc")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
	c.Assert(si.AffectsPlugOnRefresh, Equals, true)
}

func (s *systemPackagesDocSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
