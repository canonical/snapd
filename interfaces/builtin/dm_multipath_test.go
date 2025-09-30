// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type dmMultipathInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const dmMultipathMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [dm-multipath]
`

const dmMultipathCoreYaml = `name: core
version: 0
type: os
slots:
  dm-multipath:
`

var _ = Suite(&dmMultipathInterfaceSuite{
	iface: builtin.MustInterface("dm-multipath"),
})

func (s *dmMultipathInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, dmMultipathCoreYaml, nil, "dm-multipath")
	s.plug, s.plugInfo = MockConnectedPlug(c, dmMultipathMockPlugSnapInfoYaml, nil, "dm-multipath")
}

func (s *dmMultipathInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dm-multipath")
}

func (s *dmMultipathInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)
}

func (s *dmMultipathInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/etc/multipath.conf r,")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/etc/multipath/bindings rwk,")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/dev/mapper/control rw,")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "unix (send, receive, connect) type=stream peer=(addr=\"@/org/kernel/linux/storage/multipathd\"),")
}

func (s *dmMultipathInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *dmMultipathInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *dmMultipathInterfaceSuite) TestKModConnectedPlug(c *C) {
	spec := &kmod.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"dm-mod": true,
	})
}

func (s *dmMultipathInterfaceSuite) TestUDevConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), DeepEquals, []string{
		`# dm-multipath
KERNEL=="device-mapper", TAG+="snap_other_app"`,
		`# dm-multipath
KERNEL=="dm-[0-9]*", TAG+="snap_other_app"`,
		`TAG=="snap_other_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_other_app $devpath $major:$minor"`,
	})
}

func (s *dmMultipathInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
