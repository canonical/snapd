// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type HdmiCecInterfaceSuite struct {
	iface    interfaces.Interface
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&HdmiCecInterfaceSuite{
	iface: builtin.MustInterface("hdmi-cec"),
})

func (s *HdmiCecInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, `name: consumer
version: 0
apps:
 app:
  plugs: [hdmi-cec]
`, nil, "hdmi-cec")
	s.slot, s.slotInfo = MockConnectedSlot(c, `name: core
version: 0
type: os
slots:
 hdmi-cec:
`, nil, "hdmi-cec")
}

func (s *HdmiCecInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "hdmi-cec")
}

func (s *HdmiCecInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.Summary, Equals, "allows access to HDMI CEC devices")
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "hdmi-cec")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *HdmiCecInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	snippet := spec.SnippetForTag("snap.consumer.app")
	for _, rule := range []string{
		"/dev/cec[0-9]* rw,",
		"/dev/hdmicec rw,",
		"/dev/CEC rw,",
		"/dev/aocec rw,",
		"/dev/mxc_hdmi_cec rw,",
		"/dev/tegra_cec rw,",
		"/dev/vchiq rw,",
		"/sys/bus/cec/devices/** r,",
		"/sys/devices/**/cec[0-9]*/{,**} r,",
		"/sys/devices/platform/**/{subsystem,uevent} r,",
		"/sys/module/s5p_hdmi/parameters/source_phy_addr r,",
		"/sys/devices/platform/tegra_cec/cec_logical_addr_config rw,",
	} {
		c.Assert(snippet, testutil.Contains, rule)
	}
}

func (s *HdmiCecInterfaceSuite) TestUDevSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	snippets := strings.Join(spec.Snippets(), "\n")
	for _, rule := range []string{
		`SUBSYSTEM=="cec"`,
		`KERNEL=="hdmicec"`,
		`KERNEL=="CEC"`,
		`KERNEL=="aocec"`,
		`KERNEL=="mxc_hdmi_cec"`,
		`KERNEL=="tegra_cec"`,
	} {
		c.Assert(snippets, testutil.Contains, rule)
	}
}

func (s *HdmiCecInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
