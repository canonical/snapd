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
	"fmt"
	"os"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type uioInterfaceSuite struct {
	testutil.BaseTest
	iface           interfaces.Interface
	slotGadgetInfo0 *snap.SlotInfo
	slotGadgetInfo1 *snap.SlotInfo
	slotGadget0     *interfaces.ConnectedSlot
	slotGadget1     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&uioInterfaceSuite{
	iface: builtin.MustInterface("uio"),
})

func (s *uioInterfaceSuite) SetUpTest(c *C) {
	info := snaptest.MockInfo(c, `
name: gadget
version: 0
type: gadget
slots:
  uio-0:
    interface: uio
    path: /dev/uio0
  uio-1:
    interface: uio
    path: /dev/uio1
`, nil)
	s.slotGadgetInfo0 = info.Slots["uio-0"]
	s.slotGadgetInfo1 = info.Slots["uio-1"]
	s.slotGadget0 = interfaces.NewConnectedSlot(s.slotGadgetInfo0, nil, nil)
	s.slotGadget1 = interfaces.NewConnectedSlot(s.slotGadgetInfo1, nil, nil)

	info = snaptest.MockInfo(c, `
name: consumer
version: 0
plugs:
  uio:
    interface: uio
apps:
  app:
    command: foo
`, nil)
	s.plugInfo = info.Plugs["uio"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *uioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "uio")
}

func (s *uioInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotGadgetInfo0), IsNil)
	brokenSlot := snaptest.MockInfo(c, `
name: broken-gadget
version: 1
type: gadget
slots:
  uio:
    path: /dev/foo
`, nil).Slots["uio"]
	c.Assert(interfaces.BeforePrepareSlot(s.iface, brokenSlot), ErrorMatches, `slot "broken-gadget:uio" path attribute must be a valid UIO device node`)
}

func (s *uioInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slotGadget0), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# uio
SUBSYSTEM=="uio", KERNEL=="uio0", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *uioInterfaceSuite) TestAppArmorConnectedPlugIgnoresMissingConfigFile(c *C) {
	log, restore := logger.MockLogger()
	defer restore()

	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		c.Assert(path, Matches, "/sys/class/uio/uio[0-1]+/device/config")
		return "", os.ErrNotExist
	})

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	// Simulate two UIO connections.
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slotGadget0), IsNil)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slotGadget1), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, ""+
		"/dev/uio0 rw,\n"+
		"/dev/uio1 rw,\n"+
		"/sys/devices/platform/**/uio/uio[0-9]** r,  # common rule for all uio connections")

	c.Assert(log.String(), testutil.Contains, "cannot configure not existing uio device config file /sys/class/uio/uio0/device/config")
	c.Assert(log.String(), testutil.Contains, "cannot configure not existing uio device config file /sys/class/uio/uio1/device/config")
}

func (s *uioInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	builtin.MockEvalSymlinks(&s.BaseTest, func(path string) (string, error) {
		c.Assert(path, Matches, "/sys/class/uio/uio[0-1]+/device/config")
		device := "uio0"
		if !strings.Contains(path, device) {
			device = "uio1"
		}
		// Not representative of actual resolved symlink
		target := fmt.Sprint("/sys/devices/", device, "/config")
		return target, nil
	})

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	// Simulate two UIO connections.
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slotGadget0), IsNil)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slotGadget1), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, ""+
		"/dev/uio0 rw,\n"+
		"/dev/uio1 rw,\n"+
		"/sys/devices/uio0/config rwk,\n"+
		"/sys/devices/uio1/config rwk,\n"+
		"/sys/devices/platform/**/uio/uio[0-9]** r,  # common rule for all uio connections")
}

func (s *uioInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, "allows access to specific uio device")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "uio")
}

func (s *uioInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *uioInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
