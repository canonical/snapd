// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type CustomDeviceInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&CustomDeviceInterfaceSuite{
	iface: builtin.MustInterface("custom-device"),
})

const customDeviceConsumerYaml = `name: consumer
version: 0
plugs:
 hwdev:
  interface: custom-device
  custom-device: foo
apps:
 app:
  plugs: [hwdev]
`

const customDeviceProviderYaml = `name: provider
version: 0
slots:
 hwdev:
  interface: custom-device
  custom-device: foo
  devices:
    - /dev/input/event[0-9]
    - /dev/input/mice
  write: [ /bar ]
  read:
    - /dev/input/by-id/*
  udev-tagging:
    - kernel: input/mice
      subsystem: input
      attributes:
        attr1: one
        attr2: two
      environment:
        env1: first
        env2: second|other
apps:
 app:
  slots: [hwdev]
`

func (s *CustomDeviceInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, customDeviceConsumerYaml, nil, "hwdev")
	s.slot, s.slotInfo = MockConnectedSlot(c, customDeviceProviderYaml, nil, "hwdev")
}

func (s *CustomDeviceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "custom-device")
}

func (s *CustomDeviceInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *CustomDeviceInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	var customDeviceYaml = `name: consumer
version: 0
plugs:
 hwdev:
  interface: custom-device
  %s
apps:
 app:
  plugs: [hwdev]
`
	data := []struct {
		plugYaml      string
		expectedError string
	}{
		{
			"custom-device: [one two]",
			`custom-device "custom-device" attribute must be a string, not \[one two\]`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(customDeviceYaml, testData.plugYaml)
		_, plug := MockConnectedPlug(c, snapYaml, nil, "hwdev")
		err := interfaces.BeforePreparePlug(s.iface, plug)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("yaml: %s", testData.plugYaml))
	}
}

func (s *CustomDeviceInterfaceSuite) TestPlugNameAttribute(c *C) {
	var plugYamlTemplate = `name: consumer
version: 0
plugs:
 hwdev:
  interface: custom-device
  %s
apps:
 app:
  plugs: [hwdev]
`

	data := []struct {
		plugYaml     string
		expectedName string
	}{
		{
			"",      // missing "custom-device" attribute
			"hwdev", // use the name of the plug
		},
		{
			"custom-device: shmemFoo",
			"shmemFoo",
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(plugYamlTemplate, testData.plugYaml)
		_, plug := MockConnectedPlug(c, snapYaml, nil, "hwdev")
		err := interfaces.BeforePreparePlug(s.iface, plug)
		c.Assert(err, IsNil)
		c.Check(plug.Attrs["custom-device"], Equals, testData.expectedName,
			Commentf(`yaml: %q`, testData.plugYaml))
	}
}

func (s *CustomDeviceInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *CustomDeviceInterfaceSuite) TestSanitizeSlotUnhappy(c *C) {
	var customDeviceYaml = `name: provider
version: 0
slots:
 hwdev:
  interface: custom-device
  %s
apps:
 app:
  slots: [hwdev]
`
	data := []struct {
		slotYaml      string
		expectedError string
	}{
		{
			"devices: 12",
			`snap "provider" has interface "custom-device" with invalid value type int64 for "devices" attribute.*`,
		},
		{
			"devices: [/dev/zero, 2]",
			`snap "provider" has interface "custom-device" with invalid value type \[\]interface {} for "devices" attribute.*`,
		},
		{
			"devices: [/dev/@foo]",
			`custom-device path must start with / and cannot contain special characters.*`,
		},
		{
			"devices: [/run/foo]",
			`custom-device path must start with /dev/ and cannot contain special characters.*`,
		},
		{
			"devices: [/dev/../etc/passwd]",
			`custom-device path is not clean.*`,
		},
		{
			`devices: ["/dev/unmatched[bracket"]`,
			`custom-device path cannot be used: missing closing bracket ']'.*`,
		},
		{
			`read: [23]`,
			`snap "provider" has interface "custom-device" with invalid value type \[\]interface {} for "read" attribute.*`,
		},
		{
			`read: [etc]`,
			`custom-device path must start with / and cannot contain special characters.*`,
		},
		{
			`write: [one, 2]`,
			`snap "provider" has interface "custom-device" with invalid value type \[\]interface {} for "write" attribute.*`,
		},
		{
			`read: ["/dev/\"quote"]`,
			`custom-device path must start with / and cannot contain special characters.*`,
		},
		{
			`udev-tagging: why not`,
			`snap "provider" has interface "custom-device" with invalid value type string for "udev-tagging" attribute.*`,
		},
		{
			"udev-tagging:\n    - foo: bar}",
			`custom-device "udev-tagging" invalid "foo" tag: unknown key`,
		},
		{
			"udev-tagging:\n    - subsystem: 12",
			`custom-device "udev-tagging" invalid "subsystem" tag: value "12" is not a string`,
		},
		{
			"udev-tagging:\n    - subsystem: deal{which,this}",
			`custom-device "udev-tagging" invalid "subsystem" tag: value "deal{which,this}" contains invalid characters`,
		},
		{
			"udev-tagging:\n    - subsystem: bar",
			`custom-device udev tagging rule missing mandatory "kernel" key`,
		},
		{
			"udev-tagging:\n    - kernel: bar",
			`custom-device "udev-tagging" invalid "kernel" tag: "bar" does not match a specified device`,
		},
		{
			"udev-tagging:\n    - attributes: foo",
			`custom-device "udev-tagging" invalid "attributes" tag: value "foo" is not a map`,
		},
		{
			"udev-tagging:\n    - attributes: {key\": noquotes}",
			`custom-device "udev-tagging" invalid "attributes" tag: key "key"" contains invalid characters`,
		},
		{
			"udev-tagging:\n    - environment: {key: \"va{ue}\"}",
			`custom-device "udev-tagging" invalid "environment" tag: value "va{ue}" contains invalid characters`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(customDeviceYaml, testData.slotYaml)
		_, slot := MockConnectedSlot(c, snapYaml, nil, "hwdev")
		err := interfaces.BeforePrepareSlot(s.iface, slot)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("yaml: %s", testData.slotYaml))
	}
}

func (s *CustomDeviceInterfaceSuite) TestSlotAttribute(c *C) {
	snapYaml := `name: consumer
version: 0
slots:
 hwdev:
  interface: custom-device
`
	_, slot := MockConnectedSlot(c, snapYaml, nil, "hwdev")
	err := interfaces.BeforePrepareSlot(s.iface, slot)
	c.Assert(err, IsNil)
	c.Check(slot.Attrs["custom-device"], Equals, "hwdev")
}

func (s *CustomDeviceInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, false)
	c.Check(si.Summary, Equals, `provides access to specific devices via the gadget snap`)
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "custom-device")
}

func (s *CustomDeviceInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}

	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	plugSnippet := spec.SnippetForTag("snap.consumer.app")

	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	slotSnippet := spec.SnippetForTag("snap.provider.app")

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	c.Check(plugSnippet, testutil.Contains, `"/dev/input/event[0-9]" rw,`)
	c.Check(plugSnippet, testutil.Contains, `"/dev/input/mice" rw,`)
	c.Check(plugSnippet, testutil.Contains, `"/bar" rw,`)
	c.Check(plugSnippet, testutil.Contains, `"/dev/input/by-id/*" r,`)
	c.Check(slotSnippet, HasLen, 0)
}

func (s *CustomDeviceInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}

	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	snippets := spec.Snippets()

	c.Assert(snippets, testutil.Contains, `KERNEL=="input/event[0-9]"`)
	// The following rule is not fixed since the order of the elements depend
	// on the map iteration order, which in golang is not deterministic.
	// Therefore, we decompose each rule into a map:
	var decomposedSnippets []map[string]string
	for _, snippet := range snippets {
		ruleTags := strings.Split(snippet, ", ")
		decomposedTags := make(map[string]string)
		for _, ruleTag := range ruleTags {
			tagMembers := strings.SplitN(ruleTag, "==", 2)
			c.Assert(tagMembers, HasLen, 2)
			decomposedTags[tagMembers[0]] = tagMembers[1]
		}
		decomposedSnippets = append(decomposedSnippets, decomposedTags)
	}
	c.Assert(decomposedSnippets, testutil.DeepUnsortedMatches, []map[string]string{
		{`KERNEL`: `"input/event[0-9]"`},
		{
			`KERNEL`:      `"input/mice"`,
			`SUBSYSTEM`:   `"input"`,
			`ENV{env1}`:   `"first"`,
			`ENV{env2}`:   `"second|other"`,
			`ATTR{attr1}`: `"one"`,
			`ATTR{attr2}`: `"two"`,
		},
	})
}

func (s *CustomDeviceInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *CustomDeviceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
