// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"github.com/snapcore/snapd/testutil"
)

type SharedMemoryInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&SharedMemoryInterfaceSuite{
	iface: builtin.MustInterface("shared-memory"),
})

const sharedMemoryConsumerYaml = `name: consumer
version: 0
plugs:
 shmem:
  interface: shared-memory
  shared-memory: foo
apps:
 app:
  plugs: [shmem]
`

const sharedMemoryProviderYaml = `name: provider
version: 0
slots:
 shmem:
  interface: shared-memory
  shared-memory: foo
  write: [ bar ]
  read: [ bar-ro ]
apps:
 app:
  slots: [shmem]
`

func (s *SharedMemoryInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, sharedMemoryConsumerYaml, nil, "shmem")
	s.slot, s.slotInfo = MockConnectedSlot(c, sharedMemoryProviderYaml, nil, "shmem")
}

func (s *SharedMemoryInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "shared-memory")
}

func (s *SharedMemoryInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *SharedMemoryInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	var sharedMemoryYaml = `name: consumer
version: 0
plugs:
 shmem:
  interface: shared-memory
  %s
apps:
 app:
  plugs: [shmem]
`
	data := []struct {
		plugYaml      string
		expectedError string
	}{
		{
			"shared-memory: [one two]",
			`shared-memory "shared-memory" attribute must be a string, not \[one two\]`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(sharedMemoryYaml, testData.plugYaml)
		_, plug := MockConnectedPlug(c, snapYaml, nil, "shmem")
		err := interfaces.BeforePreparePlug(s.iface, plug)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("yaml: %s", testData.plugYaml))
	}
}

func (s *SharedMemoryInterfaceSuite) TestPlugShmAttribute(c *C) {
	var plugYamlTemplate = `name: consumer
version: 0
plugs:
 shmem:
  interface: shared-memory
  %s
apps:
 app:
  plugs: [shmem]
`

	data := []struct {
		plugYaml     string
		expectedName string
	}{
		{
			"",      // missing "shared-memory" attribute
			"shmem", // use the name of the plug
		},
		{
			"shared-memory: shmemFoo",
			"shmemFoo",
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(plugYamlTemplate, testData.plugYaml)
		_, plug := MockConnectedPlug(c, snapYaml, nil, "shmem")
		err := interfaces.BeforePreparePlug(s.iface, plug)
		c.Assert(err, IsNil)
		c.Check(plug.Attrs["shared-memory"], Equals, testData.expectedName,
			Commentf(`yaml: %q`, testData.plugYaml))
	}
}

func (s *SharedMemoryInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *SharedMemoryInterfaceSuite) TestSanitizeSlotUnhappy(c *C) {
	var sharedMemoryYaml = `name: provider
version: 0
slots:
 shmem:
  interface: shared-memory
  %s
apps:
 app:
  slots: [shmem]
`
	data := []struct {
		slotYaml      string
		expectedError string
	}{
		{
			"shared-memory: 12",
			`shared-memory "shared-memory" attribute must be a string, not 12`,
		},
		{
			"", // missing "write" attribute
			`shared memory interface requires at least a valid "read" or "write" attribute`,
		},
		{
			"write: a string",
			`shared-memory "write" attribute must be a list of strings, not "a string"`,
		},
		{
			"read: [Mixed, 12, False, list]",
			`shared-memory "read" attribute must be a list of strings, not "\[Mixed 12 false list\]"`,
		},
		{
			`read: ["ok", "trailing-space "]`,
			`shared-memory interface path has leading or trailing spaces: "trailing-space "`,
		},
		{
			`write: [" leading-space"]`,
			`shared-memory interface path has leading or trailing spaces: " leading-space"`,
		},
		{
			`write: [""]`,
			`shared-memory interface path is empty`,
		},
		{
			`write: [mem/**]`,
			`shared-memory interface path is invalid: "mem/\*\*" contains a reserved apparmor char.*`,
		},
		{
			`read: [..]`,
			`shared-memory interface path is not clean: ".."`,
		},
		{
			`write: [/dev/shm/bar]`,
			`shared-memory interface path should not contain '/': "/dev/shm/bar"`,
		},
		{
			`write: [mem/../etc]`,
			`shared-memory interface path should not contain '/': "mem/../etc"`,
		},
		{
			"write: [valid]\n  read: [../invalid]",
			`shared-memory interface path should not contain '/': "../invalid"`,
		},
		{
			"read: [valid]\n  write: [../invalid]",
			`shared-memory interface path should not contain '/': "../invalid"`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(sharedMemoryYaml, testData.slotYaml)
		_, slot := MockConnectedSlot(c, snapYaml, nil, "shmem")
		err := interfaces.BeforePrepareSlot(s.iface, slot)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("yaml: %s", testData.slotYaml))
	}
}

func (s *SharedMemoryInterfaceSuite) TestSlotShmAttribute(c *C) {
	var slotYamlTemplate = `name: consumer
version: 0
slots:
 shmem:
  interface: shared-memory
  write: [foo]
  %s
apps:
 app:
  slots: [shmem]
`

	data := []struct {
		slotYaml     string
		expectedName string
	}{
		{
			"",      // missing "shared-memory" attribute
			"shmem", // use the name of the slot
		},
		{
			"shared-memory: shmemBar",
			"shmemBar",
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(slotYamlTemplate, testData.slotYaml)
		_, slot := MockConnectedSlot(c, snapYaml, nil, "shmem")
		err := interfaces.BeforePrepareSlot(s.iface, slot)
		c.Assert(err, IsNil)
		c.Check(slot.Attrs["shared-memory"], Equals, testData.expectedName,
			Commentf(`yaml: %q`, testData.slotYaml))
	}
}

func (s *SharedMemoryInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, false)
	c.Check(si.Summary, Equals, `allows two snaps to use predefined shared memory objects`)
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "shared-memory")
}

func (s *SharedMemoryInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}

	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	plugSnippet := spec.SnippetForTag("snap.consumer.app")

	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	slotSnippet := spec.SnippetForTag("snap.provider.app")

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.provider.app"})

	c.Check(plugSnippet, testutil.Contains, `"/{dev,run}/shm/bar" rwk,`)
	c.Check(plugSnippet, testutil.Contains, `"/{dev,run}/shm/bar-ro" r,`)

	// Slot has read-write permissions to all paths
	c.Check(slotSnippet, testutil.Contains, `"/{dev,run}/shm/bar" rwk,`)
	c.Check(slotSnippet, testutil.Contains, `"/{dev,run}/shm/bar-ro" rwk,`)
}

func (s *SharedMemoryInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *SharedMemoryInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
