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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type MountControlInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&MountControlInterfaceSuite{
	iface: builtin.MustInterface("mount-control"),
})

const mountControlConsumerYaml = `name: consumer
version: 0
plugs:
 mntctl:
  interface: mount-control
  mount:
  - what: /dev/sd*
    where: /media/**
    type: [ext2, ext3, ext4]
    options: [rw, sync]
  - what: /usr/**
    where: $SNAP_COMMON/**
    options: [bind]
  - what: /dev/sda{0,1}
    where: $SNAP_COMMON/**
    options: [ro]
  - what: /dev/sda[0-1]
    where: $SNAP_COMMON/{foo,other,**}
    options: [sync]
apps:
 app:
  plugs: [mntctl]
`

const mountControlCoreYaml = `name: core
version: 0
type: os
slots:
  mount-control:
`

func (s *MountControlInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, mountControlConsumerYaml, nil, "mntctl")
	s.slot, s.slotInfo = MockConnectedSlot(c, mountControlCoreYaml, nil, "mount-control")

	s.AddCleanup(systemd.MockSystemdVersion(210, nil))
}

func (s *MountControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "mount-control")
}

func (s *MountControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *MountControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *MountControlInterfaceSuite) TestSanitizePlugOldSystemd(c *C) {
	restore := systemd.MockSystemdVersion(208, nil)
	defer restore()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, ErrorMatches, `systemd version 208 is too old \(expected at least 209\)`)
}

func (s *MountControlInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	var mountControlYaml = `name: consumer
version: 0
plugs:
 mntctl:
  interface: mount-control
  %s
apps:
 app:
  plugs: [mntctl]
`
	data := []struct {
		plugYaml      string
		expectedError string
	}{
		{
			"", // missing "mount" attribute
			`mount-control "mount" attribute must be a list of dictionaries`,
		},
		{
			"mount: a string",
			`mount-control "mount" attribute must be a list of dictionaries`,
		},
		{
			"mount: [this, is, a, list]",
			`mount-control "mount" attribute must be a list of dictionaries`,
		},
		{
			"mount:\n  - what: [this, is, a, list]\n    where: /media/**",
			`mount-control "what" must be a string`,
		},
		{
			"mount:\n  - what: /path/\n    where: [this, is, a, list]",
			`mount-control "where" must be a string`,
		},
		{
			"mount:\n  - what: /\n    where: /\n    persistent: string",
			`mount-control "persistent" must be a boolean`,
		},
		{
			"mount:\n  - what: /\n    where: /\n    type: string",
			`mount-control "type" must be an array of strings.*`,
		},
		{
			"mount:\n  - what: /\n    where: /\n    type: [true, false]",
			`mount-control "type" element 1 not a string.*`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    type: [auto)]",
			`mount-control filesystem type invalid.*`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    type: [upperCase]",
			`mount-control filesystem type invalid.*`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    type: [two words]",
			`mount-control filesystem type invalid.*`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n",
			`mount-control "options" cannot be empty`,
		},
		{
			"mount:\n  - what: /\n    where: /\n    options: string",
			`mount-control "options" must be an array of strings.*`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    options: []",
			`mount-control "options" cannot be empty`,
		},
		{
			"mount:\n  - what: here\n    where: /mnt",
			`mount-control "what" attribute is invalid: must start with / and not contain special characters`,
		},
		{
			"mount:\n  - what: /double\"quote\n    where: /mnt",
			`mount-control "what" attribute is invalid: must start with / and not contain special characters`,
		},
		{
			"mount:\n  - what: /variables/are/not/@{allowed}\n    where: /mnt",
			`mount-control "what" attribute is invalid: must start with / and not contain special characters`,
		},
		{
			"mount:\n  - what: /invalid}pattern\n    where: /mnt",
			`mount-control "what" setting cannot be used: invalid closing brace, no matching open.*`,
		},
		{
			"mount:\n  - what: /\n    where: /\n    options: [ro]",
			`mount-control "where" attribute must start with \$SNAP_COMMON, \$SNAP_DATA or / and not contain special characters`,
		},
		{
			"mount:\n  - what: /\n    where: /media/no\"quotes",
			`mount-control "where" attribute must start with \$SNAP_COMMON, \$SNAP_DATA or / and not contain special characters`,
		},
		{
			"mount:\n  - what: /\n    where: /media/no@{variables}",
			`mount-control "where" attribute must start with \$SNAP_COMMON, \$SNAP_DATA or / and not contain special characters`,
		},
		{
			"mount:\n  - what: /\n    where: $SNAP_DATA/$SNAP_DATA",
			`mount-control "where" attribute must start with \$SNAP_COMMON, \$SNAP_DATA or / and not contain special characters`,
		},
		{
			"mount:\n  - what: /\n    where: /$SNAP_DATA",
			`mount-control "where" attribute must start with \$SNAP_COMMON, \$SNAP_DATA or / and not contain special characters`,
		},
		{
			"mount:\n  - what: /\n    where: /media/invalid[path",
			`mount-control "where" setting cannot be used: missing closing bracket ']'.*`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    options: [sync,invalid]",
			`mount-control option unrecognized or forbidden: "invalid"`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    type: [ext4,debugfs]",
			`mount-control forbidden filesystem type: "debugfs"`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    type: [ext4]\n    options: [rw,bind]",
			`mount-control option "bind" is incompatible with specifying filesystem type`,
		},
		{
			"mount:\n  - what: /tmp/..\n    where: /media/*",
			`mount-control "what" pattern is not clean:.*`,
		},
		{
			"mount:\n  - what: /\n    where: /media/../etc",
			`mount-control "where" pattern is not clean:.*`,
		},
		{
			"mount:\n  - what: none\n    where: /media/*\n    options: [rw]",
			`mount-control "what" attribute can be "none" only with "tmpfs"`,
		},
		{
			"mount:\n  - what: none\n    where: /media/*\n    options: [rw]\n    type: [ext4,ntfs]",
			`mount-control "what" attribute can be "none" only with "tmpfs"`,
		},
		{
			"mount:\n  - what: none\n    where: /media/*\n    options: [rw]\n    type: [tmpfs,ext4]",
			`mount-control filesystem type "tmpfs" cannot be listed with other types`,
		},
		{
			"mount:\n  - what: /\n    where: /media/*\n    options: [rw]\n    type: [tmpfs]",
			`mount-control "what" attribute must be "none" with "tmpfs"; found "/" instead`,
		},
		{
			"mount:\n  - what: /\n    where: $SNAP_DATA/foo\n    options: [ro]\n    persistent: true",
			`mount-control "persistent" attribute cannot be used to mount onto \$SNAP_DATA`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(mountControlYaml, testData.plugYaml)
		plug, _ := MockConnectedPlug(c, snapYaml, nil, "mntctl")
		err := interfaces.BeforeConnectPlug(s.iface, plug)
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("Yaml: %s", testData.plugYaml))
	}
}

func (s *MountControlInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "mount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount2\n")
}

func (s *MountControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `capability sys_admin,`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/{,usr/}bin/mount ixr,`)

	expectedMountLine1 := `mount fstype=(ext2,ext3,ext4) options=(rw,sync) "/dev/sd*" -> "/media/**",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedMountLine1)

	expectedMountLine2 := `mount  options=(bind) "/usr/**" -> "/var/snap/consumer/common/**",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedMountLine2)

	expectedMountLine3 := `mount fstype=(` +
		`aufs,autofs,btrfs,ext2,ext3,ext4,hfs,iso9660,jfs,msdos,ntfs,ramfs,` +
		`reiserfs,squashfs,tmpfs,ubifs,udf,ufs,vfat,zfs,xfs` +
		`) options=(ro) "/dev/sda{0,1}" -> "/var/snap/consumer/common/**",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedMountLine3)

	expectedMountLine4 := `mount fstype=(` +
		`aufs,autofs,btrfs,ext2,ext3,ext4,hfs,iso9660,jfs,msdos,ntfs,ramfs,` +
		`reiserfs,squashfs,tmpfs,ubifs,udf,ufs,vfat,zfs,xfs` +
		`) options=(sync) "/dev/sda[0-1]" -> "/var/snap/consumer/common/{foo,other,**}",`
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, expectedMountLine4)
}

func (s *MountControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows creating transient and persistent mounts`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "mount-control")
}

func (s *MountControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *MountControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
