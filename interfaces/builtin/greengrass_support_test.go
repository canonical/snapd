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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type GreengrassSupportInterfaceSuite struct {
	iface         interfaces.Interface
	slotInfo      *snap.SlotInfo
	slot          *interfaces.ConnectedSlot
	plugInfo      *snap.PlugInfo
	plug          *interfaces.ConnectedPlug
	extraSlotInfo *snap.SlotInfo
	extraSlot     *interfaces.ConnectedSlot
	extraPlugInfo *snap.PlugInfo
	extraPlug     *interfaces.ConnectedPlug

	// for the process flavor
	processModePlugInfo *snap.PlugInfo
	processModePlug     *interfaces.ConnectedPlug

	// for the container flavor
	containerModePlugInfo *snap.PlugInfo
	containerModePlug     *interfaces.ConnectedPlug
}

const coreSlotYaml = `name: core
version: 0
type: os
slots:
  network-control:
  greengrass-support:
`
const ggMockPlugSnapInfoYaml = `name: other
version: 1.0
plugs:
 greengrass-support-legacy-container:
  interface: greengrass-support
  flavor: legacy-container
apps:
 app2:
  command: foo
  plugs: [greengrass-support-legacy-container, greengrass-support, network-control]
`

const ggProcessModeMockPlugSnapInfoYaml = `name: other
version: 1.0
plugs:
 greengrass-support-no-container:
  interface: greengrass-support
  flavor: no-container
apps:
 app2:
  command: foo
  plugs: [greengrass-support-no-container, network-control]
`

var _ = Suite(&GreengrassSupportInterfaceSuite{
	iface: builtin.MustInterface("greengrass-support"),
})

func (s *GreengrassSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, ggMockPlugSnapInfoYaml, nil, "greengrass-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, coreSlotYaml, nil, "greengrass-support")
	s.extraPlug, s.extraPlugInfo = MockConnectedPlug(c, ggMockPlugSnapInfoYaml, nil, "network-control")
	s.extraSlot, s.extraSlotInfo = MockConnectedSlot(c, coreSlotYaml, nil, "network-control")

	s.processModePlug, s.processModePlugInfo = MockConnectedPlug(c, ggProcessModeMockPlugSnapInfoYaml, nil, "greengrass-support-no-container")

	s.containerModePlug, s.containerModePlugInfo = MockConnectedPlug(c, ggMockPlugSnapInfoYaml, nil, "greengrass-support-legacy-container")

}

func (s *GreengrassSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "greengrass-support")
}

func (s *GreengrassSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *GreengrassSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *GreengrassSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	restore := apparmor_sandbox.MockFeatures(nil, nil, []string{"userns"}, nil)
	defer restore()

	for _, plug := range []*interfaces.ConnectedPlug{
		s.plug,
		s.containerModePlug,
	} {
		spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
		c.Assert(spec.AddConnectedPlug(s.iface, plug, s.slot), IsNil)
		c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
		c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "mount options=(rw, bind) /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** ,\n")
		c.Check(spec.UsesPtraceTrace(), Equals, true)
		c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "userns,\n")
	}
}

func (s *GreengrassSupportInterfaceSuite) TestProcessModeAppArmorSpec(c *C) {
	// no features so should not support userns
	restore := apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer restore()
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.processModePlug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.processModePlug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "/ ix,\n")
	c.Check(spec.SnippetForTag("snap.other.app2"), Not(testutil.Contains), "mount options=(rw, bind) /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** -> /var/snap/{@{SNAP_NAME},@{SNAP_INSTANCE_NAME}}/** ,\n")
	c.Check(spec.UsesPtraceTrace(), Equals, false)
	c.Check(spec.SnippetForTag("snap.other.app2"), Not(testutil.Contains), "userns,\n")
}

func (s *GreengrassSupportInterfaceSuite) TestSecCompSpec(c *C) {
	for _, plug := range []*interfaces.ConnectedPlug{
		s.plug,
		s.containerModePlug,
	} {
		spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
		c.Assert(spec.AddConnectedPlug(s.iface, plug, s.slot), IsNil)
		c.Check(spec.SnippetForTag("snap.other.app2"), testutil.Contains, "# for overlayfs and various bind mounts\nmount\numount2\npivot_root\n")
	}
}

func (s *GreengrassSupportInterfaceSuite) TestProcessModeSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.processModePlug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.processModePlug, s.slot), IsNil)
	c.Check(spec.SnippetForTag("snap.other.app2"), Not(testutil.Contains), "# for overlayfs and various bind mounts\nmount\numount2\npivot_root\n")
}

func (s *GreengrassSupportInterfaceSuite) TestUdevTaggingDisablingRemoveLast(c *C) {
	for _, plug := range []*interfaces.ConnectedPlug{
		s.plug,
		s.containerModePlug,
	} {
		// make a spec with network-control that has udev tagging
		spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.extraPlug.Snap(), nil))
		c.Assert(spec.AddConnectedPlug(builtin.MustInterface("network-control"), s.extraPlug, s.extraSlot), IsNil)
		c.Assert(spec.Snippets(), HasLen, 3)

		// connect the greengrass-support interface and ensure the spec is now nil
		c.Assert(spec.AddConnectedPlug(s.iface, plug, s.slot), IsNil)
		c.Check(spec.Snippets(), HasLen, 0)
	}
}

func (s *GreengrassSupportInterfaceSuite) TestProcessModeUdevTaggingWorks(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.processModePlug.Snap(), nil))
	// connect the greengrass-support interface and ensure the spec is nil
	c.Assert(spec.AddConnectedPlug(s.iface, s.processModePlug, s.slot), IsNil)
	c.Check(spec.Snippets(), HasLen, 0)

	// add network-control and now the spec is not nil
	c.Assert(spec.AddConnectedPlug(builtin.MustInterface("network-control"), s.extraPlug, s.extraSlot), IsNil)
	c.Assert(spec.Snippets(), Not(HasLen), 0)
}

func (s *GreengrassSupportInterfaceSuite) TestUdevTaggingDisablingRemoveFirst(c *C) {
	for _, plug := range []*interfaces.ConnectedPlug{
		s.plug,
		s.containerModePlug,
	} {
		spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
		// connect the greengrass-support interface and ensure the spec is nil
		c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
		c.Check(spec.Snippets(), HasLen, 0)

		// add network-control and ensure the spec is still nil
		c.Assert(spec.AddConnectedPlug(builtin.MustInterface("network-control"), plug, s.extraSlot), IsNil)
		c.Assert(spec.Snippets(), HasLen, 0)
	}
}

func (s *GreengrassSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *GreengrassSupportInterfaceSuite) TestPermanentSlotAppArmorSessionNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, plug := range []*interfaces.ConnectedPlug{
		s.plug,
		s.containerModePlug,
	} {
		apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
		err := apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
		c.Assert(err, IsNil)
		c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})

		// verify core rule present
		c.Check(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "# /system-data/var/snap/greengrass/x1/ggc-writable/packages/1.7.0/var/worker/overlays/$UUID/upper/\n")
	}
}

func (s *GreengrassSupportInterfaceSuite) TestPermanentSlotAppArmorSessionClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	for _, plug := range []*interfaces.ConnectedPlug{
		s.plug,
		s.containerModePlug,
	} {
		apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
		err := apparmorSpec.AddConnectedPlug(s.iface, plug, s.slot)
		c.Assert(err, IsNil)
		c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})

		// verify core rule not present
		c.Check(apparmorSpec.SnippetForTag("snap.other.app2"), Not(testutil.Contains), "# /system-data/var/snap/greengrass/x1/ggc-writable/packages/1.7.0/var/worker/overlays/$UUID/upper/\n")
	}
}

func (s *GreengrassSupportInterfaceSuite) TestPermanentPlugServiceSnippets(c *C) {
	for _, t := range []struct {
		plug *snap.PlugInfo
		exp  []string
	}{
		{s.plugInfo, []string{"Delegate=true"}},
		{s.containerModePlugInfo, []string{"Delegate=true"}},
		// the process-mode or no-container plug doesn't get Delegate=true
		{s.processModePlugInfo, nil},
	} {
		snips, err := interfaces.PermanentPlugServiceSnippets(s.iface, t.plug)
		c.Assert(err, IsNil)
		c.Check(snips, DeepEquals, t.exp)
	}
}
