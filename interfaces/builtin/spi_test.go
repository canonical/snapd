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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type spiInterfaceSuite struct {
	testutil.BaseTest
	iface interfaces.Interface

	slotOs1Info       *snap.SlotInfo
	slotOs1           *interfaces.ConnectedSlot
	slotOs2Info       *snap.SlotInfo
	slotOs2           *interfaces.ConnectedSlot
	slotOs3Info       *snap.SlotInfo
	slotOs3           *interfaces.ConnectedSlot
	slotOsCleanedInfo *snap.SlotInfo
	slotOsCleaned     *interfaces.ConnectedSlot

	slotGadget1Info    *snap.SlotInfo
	slotGadget1        *interfaces.ConnectedSlot
	slotGadget2Info    *snap.SlotInfo
	slotGadget2        *interfaces.ConnectedSlot
	slotGadget3Info    *snap.SlotInfo
	slotGadget3        *interfaces.ConnectedSlot
	slotGadgetBad1Info *snap.SlotInfo
	slotGadgetBad1     *interfaces.ConnectedSlot
	slotGadgetBad2Info *snap.SlotInfo
	slotGadgetBad2     *interfaces.ConnectedSlot
	slotGadgetBad3Info *snap.SlotInfo
	slotGadgetBad3     *interfaces.ConnectedSlot
	slotGadgetBad4Info *snap.SlotInfo
	slotGadgetBad4     *interfaces.ConnectedSlot
	slotGadgetBad5Info *snap.SlotInfo
	slotGadgetBad5     *interfaces.ConnectedSlot
	slotGadgetBad6Info *snap.SlotInfo
	slotGadgetBad6     *interfaces.ConnectedSlot

	plug1Info *snap.PlugInfo
	plug1     *interfaces.ConnectedPlug
	plug2Info *snap.PlugInfo
	plug2     *interfaces.ConnectedPlug
	plug3Info *snap.PlugInfo
	plug3     *interfaces.ConnectedPlug
}

var _ = Suite(&spiInterfaceSuite{
	iface: builtin.MustInterface("spi"),
})

func (s *spiInterfaceSuite) SetUpTest(c *C) {
	info := snaptest.MockInfo(c, `
name: core
version: 0
type: os
slots:
  spi-1:
    interface: spi
    path: /dev/spidev0.0
  spi-2:
    interface: spi
    path: /dev/spidev0.1
  spi-3:
    interface: spi
    path: /dev/spidev33566.0
  spi-unclean:
    interface: spi
    path: /dev/./spidev33567.0
`, nil)
	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	s.slotOs1Info = info.Slots["spi-1"]
	s.slotOs1 = interfaces.NewConnectedSlot(s.slotOs1Info, appSet, nil, nil)
	s.slotOs2Info = info.Slots["spi-2"]
	s.slotOs2 = interfaces.NewConnectedSlot(s.slotOs2Info, appSet, nil, nil)
	s.slotOs3Info = info.Slots["spi-3"]
	s.slotOs3 = interfaces.NewConnectedSlot(s.slotOs3Info, appSet, nil, nil)
	s.slotOsCleanedInfo = info.Slots["spi-unclean"]
	s.slotOsCleaned = interfaces.NewConnectedSlot(s.slotOsCleanedInfo, appSet, nil, nil)

	info = snaptest.MockInfo(c, `
name: gadget
version: 0
type: gadget
slots:
  spi-1:
    interface: spi
    path: /dev/spidev0.0
  spi-2:
    interface: spi
    path: /dev/spidev0.1
  spi-3:
    interface: spi
    path: /dev/spidev33566.0
  bad-spi-1:
    interface: spi
    path: /dev/spev0.0
  bad-spi-2:
    interface: spi
    path: /dev/sidv0.0
  bad-spi-3:
    interface: spi
    path: /dev/slpiv0.3
  bad-spi-4:
    interface: spi
    path: /dev/sdev-00
  bad-spi-5:
    interface: spi
    path: /dev/spi-foo
  bad-spi-6:
    interface: spi
`, nil)

	appSet, err = interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	s.slotGadget1Info = info.Slots["spi-1"]
	s.slotGadget1 = interfaces.NewConnectedSlot(s.slotGadget1Info, appSet, nil, nil)
	s.slotGadget2Info = info.Slots["spi-2"]
	s.slotGadget2 = interfaces.NewConnectedSlot(s.slotGadget2Info, appSet, nil, nil)
	s.slotGadget3Info = info.Slots["spi-3"]
	s.slotGadget3 = interfaces.NewConnectedSlot(s.slotGadget3Info, appSet, nil, nil)
	s.slotGadgetBad1Info = info.Slots["bad-spi-1"]
	s.slotGadgetBad1 = interfaces.NewConnectedSlot(s.slotGadgetBad1Info, appSet, nil, nil)
	s.slotGadgetBad2Info = info.Slots["bad-spi-2"]
	s.slotGadgetBad2 = interfaces.NewConnectedSlot(s.slotGadgetBad2Info, appSet, nil, nil)
	s.slotGadgetBad3Info = info.Slots["bad-spi-3"]
	s.slotGadgetBad3 = interfaces.NewConnectedSlot(s.slotGadgetBad3Info, appSet, nil, nil)
	s.slotGadgetBad4Info = info.Slots["bad-spi-4"]
	s.slotGadgetBad4 = interfaces.NewConnectedSlot(s.slotGadgetBad4Info, appSet, nil, nil)
	s.slotGadgetBad5Info = info.Slots["bad-spi-5"]
	s.slotGadgetBad5 = interfaces.NewConnectedSlot(s.slotGadgetBad5Info, appSet, nil, nil)
	s.slotGadgetBad6Info = info.Slots["bad-spi-6"]
	s.slotGadgetBad6 = interfaces.NewConnectedSlot(s.slotGadgetBad6Info, appSet, nil, nil)

	info = snaptest.MockInfo(c, `
name: consumer
version: 0
plugs:
  spi-1:
    interface: spi
    path: /dev/spidev.0.0
  spi-2:
    interface: spi
    path: /dev/spidev0.1
  spi-3:
    interface: spi
    path: /dev/spidev33566.0
apps:
  app:
    command: foo
    plugs: [spi-1]
`, nil)
	appSet, err = interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	s.plug1Info = info.Plugs["spi-1"]
	s.plug1 = interfaces.NewConnectedPlug(s.plug1Info, appSet, nil, nil)
	s.plug2Info = info.Plugs["spi-2"]
	s.plug2 = interfaces.NewConnectedPlug(s.plug2Info, appSet, nil, nil)
	s.plug3Info = info.Plugs["spi-3"]
	s.plug3 = interfaces.NewConnectedPlug(s.plug3Info, appSet, nil, nil)
}

func (s *spiInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "spi")
}

func (s *spiInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotOs1Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotOs2Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotOs3Info), IsNil)
	// Verify historically filepath.Clean()d paths are still valid
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotOsCleanedInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotGadget1Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotGadget2Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotGadget3Info), IsNil)
	err := interfaces.BeforePrepareSlot(s.iface, s.slotGadgetBad1Info)
	c.Assert(err, ErrorMatches, `"/dev/spev0.0" is not a valid SPI device`)
	err = interfaces.BeforePrepareSlot(s.iface, s.slotGadgetBad2Info)
	c.Assert(err, ErrorMatches, `"/dev/sidv0.0" is not a valid SPI device`)
	err = interfaces.BeforePrepareSlot(s.iface, s.slotGadgetBad3Info)
	c.Assert(err, ErrorMatches, `"/dev/slpiv0.3" is not a valid SPI device`)
	err = interfaces.BeforePrepareSlot(s.iface, s.slotGadgetBad4Info)
	c.Assert(err, ErrorMatches, `"/dev/sdev-00" is not a valid SPI device`)
	err = interfaces.BeforePrepareSlot(s.iface, s.slotGadgetBad5Info)
	c.Assert(err, ErrorMatches, `"/dev/spi-foo" is not a valid SPI device`)
	err = interfaces.BeforePrepareSlot(s.iface, s.slotGadgetBad6Info)
	c.Assert(err, ErrorMatches, `slot "gadget:bad-spi-6" must have a path attribute`)
}

func (s *spiInterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(s.plug1.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug1, s.slotGadget1), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# spi
KERNEL=="spidev0.0", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *spiInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(s.plug1.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug1, s.slotGadget1), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, ""+
		"/dev/spidev0.0 rw,\n"+
		"/sys/devices/platform/**/**.spi/**/spidev0.0/** rw,  # Add any condensed parametric rules")
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug1, s.slotGadget2), IsNil)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, ""+
		"/dev/spidev0.0 rw,\n"+
		"/dev/spidev0.1 rw,\n"+
		"/sys/devices/platform/**/**.spi/**/spidev{0.0,0.1}/** rw,  # Add any condensed parametric rules")
}

func (s *spiInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, "allows access to specific spi controller")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "spi")
}

func (s *spiInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *spiInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
