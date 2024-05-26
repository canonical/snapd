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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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

	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: can access documentation of system packages.")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/{,local/}share/doc/{,**} r,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/cups/doc-root/{,**} r,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/gimp/2.0/help/{,**} r,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/javascript/{jquery,sphinxdoc}/{,**}")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/libreoffice/help/{,**} r,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/sphinx_rtd_theme/{,**} r,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/share/xubuntu-docs/{,**} r,")

	updateNS := spec.UpdateNS()
	c.Check(updateNS, testutil.Contains, "  # Mount documentation of system packages\n")
	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/doc/ -> /usr/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/doc/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/local/share/doc/ -> /usr/local/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/local/share/doc/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/local/share/doc/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/cups/doc-root/ -> /usr/share/cups/doc-root/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/cups/doc-root/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/cups/doc-root/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/gimp/2.0/help/ -> /usr/share/gimp/2.0/help/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/gimp/2.0/help/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/gimp/2.0/help/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/gtk-doc/ -> /usr/share/gtk-doc/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/gtk-doc/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/gtk-doc/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/javascript/jquery/ -> /usr/share/javascript/jquery/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/javascript/jquery/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/javascript/jquery/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/javascript/sphinxdoc/ -> /usr/share/javascript/sphinxdoc/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/javascript/sphinxdoc/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/javascript/sphinxdoc/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/libreoffice/help/ -> /usr/share/libreoffice/help/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/libreoffice/help/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/libreoffice/help/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/sphinx_rtd_theme/ -> /usr/share/sphinx_rtd_theme/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/sphinx_rtd_theme/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/sphinx_rtd_theme/,\n")

	c.Check(updateNS, testutil.Contains, "  mount options=(bind) /var/lib/snapd/hostfs/usr/share/xubuntu-docs/ -> /usr/share/xubuntu-docs/,\n")
	c.Check(updateNS, testutil.Contains, "  remount options=(bind, ro) /usr/share/xubuntu-docs/,\n")
	c.Check(updateNS, testutil.Contains, "  umount /usr/share/xubuntu-docs/,\n")
	// check mimic bits
	c.Check(updateNS, testutil.Contains, "  # Writable mimic /usr/share/libreoffice\n")
	c.Check(updateNS, testutil.Contains, "  mount fstype=tmpfs options=(rw) tmpfs -> \"/usr/share/\",\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/\" r,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/cups/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/gimp/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/gimp/2.0/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/javascript/jquery/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/javascript/sphinxdoc/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/libreoffice/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/usr/share/sphinx_rtd_theme/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/cups/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/cups/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/gimp/2.0/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/gimp/2.0/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/javascript/jquery/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/javascript/jquery/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/javascript/sphinxdoc/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/javascript/sphinxdoc/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/libreoffice/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/libreoffice/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/sphinx_rtd_theme/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  \"/tmp/.snap/usr/share/sphinx_rtd_theme/*/\" rw,\n")
	c.Check(updateNS, testutil.Contains, "  mount options=(bind, rw) \"/tmp/.snap/usr/share/*\" -> \"/usr/share/*\",\n")
}

func (s *systemPackagesDocSuite) TestMountSpec(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)

	entries := spec.MountEntries()
	c.Assert(entries, HasLen, 10)
	c.Check(entries[0].Name, Equals, "/var/lib/snapd/hostfs/usr/share/doc")
	c.Check(entries[0].Dir, Equals, "/usr/share/doc")
	c.Check(entries[1].Name, Equals, "/var/lib/snapd/hostfs/usr/local/share/doc")
	c.Check(entries[1].Dir, Equals, "/usr/local/share/doc")
	c.Check(entries[1].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[2].Name, Equals, "/var/lib/snapd/hostfs/usr/share/cups/doc-root")
	c.Check(entries[2].Dir, Equals, "/usr/share/cups/doc-root")
	c.Check(entries[2].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[3].Name, Equals, "/var/lib/snapd/hostfs/usr/share/gimp/2.0/help")
	c.Check(entries[3].Dir, Equals, "/usr/share/gimp/2.0/help")
	c.Check(entries[3].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[4].Name, Equals, "/var/lib/snapd/hostfs/usr/share/gtk-doc")
	c.Check(entries[4].Dir, Equals, "/usr/share/gtk-doc")
	c.Check(entries[4].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[5].Name, Equals, "/var/lib/snapd/hostfs/usr/share/javascript/jquery")
	c.Check(entries[5].Dir, Equals, "/usr/share/javascript/jquery")
	c.Check(entries[5].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[6].Name, Equals, "/var/lib/snapd/hostfs/usr/share/javascript/sphinxdoc")
	c.Check(entries[6].Dir, Equals, "/usr/share/javascript/sphinxdoc")
	c.Check(entries[6].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[7].Name, Equals, "/var/lib/snapd/hostfs/usr/share/libreoffice/help")
	c.Check(entries[7].Dir, Equals, "/usr/share/libreoffice/help")
	c.Check(entries[7].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[8].Name, Equals, "/var/lib/snapd/hostfs/usr/share/sphinx_rtd_theme")
	c.Check(entries[8].Dir, Equals, "/usr/share/sphinx_rtd_theme")
	c.Check(entries[8].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[9].Name, Equals, "/var/lib/snapd/hostfs/usr/share/xubuntu-docs")
	c.Check(entries[9].Dir, Equals, "/usr/share/xubuntu-docs")
	c.Check(entries[9].Options, DeepEquals, []string{"bind", "ro"})

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

// Variant of the test for base: bare on the plug

type systemPackagesDocBareBaseSuite struct {
	systemPackagesDocSuite
}

var _ = Suite(&systemPackagesDocBareBaseSuite{
	systemPackagesDocSuite{
		iface: builtin.MustInterface("system-packages-doc"),
	},
})

const systemPackagesDocBareBaseConsumerYaml = systemPackagesDocConsumerYaml + `
base: bare
`

func (s *systemPackagesDocBareBaseSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, systemPackagesDocBareBaseConsumerYaml, nil, "system-packages-doc")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, systemPackagesDocCoreYaml, nil, "system-packages-doc")
}

func (s *systemPackagesDocBareBaseSuite) TestAppArmorSpec(c *C) {
	s.systemPackagesDocSuite.TestAppArmorSpec(c)

	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	updateNS := spec.UpdateNS()
	c.Check(strings.Join(updateNS, "\n"), testutil.Contains, "  # Writable mimic over / - extra permissions generalized\n")
}
