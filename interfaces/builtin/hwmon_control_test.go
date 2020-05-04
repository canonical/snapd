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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type HardwareControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const hwcontrolMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [hwmon-control]
`

var _ = Suite(&HardwareControlInterfaceSuite{
	iface: builtin.MustInterface("hwmon-control"),
})

func (s *HardwareControlInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "hwmon-control",
		Interface: "hwmon-control",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, hwcontrolMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["hwmon-control"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *HardwareControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "hwmon-control")
}

func (s *HardwareControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *HardwareControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *HardwareControlInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	var snapYamlTemplate = `name: consumer
version: 0
plugs:
 hwmon:
  interface: hwmon-control
  %s
apps:
 app:
  plugs: [hwmon]
`
	data := []struct {
		plugYaml      string
		expectedError string
	}{
		{
			"channels: a string",
			`hwmon-control "channels" attribute must be a list of strings`,
		},
		{
			"channels: [yes, no]",
			`hwmon-control "channels" attribute must be a list of strings`,
		},
		{
			"channels: [fan, humidity, heart]",
			`hwmon-control: unsupported "channels" attribute "heart"`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(snapYamlTemplate, testData.plugYaml)
		info := snaptest.MockInfo(c, snapYaml, nil)
		plug := info.Plugs["hwmon"]
		err := interfaces.BeforePreparePlug(s.iface, plug)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("yaml: %s", testData.plugYaml))
	}
}

func (s *HardwareControlInterfaceSuite) TestAppArmorSpecAll(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})

	snippet := spec.SnippetForTag("snap.other.app2")

	// common accesses
	c.Check(snippet, testutil.Contains, "/sys/class/hwmon/ r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/ r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/beep_enable r,\n")

	// current
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/curr[1-9]*_max rw,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/curr[1-9]*_lcrit_alarm r,\n")

	// energy
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/energy[1-9]*_input r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/energy[1-9]*_enable rw,\n")

	// fan
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/fan[1-9]*_div rw,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/fan[1-9]*_alarm r,\n")

	// humidity
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/humidity[1-9]*_input r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/humidity[1-9]*_enable rw,\n")

	// intrusion
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/intrusion[0-9]*_alarm rw,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/intrusion[0-9]*_beep rw,\n")

	// power
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/power[1-9]*_average r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/power[1-9]*_cap rw,\n")

	// pwm
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/pwm[1-9]*_freq rw,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/temp[1-9]*_auto_point[1-9]*_temp rw,\n")

	// temperature
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/temp[1-9]*_crit rw,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/temp[1-9]*_reset_history w,\n")

	// voltage
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/in[0-9]*_enable rw,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/cpu[0-9]*_vid r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/vrm rw,\n")
}

func (s *HardwareControlInterfaceSuite) TestAppArmorSpecSome(c *C) {
	snapYaml := `name: consumer
version: 0
plugs:
 hwmon:
  interface: hwmon-control
  channels: [fan, power]
apps:
 app:
  plugs: [hwmon]
`
	plug, _ := MockConnectedPlug(c, snapYaml, nil, "hwmon")
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	snippet := spec.SnippetForTag("snap.consumer.app")

	// common accesses
	c.Check(snippet, testutil.Contains, "/sys/class/hwmon/ r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/ r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/beep_enable r,\n")

	// current
	c.Check(snippet, Not(testutil.Contains), "/sys/devices/**/hwmon[0-9]*/curr[1-9]*_max rw,\n")

	// energy
	c.Check(snippet, Not(testutil.Contains), "/sys/devices/**/hwmon[0-9]*/energy[1-9]*_input r,\n")

	// fan
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/fan[1-9]*_div rw,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/fan[1-9]*_alarm r,\n")

	// humidity
	c.Check(snippet, Not(testutil.Contains), "/sys/devices/**/hwmon[0-9]*/humidity[1-9]*_input r,\n")

	// intrusion
	c.Check(snippet, Not(testutil.Contains), "/sys/devices/**/hwmon[0-9]*/intrusion[0-9]*_alarm rw,\n")

	// power
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/power[1-9]*_average r,\n")
	c.Check(snippet, testutil.Contains, "/sys/devices/**/hwmon[0-9]*/power[1-9]*_cap rw,\n")

	// pwm
	c.Check(snippet, Not(testutil.Contains), "/sys/devices/**/hwmon[0-9]*/pwm[1-9]*_freq rw,\n")

	// temperature
	c.Check(snippet, Not(testutil.Contains), "/sys/devices/**/hwmon[0-9]*/temp[1-9]*_crit rw,\n")

	// voltage
	c.Check(snippet, Not(testutil.Contains), "/sys/devices/**/hwmon[0-9]*/in[0-9]*_enable rw,\n")
}

func (s *HardwareControlInterfaceSuite) TestAppArmorSpecFail(c *C) {
	// Any error in the YAML will already be detected in the
	// BeforePreparePlug() phase, but since our AppArmorConnectedPlug() method
	// has a check for this, let's test it.
	snapYaml := `name: consumer
version: 0
plugs:
 hwmon:
  interface: hwmon-control
  channels: not a list
apps:
 app:
  plugs: [hwmon]
`
	plug, _ := MockConnectedPlug(c, snapYaml, nil, "hwmon")
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, plug, s.slot)
	c.Check(err, ErrorMatches, `hwmon-control "channels" attribute must be a list of strings`)
}

func (s *HardwareControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
