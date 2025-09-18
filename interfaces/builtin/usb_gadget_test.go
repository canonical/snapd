// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type UsbGadgetInterfaceSuite struct {
	testutil.BaseTest
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&UsbGadgetInterfaceSuite{
	iface: builtin.MustInterface("usb-gadget"),
})

const usbGadgetConsumerYaml = `name: consumer
version: 0
plugs:
 usbg:
  interface: usb-gadget
apps:
 app:
  plugs: [usbg]
`

const usbGadgetWithFFSConsumerYaml = `name: consumer
version: 0
plugs:
 usbg:
  interface: usb-gadget
  ffs-mounts:
  - name: ffs-dev0
    where: /media/**
  - name: ffs-dev1
    where: /dev/ffs-dev1
    persistent: true
  - name: ffs-mtp
    where: $SNAP_COMMON/**
apps:
 app:
  plugs: [usbg]
`

const usbGadgetCoreYaml = `name: core
version: 0
type: os
slots:
  usb-gadget:
`

func (s *UsbGadgetInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.slot, s.slotInfo = MockConnectedSlot(c, usbGadgetCoreYaml, nil, "usb-gadget")
	s.AddCleanup(systemd.MockSystemdVersion(210, nil))
}

func (s *UsbGadgetInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "usb-gadget")
}

func (s *UsbGadgetInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *UsbGadgetInterfaceSuite) TestSanitizePlug(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, usbGadgetConsumerYaml, nil, "usbg")
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *UsbGadgetInterfaceSuite) TestSanitizePlugWithMounts(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, usbGadgetWithFFSConsumerYaml, nil, "usbg")
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *UsbGadgetInterfaceSuite) TestSanitizePlugOldSystemd(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, usbGadgetConsumerYaml, nil, "usbg")
	restore := systemd.MockSystemdVersion(208, nil)
	defer restore()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, ErrorMatches, `systemd version 208 is too old \(expected at least 209\)`)
}

func (s *UsbGadgetInterfaceSuite) TestSecCompSpec(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, usbGadgetConsumerYaml, nil, "usbg")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)

	// Should not contain any snippets for mount/umount as there are no ffs-mounts
	c.Assert(spec.SecurityTags(), HasLen, 0)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "mount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "umount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "umount2\n")
}

func (s *UsbGadgetInterfaceSuite) TestSecCompSpecWithMounts(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, usbGadgetWithFFSConsumerYaml, nil, "usbg")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)

	// Should contain snippets for mount/umount as there are ffs-mounts
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "mount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount2\n")
}

func (s *UsbGadgetInterfaceSuite) TestAppArmorSpec(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, usbGadgetConsumerYaml, nil, "usbg")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/kernel/config/usb_gadget/`)
}

func (s *UsbGadgetInterfaceSuite) TestAppArmorSpecWithMounts(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, usbGadgetWithFFSConsumerYaml, nil, "usbg")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/kernel/config/usb_gadget/`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `capability sys_admin,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/{,usr/}bin/mount ixr,`)

	expectedMountLine1 := `mount fstype=(functionfs) "ffs-dev0" -> "/media/**{,/}",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedMountLine1)

	expectedAccessLine1 := `/media/**{,/} rw,`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedAccessLine1)

	expectedMountLine2 := `mount fstype=(functionfs) "ffs-dev1" -> "/dev/ffs-dev1{,/}",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedMountLine2)
	expectedAccessLine2 := `/dev/ffs-dev1{,/} rw,`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedAccessLine2)

	expectedMountLine3 := `mount fstype=(functionfs) "ffs-mtp" -> "/var/snap/consumer/common/**{,/}",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedMountLine3)
	expectedUmountLine3 := `umount "/var/snap/consumer/common/**{,/}",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedUmountLine3)
	expectedAccessLine3 := `/var/snap/consumer/common/**{,/} rw,`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedAccessLine3)
}

func (s *UsbGadgetInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the usb gadget API`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "usb-gadget")
}

func (s *UsbGadgetInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *UsbGadgetInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
