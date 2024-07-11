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
	"io/fs"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type displayControlInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug

	tmpdir string
}

var _ = Suite(&displayControlInterfaceSuite{
	iface: builtin.MustInterface("display-control"),
})

const displayControlConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [display-control]
`

const displayControlCoreYaml = `name: core
version: 0
type: os
slots:
  display-control:
`

func (s *displayControlInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, displayControlConsumerYaml, nil, "display-control")
	s.slot, s.slotInfo = MockConnectedSlot(c, displayControlCoreYaml, nil, "display-control")

	s.tmpdir = c.MkDir()
}

func (s *displayControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "display-control")
}

func (s *displayControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *displayControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *displayControlInterfaceSuite) TestAppArmorSpec(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.tmpdir, "foo_backlight"), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(s.tmpdir, "bar_backlight"), 0755), IsNil)
	builtin.MockReadDir(&s.BaseTest, func(path string) ([]fs.DirEntry, error) {
		return os.ReadDir(s.tmpdir)
	})
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		return "(dereferenced)" + path, nil
	})
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/sys/class/backlight/ r,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "autodetected backlight: bar_backlight\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "(dereferenced)/sys/class/backlight/bar_backlight/{,**} r,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "autodetected backlight: foo_backlight\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "(dereferenced)/sys/class/backlight/foo_backlight/{,**} r,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/devices/platform/lvds_backlight/backlight/lvds_backlight/brightness rw,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/devices/platform/lvds_backlight/backlight/lvds_backlight/bl_power rw,`)
}

func (s *displayControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows configuring display parameters`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "display-control")
}

func (s *displayControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *displayControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
