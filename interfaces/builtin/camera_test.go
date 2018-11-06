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
	"github.com/snapcore/snapd/interfaces/hotplug"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type CameraInterfaceSuite struct {
	iface       interfaces.Interface
	slot        *interfaces.ConnectedSlot
	hotplugSlot *interfaces.ConnectedSlot
	slotInfo    *snap.SlotInfo
	plug        *interfaces.ConnectedPlug
	plugInfo    *snap.PlugInfo
}

var _ = Suite(&CameraInterfaceSuite{
	iface: builtin.MustInterface("camera"),
})

const cameraConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [camera]
`

const cameraCoreYaml = `name: core
version: 0
type: os
slots:
  camera:
`

func (s *CameraInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, cameraConsumerYaml, nil, "camera")
	s.slot, s.slotInfo = MockConnectedSlot(c, cameraCoreYaml, nil, "camera")

	attrs := map[string]interface{}{"path": "/dev/video0", "devpath": "/sys/somepath", "minor": "1"}
	hotplugSlotInfo := MockHotplugSlot(c, cameraCoreYaml, nil, "1234", "camera", "integratedwebcam1", attrs)
	s.hotplugSlot = interfaces.NewConnectedSlot(hotplugSlotInfo, nil, nil)
}

func (s *CameraInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "camera")
}

func (s *CameraInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "camera",
		Interface: "camera",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"camera slots are reserved for the core snap")
}

func (s *CameraInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *CameraInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/video[0-9]* rw")
}

func (s *CameraInterfaceSuite) TestAppArmorSpecHotplug(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.hotplugSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/video0 rw")
}

func (s *CameraInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# camera
KERNEL=="video[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_consumer_app", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`)
}

func (s *CameraInterfaceSuite) TestUDevSpecHotplug(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.hotplugSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# camera
KERNEL=="video0", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_consumer_app", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`)
}

func (s *CameraInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to all cameras`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "camera")
}

func (s *CameraInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *CameraInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *CameraInterfaceSuite) TestHotplugRequiresV4LSubsystem(c *C) {
	hotplugIface := s.iface.(hotplug.Definer)

	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"SUBSYSTEM": "tty",
		"DEVNAME":   "/dev/ttyUSB0",
		"DEVPATH":   "/other",
	})
	c.Assert(err, IsNil)

	var spec hotplug.Specification
	c.Assert(hotplugIface.HotplugDeviceDetected(di, &spec), IsNil)
	c.Assert(spec.Slot(), IsNil)
}

func (s *CameraInterfaceSuite) TestHotplugDeviceDetected(c *C) {
	hotplugIface := s.iface.(hotplug.Definer)

	di, err := hotplug.NewHotplugDeviceInfo(map[string]string{
		"SUBSYSTEM":       "video4linux",
		"ID_SERIAL_SHORT": "200901010001",
		"ID_V4L_PRODUCT":  "Integrated_Webcam_HD: Integrate",
		"DEVNAME":         "/dev/video0",
		"DEVPATH":         "/devices/pci0000:00/0000:00:14.0/usb1/1-11/1-11:1.0/video4linux/video0",
		"MAJOR":           "81",
		"MINOR":           "0",
	})
	c.Assert(err, IsNil)

	var spec hotplug.Specification
	c.Assert(hotplugIface.HotplugDeviceDetected(di, &spec), IsNil)

	slotSpec := spec.Slot()
	c.Assert(slotSpec, NotNil)
	c.Assert(slotSpec.Attrs, DeepEquals, map[string]interface{}{
		"path":    "/dev/video0",
		"devpath": "/sys/devices/pci0000:00/0000:00:14.0/usb1/1-11/1-11:1.0/video4linux/video0",
		"minor":   "0",
	})
}
