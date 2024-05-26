// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type IioInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	// OS Snap
	testSlot1Info       *snap.SlotInfo
	testSlotCleanedInfo *snap.SlotInfo

	// Gadget Snap
	testUDev1                 *interfaces.ConnectedSlot
	testUDev1Info             *snap.SlotInfo
	testUDev2                 *interfaces.ConnectedSlot
	testUDev2Info             *snap.SlotInfo
	testUDev3                 *interfaces.ConnectedSlot
	testUDev3Info             *snap.SlotInfo
	testUDevBadValue1         *interfaces.ConnectedSlot
	testUDevBadValue1Info     *snap.SlotInfo
	testUDevBadValue2         *interfaces.ConnectedSlot
	testUDevBadValue2Info     *snap.SlotInfo
	testUDevBadValue3         *interfaces.ConnectedSlot
	testUDevBadValue3Info     *snap.SlotInfo
	testUDevBadValue4         *interfaces.ConnectedSlot
	testUDevBadValue4Info     *snap.SlotInfo
	testUDevBadValue5         *interfaces.ConnectedSlot
	testUDevBadValue5Info     *snap.SlotInfo
	testUDevBadValue6         *interfaces.ConnectedSlot
	testUDevBadValue6Info     *snap.SlotInfo
	testUDevBadValue7         *interfaces.ConnectedSlot
	testUDevBadValue7Info     *snap.SlotInfo
	testUDevBadValue8         *interfaces.ConnectedSlot
	testUDevBadValue8Info     *snap.SlotInfo
	testUDevBadInterface1     *interfaces.ConnectedSlot
	testUDevBadInterface1Info *snap.SlotInfo

	// Consuming Snap
	testPlugPort1     *interfaces.ConnectedPlug
	testPlugPort1Info *snap.PlugInfo
}

var _ = Suite(&IioInterfaceSuite{
	iface: builtin.MustInterface("iio"),
})

func (s *IioInterfaceSuite) SetUpTest(c *C) {
	// Mock for OS Snap
	osSnapInfo := snaptest.MockInfo(c, `
name: ubuntu-core
version: 0
type: os
slots:
  test-port-1:
    interface: iio
    path: /dev/iio:device0
  test-port-unclean:
    interface: iio
    path: ///dev/iio:device1
`, nil)
	s.testSlot1Info = osSnapInfo.Slots["test-port-1"]
	s.testSlotCleanedInfo = osSnapInfo.Slots["test-port-unclean"]

	// Mock for Gadget Snap
	gadgetSnapInfo := snaptest.MockInfo(c, `
name: some-device
version: 0
type: gadget
slots:
  test-udev-1:
    interface: iio
    path: /dev/iio:device1
  test-udev-2:
    interface: iio
    path: /dev/iio:device2
  test-udev-3:
    interface: iio
    path: /dev/iio:device10000
  test-udev-bad-value-1:
    interface: iio
    path: /dev/iio
  test-udev-bad-value-2:
    interface: iio
    path: /dev/iio:devicea
  test-udev-bad-value-3:
    interface: iio
    path: /dev/iio:device2a
  test-udev-bad-value-4:
    interface: iio
    path: /dev/foo-0
  test-udev-bad-value-5:
    interface: iio
    path: /dev/iio:devicefoo
  test-udev-bad-value-6:
    interface: iio
    path: /dev/iio-device0
  test-udev-bad-value-7:
    interface: iio
    path: ""
  test-udev-bad-value-8:
    interface: iio
  test-udev-bad-interface-1:
    interface: other-interface
`, nil)
	s.testUDev1Info = gadgetSnapInfo.Slots["test-udev-1"]
	s.testUDev1 = interfaces.NewConnectedSlot(s.testUDev1Info, nil, nil)
	s.testUDev2Info = gadgetSnapInfo.Slots["test-udev-2"]
	s.testUDev2 = interfaces.NewConnectedSlot(s.testUDev2Info, nil, nil)
	s.testUDev3Info = gadgetSnapInfo.Slots["test-udev-3"]
	s.testUDev3 = interfaces.NewConnectedSlot(s.testUDev3Info, nil, nil)
	s.testUDevBadValue1Info = gadgetSnapInfo.Slots["test-udev-bad-value-1"]
	s.testUDevBadValue1 = interfaces.NewConnectedSlot(s.testUDevBadValue1Info, nil, nil)
	s.testUDevBadValue2Info = gadgetSnapInfo.Slots["test-udev-bad-value-2"]
	s.testUDevBadValue2 = interfaces.NewConnectedSlot(s.testUDevBadValue2Info, nil, nil)
	s.testUDevBadValue3Info = gadgetSnapInfo.Slots["test-udev-bad-value-3"]
	s.testUDevBadValue3 = interfaces.NewConnectedSlot(s.testUDevBadValue3Info, nil, nil)
	s.testUDevBadValue4Info = gadgetSnapInfo.Slots["test-udev-bad-value-4"]
	s.testUDevBadValue4 = interfaces.NewConnectedSlot(s.testUDevBadValue4Info, nil, nil)
	s.testUDevBadValue5Info = gadgetSnapInfo.Slots["test-udev-bad-value-5"]
	s.testUDevBadValue5 = interfaces.NewConnectedSlot(s.testUDevBadValue5Info, nil, nil)
	s.testUDevBadValue6Info = gadgetSnapInfo.Slots["test-udev-bad-value-6"]
	s.testUDevBadValue6 = interfaces.NewConnectedSlot(s.testUDevBadValue6Info, nil, nil)
	s.testUDevBadValue7Info = gadgetSnapInfo.Slots["test-udev-bad-value-7"]
	s.testUDevBadValue7 = interfaces.NewConnectedSlot(s.testUDevBadValue7Info, nil, nil)
	s.testUDevBadValue8Info = gadgetSnapInfo.Slots["test-udev-bad-value-8"]
	s.testUDevBadValue8 = interfaces.NewConnectedSlot(s.testUDevBadValue8Info, nil, nil)
	s.testUDevBadInterface1Info = gadgetSnapInfo.Slots["test-udev-bad-interface-1"]
	s.testUDevBadInterface1 = interfaces.NewConnectedSlot(s.testUDevBadInterface1Info, nil, nil)

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
version: 0
plugs:
  plug-for-port-1:
    interface: iio
apps:
  app-accessing-1-port:
    command: foo
    plugs: [iio]
`, nil)
	s.testPlugPort1Info = consumingSnapInfo.Plugs["plug-for-port-1"]
	s.testPlugPort1 = interfaces.NewConnectedPlug(s.testPlugPort1Info, nil, nil)
}

func (s *IioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "iio")
}

func (s *IioInterfaceSuite) TestSanitizeCoreSnapSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlot1Info), IsNil)
	// Verify historically filepath.Clean()d paths are still valid
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlotCleanedInfo), IsNil)
}

func (s *IioInterfaceSuite) TestSanitizeBadGadgetSnapSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue1Info), ErrorMatches, "iio path attribute must be a valid device node")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue2Info), ErrorMatches, "iio path attribute must be a valid device node")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue3Info), ErrorMatches, "iio path attribute must be a valid device node")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue4Info), ErrorMatches, "iio path attribute must be a valid device node")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue5Info), ErrorMatches, "iio path attribute must be a valid device node")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue6Info), ErrorMatches, "iio path attribute must be a valid device node")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue7Info), ErrorMatches, "iio slot must have a path attribute")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testUDevBadValue8Info), ErrorMatches, "iio slot must have a path attribute")
}

func (s *IioInterfaceSuite) TestConnectedPlugUDevSnippets(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.testPlugPort1.Snap(), nil))

	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev1), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# iio
KERNEL=="iio:device1", TAG+="snap_client-snap_app-accessing-1-port"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_client-snap_app-accessing-1-port", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_client-snap_app-accessing-1-port $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *IioInterfaceSuite) TestConnectedPlugAppArmorSingleSnippet(c *C) {
	expectedSnippet := `
# Description: Give access to a specific IIO device on the system.

/dev/iio:device1 rw,
/sys/bus/iio/devices/iio:device1/ r,
/sys/bus/iio/devices/iio:device1/** rwk,

/sys/devices/**/iio:device1/ r,  # Add any condensed parametric rules
/sys/devices/**/iio:device1/** rwk,  # Add any condensed parametric rules`
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.testPlugPort1.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev1))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-1-port"})
	snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-1-port")
	c.Assert(snippet, Equals, expectedSnippet)
}

func (s *IioInterfaceSuite) TestConnectedPlugAppArmorSnippetsMultipleOptimized(c *C) {
	expectedSnippet := `
# Description: Give access to a specific IIO device on the system.

/dev/iio:device1 rw,
/sys/bus/iio/devices/iio:device1/ r,
/sys/bus/iio/devices/iio:device1/** rwk,


# Description: Give access to a specific IIO device on the system.

/dev/iio:device2 rw,
/sys/bus/iio/devices/iio:device2/ r,
/sys/bus/iio/devices/iio:device2/** rwk,

/sys/devices/**/iio:device{1,2}/ r,  # Add any condensed parametric rules
/sys/devices/**/iio:device{1,2}/** rwk,  # Add any condensed parametric rules`
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.testPlugPort1.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev1))

	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.testPlugPort1, s.testUDev2))

	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.client-snap.app-accessing-1-port"})
	// XXX: the tag snap.client-snap.app-accessing-1-port is
	// misleading when you are testing for '1' and '2' ports
	snippet := apparmorSpec.SnippetForTag("snap.client-snap.app-accessing-1-port")
	c.Assert(snippet, Equals, expectedSnippet)
}

func (s *IioInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *IioInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
