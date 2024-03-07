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
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type SharedMemoryInterfaceSuite struct {
	testutil.BaseTest

	iface            interfaces.Interface
	slotInfo         *snap.SlotInfo
	slot             *interfaces.ConnectedSlot
	plugInfo         *snap.PlugInfo
	plug             *interfaces.ConnectedPlug
	wildcardPlugInfo *snap.PlugInfo
	wildcardPlug     *interfaces.ConnectedPlug
	wildcardSlotInfo *snap.SlotInfo
	wildcardSlot     *interfaces.ConnectedSlot
	privatePlugInfo  *snap.PlugInfo
	privatePlug      *interfaces.ConnectedPlug
	privateSlotInfo  *snap.SlotInfo
	privateSlot      *interfaces.ConnectedSlot
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
  private: false
 shmem-wildcard:
  interface: shared-memory
  shared-memory: foo-wildcard
  private: false
 shmem-private:
  interface: shared-memory
  private: true
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
  private: false
 shmem-wildcard:
  interface: shared-memory
  shared-memory: foo-wildcard
  write: [ bar* ]
  read: [ bar-ro* ]
  private: false
apps:
 app:
  slots: [shmem]
`

const sharedMemoryCoreYaml = `name: core
version: 0
type: os
slots:
 shared-memory:
  interface: shared-memory
apps:
 app:
`

func (s *SharedMemoryInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, sharedMemoryConsumerYaml, nil, "shmem")
	s.slot, s.slotInfo = MockConnectedSlot(c, sharedMemoryProviderYaml, nil, "shmem")

	s.wildcardPlug, s.wildcardPlugInfo = MockConnectedPlug(c, sharedMemoryConsumerYaml, nil, "shmem-wildcard")
	s.wildcardSlot, s.wildcardSlotInfo = MockConnectedSlot(c, sharedMemoryProviderYaml, nil, "shmem-wildcard")

	s.privatePlug, s.privatePlugInfo = MockConnectedPlug(c, sharedMemoryConsumerYaml, nil, "shmem-private")
	s.privateSlot, s.privateSlotInfo = MockConnectedSlot(c, sharedMemoryCoreYaml, nil, "shared-memory")
}

func (s *SharedMemoryInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "shared-memory")
}

func (s *SharedMemoryInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)

	c.Check(interfaces.BeforePreparePlug(s.iface, s.wildcardPlugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.wildcardPlug), IsNil)
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
		{
			"private: hello",
			`shared-memory "private" attribute must be a bool, not hello`,
		},
		{
			"private: true\n  shared-memory: foo",
			`shared-memory "shared-memory" attribute must not be set together with "private: true"`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(sharedMemoryYaml, testData.plugYaml)
		_, plug := MockConnectedPlug(c, snapYaml, nil, "shmem")
		err := interfaces.BeforePreparePlug(s.iface, plug)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("yaml: %s", testData.plugYaml))
	}
}

func (s *SharedMemoryInterfaceSuite) TestPlugPrivateAttribute(c *C) {
	const snapYaml = `name: consumer
version: 0
plugs:
 shmem:
  interface: shared-memory
  private: true
apps:
 app:
  plugs: [shmem]
`
	_, plug := MockConnectedPlug(c, snapYaml, nil, "shmem")
	err := interfaces.BeforePreparePlug(s.iface, plug)
	c.Assert(err, IsNil)
	c.Check(plug.Attrs["private"], Equals, true)
	c.Check(plug.Attrs["shared-memory"], Equals, nil)
}

func (s *SharedMemoryInterfaceSuite) TestPlugPrivateConflictsWithNonPrivate(c *C) {
	const snapYaml1 = `name: consumer
version: 0
plugs:
  shmem:
    interface: shared-memory
  shmem-private:
    interface: shared-memory
    private: true
`
	_, plug := MockConnectedPlug(c, snapYaml1, nil, "shmem-private")
	err := interfaces.BeforePreparePlug(s.iface, plug)
	c.Check(err, ErrorMatches, `shared-memory plug with "private: true" set cannot be used with other shared-memory plugs`)

	const snapYaml2 = `name: consumer
version: 0
plugs:
  shmem-private:
    interface: shared-memory
    private: true
slots:
  shmem:
    interface: shared-memory
`
	_, plug = MockConnectedPlug(c, snapYaml2, nil, "shmem-private")
	err = interfaces.BeforePreparePlug(s.iface, plug)
	c.Check(err, ErrorMatches, `shared-memory plug with \"private: true\" set cannot be used with shared-memory slots`)
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
		c.Check(plug.Attrs["private"], Equals, false,
			Commentf(`yaml: %q`, testData.plugYaml))
		c.Check(plug.Attrs["shared-memory"], Equals, testData.expectedName,
			Commentf(`yaml: %q`, testData.plugYaml))
	}
}

func (s *SharedMemoryInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.wildcardSlotInfo), IsNil)
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
			`write: [mem**]`,
			`shared-memory interface path is invalid: "mem\*\*" contains \*\* which is unsupported.*`,
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
	c.Check(si.ImplicitOnCore, Equals, true)
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, `allows two snaps to use predefined shared memory objects`)
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "shared-memory")
}

func (s *SharedMemoryInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	plugSnippet := spec.SnippetForTag("snap.consumer.app")

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	c.Check(plugSnippet, testutil.Contains, `"/{dev,run}/shm/bar" mrwlk,`)
	c.Check(plugSnippet, testutil.Contains, `"/{dev,run}/shm/bar-ro" r,`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})

	slotSnippet := spec.SnippetForTag("snap.provider.app")

	// Slot has read-write permissions to all paths
	c.Check(slotSnippet, testutil.Contains, `"/{dev,run}/shm/bar" mrwlk,`)
	c.Check(slotSnippet, testutil.Contains, `"/{dev,run}/shm/bar-ro" mrwlk,`)

	wildcardSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.wildcardPlug.Snap(), nil))
	c.Assert(wildcardSpec.AddConnectedPlug(s.iface, s.wildcardPlug, s.wildcardSlot), IsNil)
	wildcardPlugSnippet := wildcardSpec.SnippetForTag("snap.consumer.app")

	c.Assert(wildcardSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})

	c.Check(wildcardPlugSnippet, testutil.Contains, `"/{dev,run}/shm/bar*" mrwlk,`)
	c.Check(wildcardPlugSnippet, testutil.Contains, `"/{dev,run}/shm/bar-ro*" r,`)

	wildcardSpec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.wildcardSlot.Snap(), nil))
	c.Assert(wildcardSpec.AddConnectedSlot(s.iface, s.wildcardPlug, s.wildcardSlot), IsNil)

	c.Assert(wildcardSpec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})

	wildcardSlotSnippet := wildcardSpec.SnippetForTag("snap.provider.app")

	// Slot has read-write permissions to all paths
	c.Check(wildcardSlotSnippet, testutil.Contains, `"/{dev,run}/shm/bar*" mrwlk,`)
	c.Check(wildcardSlotSnippet, testutil.Contains, `"/{dev,run}/shm/bar-ro*" mrwlk,`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.privatePlug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.privatePlug, s.privateSlot), IsNil)
	privatePlugSnippet := spec.SnippetForTag("snap.consumer.app")
	privateUpdateNS := spec.UpdateNS()

	c.Check(privatePlugSnippet, testutil.Contains, `"/dev/shm/**" mrwlkix`)
	c.Check(strings.Join(privateUpdateNS, ""), Equals, `  # Private /dev/shm
  /dev/ r,
  /dev/shm/{,**} rw,
  mount options=(bind, rw) /dev/shm/snap.consumer/ -> /dev/shm/,
  umount /dev/shm/,`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.privateSlot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.privatePlug, s.privateSlot), IsNil)
	privateSlotSnippet := spec.SnippetForTag("snap.core.app")

	c.Check(privateSlotSnippet, Equals, "")
}

func (s *SharedMemoryInterfaceSuite) TestMountSpec(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	defer dirs.SetRootDir("/")
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/dev/shm"), 0777), IsNil)

	// No mount entries for non-private shared-memory plugs
	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.MountEntries(), HasLen, 0)

	spec = &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.privatePlug, s.privateSlot), IsNil)
	mounts := []osutil.MountEntry{
		{
			Name:    filepath.Join(tmpdir, "/dev/shm/snap.consumer"),
			Dir:     "/dev/shm",
			Options: []string{"bind", "rw"},
		},
	}
	c.Check(spec.MountEntries(), DeepEquals, mounts)

	// Cannot set up mount entries if /dev/shm is a symlink
	c.Assert(os.Remove(filepath.Join(tmpdir, "/dev/shm")), IsNil)
	c.Assert(os.Symlink("/run/shm", filepath.Join(tmpdir, "/dev/shm")), IsNil)
	spec = &mount.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.privatePlug, s.privateSlot)
	c.Check(err, ErrorMatches, `shared-memory plug with "private: true" cannot be connected if ".*/dev/shm" is a symlink`)
}

func (s *SharedMemoryInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *SharedMemoryInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *SharedMemoryInterfaceSuite) TestNoErrorOnMissingPrivate(c *C) {
	consumerYaml := `name: consumer
version: 0
plugs:
 shmem-missing:
  interface: shared-memory
  shared-memory: foo
`
	plug, _ := MockConnectedPlug(c, consumerYaml, nil, "shmem-missing")

	spec := &mount.Specification{}
	err := spec.AddConnectedPlug(s.iface, plug, nil)
	c.Assert(err, IsNil)
}

func (s *SharedMemoryInterfaceSuite) TestErrorOnBadPlug(c *C) {
	consumerYaml := `name: consumer
version: 0
plugs:
 shmem-missing:
  interface: shared-memory
  shared-memory: foo
  private: xxx
`
	plug, _ := MockConnectedPlug(c, consumerYaml, nil, "shmem-missing")

	spec := &mount.Specification{}
	err := spec.AddConnectedPlug(s.iface, plug, nil)
	c.Assert(err, ErrorMatches, `snap "consumer" has interface "shared-memory" with invalid value type string for "private" attribute: \*bool`)
}
