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
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type polkitInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&polkitInterfaceSuite{
	iface: builtin.MustInterface("polkit"),
})

func (s *polkitInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})

	const mockPlugSnapInfoYaml = `name: other
version: 1.0
plugs:
 polkit:
  action-prefix: org.example.foo
apps:
 app:
  command: foo
  plugs: [polkit]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 polkit:
  interface: polkit
`

	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "polkit")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "polkit")
}

func (s *polkitInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "polkit")
}

func (s *polkitInterfaceSuite) TestConnectedPlugAppArmor(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "# Description: Can talk to polkitd's CheckAuthorization API")
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `member="{,Cancel}CheckAuthorization"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkit(c *C) {
	const samplePolicy1 = `<policyconfig>
  <action id="org.example.foo.some-action">
    <description>Some action</description>
    <message>Authentication is required to do some action</message>
    <defaults>
      <allow_any>no</allow_any>
      <allow_inactive>no</allow_inactive>
      <allow_active>auth_admin</allow_active>
    </defaults>
  </action>
</policyconfig>`
	const samplePolicy2 = `<policyconfig/>`

	c.Assert(os.MkdirAll(filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit"), 0755), IsNil)
	policyPath := filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy1), 0644), IsNil)
	policyPath = filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit/polkit.bar.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy2), 0644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)

	c.Check(polkitSpec.Policies(), DeepEquals, map[string]polkit.Policy{
		"polkit.foo": polkit.Policy(samplePolicy1),
		"polkit.bar": polkit.Policy(samplePolicy2),
	})
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitMissing(c *C) {
	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Check(err, ErrorMatches, `cannot find any policy files for plug "polkit"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitNotFile(c *C) {
	c.Assert(os.MkdirAll(filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit"), 0755), IsNil)
	policyPath := filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.Mkdir(policyPath, 0755), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Check(err, ErrorMatches, `cannot read file ".*/meta/polkit/polkit.foo.policy": read .*: is a directory`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitBadXML(c *C) {
	const samplePolicy = `<malformed`
	c.Assert(os.MkdirAll(filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit"), 0755), IsNil)
	policyPath := filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy), 0644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Check(err, ErrorMatches, `cannot validate policy file ".*/meta/polkit/polkit.foo.policy": XML syntax error on line 1: unexpected EOF`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitBadAction(c *C) {
	const samplePolicy = `<policyconfig>
  <action id="org.freedesktop.systemd1.manage-units">
    <description>A conflict with systemd's polkit actions</description>
    <message>Manage system services</message>
    <defaults>
      <allow_any>yes</allow_any>
    </defaults>
  </action>
</policyconfig>`
	c.Assert(os.MkdirAll(filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit"), 0755), IsNil)
	policyPath := filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy), 0644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Check(err, ErrorMatches, `policy file ".*/meta/polkit/polkit.foo.policy" contains unexpected action ID "org.freedesktop.systemd1.manage-units"`)
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitBadImplies(c *C) {
	const samplePolicy = `<policyconfig>
  <action id="org.example.foo.some-action">
    <description>Some action</description>
    <message>Allow "some action" (and also managing system services for some reason)</message>
    <defaults>
      <allow_any>yes</allow_any>
    </defaults>
    <annotate key="org.freedesktop.policykit.imply">org.freedesktop.systemd1.manage-units</annotate>
  </action>
</policyconfig>`
	c.Assert(os.MkdirAll(filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit"), 0755), IsNil)
	policyPath := filepath.Join(s.plugInfo.Snap.MountDir(), "meta/polkit/polkit.foo.policy")
	c.Assert(os.WriteFile(policyPath, []byte(samplePolicy), 0644), IsNil)

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Check(err, ErrorMatches, `policy file ".*/meta/polkit/polkit.foo.policy" contains unexpected action ID "org.freedesktop.systemd1.manage-units"`)
}

func (s *polkitInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlugHappy(c *C) {
	const mockSnapYaml = `name: polkit-plug-snap
version: 1.0
plugs:
 polkit:
  action-prefix: org.example.bar
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["polkit"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *polkitInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	const mockSnapYaml = `name: polkit-plug-snap
version: 1.0
plugs:
 polkit:
  $t
`
	var testCases = []struct {
		inp    string
		errStr string
	}{
		{"", `snap "polkit-plug-snap" does not have attribute "action-prefix" for interface "polkit"`},
		{`action-prefix: true`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type bool for "action-prefix" attribute: \*string`},
		{`action-prefix: 42`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type int64 for "action-prefix" attribute: \*string`},
		{`action-prefix: []`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type \[\]interface {} for "action-prefix" attribute: \*string`},
		{`action-prefix: {}`, `snap "polkit-plug-snap" has interface "polkit" with invalid value type map\[string\]interface {} for "action-prefix" attribute: \*string`},

		{`action-prefix: ""`, `plug has invalid action-prefix: ""`},
		{`action-prefix: "org"`, `plug has invalid action-prefix: "org"`},
		{`action-prefix: "a+b"`, `plug has invalid action-prefix: "a\+b"`},
		{`action-prefix: "org.example\n"`, `plug has invalid action-prefix: "org.example\\n"`},
		{`action-prefix: "com.example "`, `plug has invalid action-prefix: "com.example "`},
		{`action-prefix: "123.foo.bar"`, `plug has invalid action-prefix: "123.foo.bar"`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.inp, -1)
		info := snaptest.MockInfo(c, yml, nil)
		plug := info.Plugs["polkit"]

		c.Check(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, t.errStr, Commentf("unexpected error for %q", t.inp))
	}
}

func (s *polkitInterfaceSuite) TestConnectedPlugPolkitInternalError(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
plugs:
 polkit:
  action-prefix: false
apps:
 app:
  command: foo
  plugs: [polkit]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 polkit:
  interface: polkit
`
	slot, _ := MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "polkit")
	plug, _ := MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "polkit")

	polkitSpec := &polkit.Specification{}
	err := polkitSpec.AddConnectedPlug(s.iface, plug, slot)
	c.Assert(err, ErrorMatches, `snap "other" has interface "polkit" with invalid value type bool for "action-prefix" attribute: \*string`)
}

func (s *polkitInterfaceSuite) isProfilePathAccessible(c *C) bool {
	mntProfile, err := osutil.LoadMountProfile("/proc/self/mounts")
	c.Assert(err, IsNil)
	return builtin.IsPathMountedWritable(mntProfile, "/usr/share/polkit-1/actions")
}

func (s *polkitInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, s.isProfilePathAccessible(c) && (osutil.IsExecutable("/usr/libexec/polkitd") || osutil.IsExecutable("/usr/lib/polkit-1/polkitd")))
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, "allows access to polkitd to check authorisation")
	c.Check(si.BaseDeclarationPlugs, testutil.Contains, "polkit")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "polkit")
}

func (s *polkitInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *polkitInterfaceSuite) TestIsPathMountedWritable(c *C) {

	tests := []struct {
		mounts   string
		path     string
		expected bool
	}{
		// Test a base case where root is ro
		{
			`rpool/ROOT/ubuntu / zfs ro,relatime,xattr,posixacl,casesensitive 0 0
`,
			"/usr/share/polkit-1/actions",
			false,
		},

		// Test a base case where root is rw
		{
			`rpool/ROOT/ubuntu / zfs rw,relatime,xattr,posixacl,casesensitive 0 0
`,
			"/usr/share/polkit-1/actions",
			true,
		},

		// Test a case where the root is mounted rw, but /usr/share is ro
		{
			`rpool/ROOT/ubuntu / zfs rw,relatime,xattr,posixacl,casesensitive 0 0
rpool/ROOT/ubuntu/usr/share /usr/share zfs ro,relatime,xattr,posixacl,casesensitive 0 0
`,
			"/usr/share/polkit-1/actions",
			false,
		},

		// Test a case where the root is mounted ro, but /usr/share is rw
		{
			`rpool/ROOT/ubuntu / zfs ro,relatime,xattr,posixacl,casesensitive 0 0
rpool/ROOT/ubuntu/usr/share /usr/share zfs rw,relatime,xattr,posixacl,casesensitive 0 0
`,
			"/usr/share/polkit-1/actions",
			true,
		},

		// Test a case where the root is mounted rw, but the path specifically being ro
		{
			`rpool/ROOT/ubuntu / zfs ro,relatime,xattr,posixacl,casesensitive 0 0
rpool/ROOT/ubuntu/usr/share/polkit-1/actions /usr/share/polkit-1/actions zfs ro,relatime,xattr,posixacl,casesensitive 0 0
`,
			"/usr/share/polkit-1/actions",
			false,
		},

		// Test a case where the path string ends on '/', so we know it's handled
		{
			`rpool/ROOT/ubuntu / zfs ro,relatime,xattr,posixacl,casesensitive 0 0
rpool/ROOT/ubuntu/usr/share /usr/share zfs rw,relatime,xattr,posixacl,casesensitive 0 0
`,
			"/usr/share/",
			true,
		},
		// Test an more real example
		{
			`/dev/sda3 /run/mnt/ubuntu-boot ext4 rw,relatime 0 0
/dev/sda2 /run/mnt/ubuntu-seed vfat rw,relatime,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro[7m>[27m
/dev/sda5 /run/mnt/data ext4 rw,nosuid,relatime 0 0
/dev/sda4 /run/mnt/ubuntu-save ext4 rw,relatime,stripe=4 0 0
/dev/loop0 /run/mnt/base squashfs ro,relatime,errors=continue 0 0
/dev/loop1 /run/mnt/gadget squashfs ro,relatime,errors=continue 0 0
/dev/loop2 /run/mnt/kernel squashfs ro,relatime,errors=continue 0 0
/dev/loop3 /run/mnt/snapd squashfs ro,relatime,errors=continue 0 0
/dev/loop0 / squashfs ro,relatime,errors=continue 0 0
/dev/loop5 /snap/test-snapd-rsync-core22/1 squashfs ro,nodev,relatime,errors=continue 0 0
/dev/loop6 /snap/jq-core22/1 squashfs ro,nodev,relatime,errors=continue 0 0
nsfs /run/snapd/ns/jq-core22.mnt nsfs rw 0 0
`,
			"/usr/share/polkit-1/actions",
			false,
		},
	}

	for _, t := range tests {
		mntProfile, err := osutil.LoadMountProfileText(t.mounts)
		c.Assert(err, IsNil)
		c.Check(builtin.IsPathMountedWritable(mntProfile, t.path), Equals, t.expected)
	}
}
