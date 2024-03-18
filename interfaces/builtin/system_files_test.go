// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type systemFilesInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&systemFilesInterfaceSuite{
	iface: builtin.MustInterface("system-files"),
})

func (s *systemFilesInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 system-files:
  read: [/etc/read-dir2, /etc/read-file2]
  write:  [/etc/write-dir2, /etc/write-file2]
apps:
 app:
  command: foo
  plugs: [system-files]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "system-files",
		Interface: "system-files",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["system-files"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *systemFilesInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "system-files")
}

func (s *systemFilesInterfaceSuite) TestConnectedPlugAppArmor(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), Equals, `
# Description: Can access specific system files or directories.
# This is restricted because it gives file access to arbitrary locations.
"/etc/read-dir2{,/,/**}" rk,
"/etc/read-file2{,/,/**}" rk,
"/etc/write-dir2{,/,/**}" rwkl,
"/etc/write-file2{,/,/**}" rwkl,
`)
}

func (s *systemFilesInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *systemFilesInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *systemFilesInterfaceSuite) TestSanitizePlugHappy(c *C) {
	const mockSnapYaml = `name: system-files-plug-snap
version: 1.0
plugs:
 system-files:
  read: ["/etc/file1"]
  write: ["/etc/dir1"]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["system-files"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *systemFilesInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	const mockSnapYaml = `name: system-files-plug-snap
version: 1.0
plugs:
 system-files:
  $t
`
	errPrefix := `cannot add system-files plug: `
	var testCases = []struct {
		inp    string
		errStr string
	}{
		{`read: ""`, `"read" must be a list of strings`},
		{`read: [ 123 ]`, `"read" must be a list of strings`},
		{`read: [ "/foo/./bar" ]`, `cannot use "/foo/./bar": try "/foo/bar"`},
		{`read: [ "../foo" ]`, `"../foo" must start with "/"`},
		{`read: [ "/foo[" ]`, `"/foo\[" contains a reserved apparmor char from .*`},
		{`write: ""`, `"write" must be a list of strings`},
		{`write: bar`, `"write" must be a list of strings`},
		{`read: [ "~/foo" ]`, `"~/foo" cannot contain "~"`},
		{`read: [ "/foo/~/foo" ]`, `"/foo/~/foo" cannot contain "~"`},
		{`read: [ "/foo/../foo" ]`, `cannot use "/foo/../foo": try "/foo"`},
		{`read: [ "/home/$HOME/foo" ]`, `\$HOME cannot be used in "/home/\$HOME/foo"`},
		{`read: [ "$HOME/sweet/$HOME" ]`, `"\$HOME/sweet/\$HOME" must start with "/"`},
		{`read: [ "/@{FOO}" ]`, `"/@{FOO}" contains a reserved apparmor char from .*`},
		{`read: [ "/home/@{HOME}/foo" ]`, `"/home/@{HOME}/foo" contains a reserved apparmor char from .*`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.inp, -1)
		info := snaptest.MockInfo(c, yml, nil)
		plug := info.Plugs["system-files"]

		c.Check(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, errPrefix+t.errStr, Commentf("unexpected error for %q", t.inp))
	}
}

func (s *systemFilesInterfaceSuite) TestConnectedPlugAppArmorInternalError(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 system-files:
  read: [ 123 , 345 ]
apps:
 app:
  command: foo
  plugs: [system-files]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "system-files",
		Interface: "system-files",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["system-files"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, ErrorMatches, `cannot connect plug system-files: 123 \(int64\) is not a string`)
}

func (s *systemFilesInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
