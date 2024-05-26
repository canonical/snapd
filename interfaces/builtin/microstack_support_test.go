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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type microStackSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const microStackSupportMockPlugSnapInfoYaml = `name: microstack
version: 1.0
apps:
 app:
  command: foo
  plugs: [microstack-support]
`

const microstackSupportCoreYaml = `name: core
version: 0
type: os
slots:
  microstack-support:
`

var _ = Suite(&microStackSupportInterfaceSuite{
	iface: builtin.MustInterface("microstack-support"),
})

func (s *microStackSupportInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, microstackSupportCoreYaml, nil, "microstack-support")
	s.plug, s.plugInfo = MockConnectedPlug(c, microStackSupportMockPlugSnapInfoYaml, nil, "microstack-support")
}

func (s *microStackSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "microstack-support")
}

func (s *microStackSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	// connected plugs have a non-nil security snippet for seccomp
	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	seccompSpec := seccomp.NewSpecification(appSet)
	mylog.Check(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(seccompSpec.Snippets(), HasLen, 1)
}

func (s *microStackSupportInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.microstack.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.microstack.app"), testutil.Contains, "/dev/vhost-net rw,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.microstack.app"), testutil.Contains, "/dev/microstack-*/{,**} rw,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.microstack.app"), testutil.Contains, "unmount /run/netns/ovnmeta-*,\n")

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	seccompSpec := seccomp.NewSpecification(appSet)
	mylog.Check(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.microstack.app"})
	c.Check(seccompSpec.SnippetForTag("snap.microstack.app"), testutil.Contains, "mknod - |S_IFBLK -\nmknodat - - |S_IFBLK -")
}

func (s *microStackSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *microStackSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *microStackSupportInterfaceSuite) TestKModConnectedPlug(c *C) {
	spec := &kmod.Specification{}
	mylog.Check(spec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"vhost":           true,
		"vhost-net":       true,
		"vhost-scsi":      true,
		"vhost-vsock":     true,
		"pci-stub":        true,
		"vfio":            true,
		"vfio-pci":        true,
		"nbd":             true,
		"dm-mod":          true,
		"dm-thin-pool":    true,
		"dm-snapshot":     true,
		"iscsi-tcp":       true,
		"target-core-mod": true,
	})
}

func (s *microStackSupportInterfaceSuite) TestUDevConnectedPlug(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := udev.NewSpecification(appSet)
	mylog.
		// no udev specs because the interface controls it's own device cgroups
		Check(spec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *microStackSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
