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

	"github.com/snapcore/snapd/dirs"
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
  read-devices:
    - /dev/js*
  files:
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
			"custom-device: [one two]",
			`custom-device "custom-device" attribute must be a string, not \[one two\]`,
		},
		{
			"devices: 12",
			`snap "provider" has interface "custom-device" with invalid value type int64 for "devices" attribute.*`,
		},
		{
			"read-devices: [/dev/zero, 2]",
			`snap "provider" has interface "custom-device" with invalid value type \[\]interface {} for "read-devices" attribute.*`,
		},
		{
			"devices: [/dev/foo**]",
			`custom-device "devices" path contains invalid glob pattern "\*\*"`,
		},
		{
			"devices: [/dev/@foo]",
			`custom-device "devices" path must start with / and cannot contain special characters.*`,
		},
		{
			"devices: [/dev/foo|bar]",
			`custom-device "devices" path must start with /dev/ and cannot contain special characters.*`,
		},
		{
			`devices: [/dev/foo"bar]`,
			`custom-device "devices" path must start with /dev/ and cannot contain special characters.*`,
		},
		{
			`devices: ["/dev/{foo}bar"]`,
			`custom-device "devices" path must start with /dev/ and cannot contain special characters.*`,
		},
		{
			"read-devices: [/dev/foo\\bar]",
			`custom-device "read-devices" path must start with /dev/ and cannot contain special characters.*`,
		},
		{
			"devices: [/run/foo]",
			`custom-device "devices" path must start with /dev/ and cannot contain special characters.*`,
		},
		{
			"devices: [/dev/../etc/passwd]",
			`custom-device "devices" path is not clean.*`,
		},
		{
			`read-devices: ["/dev/unmatched[bracket"]`,
			`custom-device "read-devices" path cannot be used: missing closing bracket ']'.*`,
		},
		{
			"devices: [/dev/foo]\n  read-devices: [/dev/foo]",
			`cannot specify path "/dev/foo" both in "devices" and "read-devices" attributes`,
		},
		{
			`files: {read: [23]}`,
			`snap "provider" has interface "custom-device" with invalid value type map\[string\]interface {} for "files" attribute.*`,
		},
		{
			`files: {write: [23]}`,
			`snap "provider" has interface "custom-device" with invalid value type map\[string\]interface {} for "files" attribute.*`,
		},
		{
			`files: {foo: [ /etc/foo ]}`,
			`cannot specify \"foo\" in \"files\" section, only \"read\" and \"write\" allowed`,
		},
		{
			`files: {read: [etc]}`,
			`custom-device "read" path must start with / and cannot contain special characters.*`,
		},
		{
			`files: {write: [one, 2]}`,
			`snap "provider" has interface "custom-device" with invalid value type map\[string\]interface {} for "files" attribute.*`,
		},
		{
			`files: {read: [/etc/foo], write: [one, 2]}`,
			`snap "provider" has interface "custom-device" with invalid value type map\[string\]interface {} for "files" attribute.*`,
		},
		{
			`files: {read: [222], write: [/etc/one]}`,
			`snap "provider" has interface "custom-device" with invalid value type map\[string\]interface {} for "files" attribute.*`,
		},
		{
			`files: {read: ["/dev/\"quote"]}`,
			`custom-device "read" path must start with / and cannot contain special characters.*`,
		},
		{
			`files: {write: ["/dev/\"quote"]}`,
			`custom-device "write" path must start with / and cannot contain special characters.*`,
		},
		{
			`files: {remove: ["/just/a/file"]}`,
			`cannot specify "remove" in "files" section, only "read" and "write" allowed`,
		},
		{
			`udev-tagging: []`,
			`cannot use custom-device slot without any files or devices`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging: true",
			`snap "provider" has interface "custom-device" with invalid value type bool for "udev-tagging" attribute.*`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - foo: bar}",
			`custom-device "udev-tagging" invalid "foo" tag: unknown tag`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - subsystem: 12",
			`custom-device "udev-tagging" invalid "subsystem" tag: value "12" is not a string`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - subsystem: deal{which,this}",
			`custom-device "udev-tagging" invalid "subsystem" tag: value "deal{which,this}" contains invalid characters`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - subsystem: bar",
			`custom-device udev tagging rule missing mandatory "kernel" key`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - kernel: bar",
			`custom-device "udev-tagging" invalid "kernel" tag: "bar" does not match any specified device`,
		},
		{
			"devices: [/dev/subdir/foo]\n  udev-tagging:\n    - kernel: foo\n      subsystem: 12",
			`custom-device "udev-tagging" invalid "subsystem" tag: value "12" is not a string`,
		},
		{
			"devices: [/dev/dir1/foo, /dev/dir2/foo]\n  udev-tagging:\n    - kernel: foo",
			`custom-device "udev-tagging" invalid "kernel" tag: "foo" matches more than one specified device: \["/dev/dir1/foo" "/dev/dir2/foo"\]`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - attributes: foo",
			`custom-device "udev-tagging" invalid "attributes" tag: value "foo" is not a map`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - attributes: {key\": noquotes}",
			`custom-device "udev-tagging" invalid "attributes" tag: key "key"" contains invalid characters`,
		},
		{
			"devices: [/dev/null]\n  udev-tagging:\n    - environment: {key: \"va{ue}\"}",
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

func (s *CustomDeviceInterfaceSuite) TestSlotNameAttribute(c *C) {
	var slotYamlTemplate = `name: provider
version: 0
slots:
 hwdev:
  interface: custom-device
  devices: [ /dev/null ]
  %s
`

	data := []struct {
		slotYaml     string
		expectedName string
	}{
		{
			"",      // missing "custom-device" attribute
			"hwdev", // use the name of the slot
		},
		{
			"custom-device: shmemFoo",
			"shmemFoo",
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(slotYamlTemplate, testData.slotYaml)
		_, slot := MockConnectedSlot(c, snapYaml, nil, "hwdev")
		err := interfaces.BeforePrepareSlot(s.iface, slot)
		c.Assert(err, IsNil)
		c.Check(slot.Attrs["custom-device"], Equals, testData.expectedName,
			Commentf(`yaml: %q`, testData.slotYaml))
	}
}

func (s *CustomDeviceInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, false)
	c.Check(si.Summary, Equals, `provides access to custom devices specified via the gadget snap`)
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "custom-device")
}

func (s *CustomDeviceInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)

	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	plugSnippet := spec.SnippetForTag("snap.consumer.app")

	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	slotSnippet := spec.SnippetForTag("snap.provider.app")

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	c.Check(plugSnippet, testutil.Contains, `"/dev/input/event[0-9]" rwk,`)
	c.Check(plugSnippet, testutil.Contains, `"/dev/input/mice" rwk,`)
	c.Check(plugSnippet, testutil.Contains, `"/dev/js*" r,`)
	c.Check(plugSnippet, testutil.Contains, `"/bar" rw,`)
	c.Check(plugSnippet, testutil.Contains, `"/dev/input/by-id/*" r,`)
	c.Check(slotSnippet, HasLen, 0)
}

func (s *CustomDeviceInterfaceSuite) TestUDevSpec(c *C) {
	const slotYamlTemplate = `name: provider
version: 0
slots:
 hwdev:
  interface: custom-device
  custom-device: foo
  devices:
    - /dev/input/event[0-9]
    - /dev/input/mice
    - /dev/dma_heap/qcom,qseecom
    - /dev/bar
    - /dev/foo/bar
    - /dev/dir1/baz
    - /dev/dir2/baz
  read-devices:
    - /dev/js*
  %s
apps:
 app:
  slots: [hwdev]
`

	data := []struct {
		slotYaml      string
		expectedRules []map[string]string
	}{
		{
			"", // missing "udev-tagging" attribute
			[]map[string]string{
				// all rules are automatically-generated
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`},
				{`KERNEL`: `"mice"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		{
			"udev-tagging:\n   - kernel: input/mice\n     subsystem: input",
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`, `SUBSYSTEM`: `"input"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		{
			`udev-tagging:
   - kernel: input/mice
     subsystem: input
   - kernel: js*
     attributes:
      attr1: one
      attr2: two`,
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`, `SUBSYSTEM`: `"input"`},
				{`KERNEL`: `"js*"`, `ATTR{attr1}`: `"one"`, `ATTR{attr2}`: `"two"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		{
			`udev-tagging:
   - kernel: input/mice
     attributes:
      wheel: "true"
   - kernel: input/event[0-9]
     subsystem: input
     environment:
      env1: first
      env2: second|other`,
			[]map[string]string{
				{
					`KERNEL`:    `"input/event[0-9]"`,
					`SUBSYSTEM`: `"input"`,
					`ENV{env1}`: `"first"`,
					`ENV{env2}`: `"second|other"`,
				},
				{`KERNEL`: `"input/mice"`, `ATTR{wheel}`: `"true"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		{
			"udev-tagging:\n   - kernel: dma_heap/qcom,qseecom",
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`},
				{`KERNEL`: `"mice"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		{
			"udev-tagging:\n   - kernel: qcom,qseecom",
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`},
				{`KERNEL`: `"mice"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		// if there happens to be a full device path which matches the
		// basename of another device path, don't override default rule
		// creation.
		{
			"udev-tagging:\n   - kernel: bar",
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`},
				{`KERNEL`: `"mice"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		{
			"udev-tagging:\n   - kernel: foo/bar",
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`},
				{`KERNEL`: `"mice"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		// if there happen to be two device paths with the same
		// basenames, create default rules for both full paths, and one
		// default rule with the shared basename.  If there is a rule
		// for one of the full paths, the other still gets both default
		// rules.  If there is are rules for both full paths, don't
		// generate the basename default rule.
		{
			"udev-tagging:\n   - kernel: dir1/baz",
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`},
				{`KERNEL`: `"mice"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
				{`KERNEL`: `"baz"`},
			},
		},
		{
			`udev-tagging:
   - kernel: dir1/baz
   - kernel: dir2/baz`,
			[]map[string]string{
				{`KERNEL`: `"input/event[0-9]"`},
				{`KERNEL`: `"event[0-9]"`},
				{`KERNEL`: `"input/mice"`},
				{`KERNEL`: `"mice"`},
				{`KERNEL`: `"js*"`},
				{`KERNEL`: `"dma_heap/qcom,qseecom"`},
				{`KERNEL`: `"qcom,qseecom"`},
				{`KERNEL`: `"bar"`},
				{`KERNEL`: `"foo/bar"`},
				{`KERNEL`: `"dir1/baz"`},
				{`KERNEL`: `"dir2/baz"`},
			},
		},
	}

	for i, testData := range data {
		testLabel := Commentf("yaml: %s", testData.slotYaml)
		appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
		c.Assert(err, IsNil)
		spec := udev.NewSpecification(appSet)
		snapYaml := fmt.Sprintf(slotYamlTemplate, testData.slotYaml)
		slot, _ := MockConnectedSlot(c, snapYaml, nil, "hwdev")
		c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
		snippets := spec.Snippets()

		// The first lines are for the tagging, the last one is for the
		// snap-device-helper
		rulesCount := len(testData.expectedRules)
		c.Assert(snippets, HasLen, rulesCount+1, Commentf("Error on test case index %d", i))

		// The following rule is not fixed since the order of the elements depend
		// on the map iteration order, which in golang is not deterministic.
		// Therefore, we decompose each rule into a map:
		var decomposedSnippets []map[string]string
		for _, snippet := range snippets[:rulesCount] {
			lines := strings.Split(snippet, "\n")
			c.Assert(lines, HasLen, 2, testLabel)

			// The first line is just a comment
			c.Check(lines[0], Matches, "^#.*", testLabel)

			// The second line contains the actual rule
			ruleTags := strings.Split(lines[1], ", ")
			// Verify that the last part is the tag assignment
			lastElement := len(ruleTags) - 1
			c.Check(ruleTags[lastElement], Equals, `TAG+="snap_consumer_app"`)
			decomposedTags := make(map[string]string)
			for _, ruleTag := range ruleTags[:lastElement] {
				tagMembers := strings.SplitN(ruleTag, "==", 2)
				c.Assert(tagMembers, HasLen, 2)
				decomposedTags[tagMembers[0]] = tagMembers[1]
			}
			decomposedSnippets = append(decomposedSnippets, decomposedTags)
		}
		c.Assert(decomposedSnippets, testutil.DeepUnsortedMatches, testData.expectedRules, testLabel)

		// The last line of the snippet is about snap-device-helper
		actionLine := snippets[rulesCount]
		c.Assert(actionLine, Matches,
			fmt.Sprintf(`^TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN\+="%s/snap-device-helper .*`, dirs.DistroLibExecDir),
			testLabel)
	}
}

func (s *CustomDeviceInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *CustomDeviceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
