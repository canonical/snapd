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

type sensitiveFilesInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&sensitiveFilesInterfaceSuite{
	iface: builtin.MustInterface("sensitive-files"),
})

func (s *sensitiveFilesInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 sensitive-files:
  read: [$HOME/.read-dir1, /etc/read-dir2, $HOME/.read-file2, /etc/read-file2]
  write:  [$HOME/.write-dir1, /etc/write-dir2, $HOME/.write-file2, /etc/write-file2]
apps:
 app:
  command: foo
  plugs: [sensitive-files]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "sensitive-files",
		Interface: "sensitive-files",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["sensitive-files"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *sensitiveFilesInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "sensitive-files")
}

func (s *sensitiveFilesInterfaceSuite) TestConnectedPlugAppArmor(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app"), Equals, `
# Description: Can access specific files or directories.
# This is restricted because it gives file access to arbitrary locations.
owner "@{HOME}/.read-dir1{,/,/**}" rk,
"/etc/read-dir2{,/,/**}" rk,
owner "@{HOME}/.read-file2{,/,/**}" rk,
"/etc/read-file2{,/,/**}" rk,
owner "@{HOME}/.write-dir1{,/,/**}" rwkl,
"/etc/write-dir2{,/,/**}" rwkl,
owner "@{HOME}/.write-file2{,/,/**}" rwkl,
"/etc/write-file2{,/,/**}" rwkl,
`)
}

func (s *sensitiveFilesInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "sensitive-files",
		Interface: "sensitive-files",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"sensitive-files slots are reserved for the core snap")
}

func (s *sensitiveFilesInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *sensitiveFilesInterfaceSuite) TestSanitizePlugHappy(c *C) {
	const mockSnapYaml = `name: sensitive-files-plug-snap
version: 1.0
plugs:
 sensitive-files:
  read: ["$HOME/.file1"]
  write: ["$HOME/.dir1"]
`
	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["sensitive-files"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), IsNil)
}

func (s *sensitiveFilesInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	const mockSnapYaml = `name: sensitive-files-plug-snap
version: 1.0
plugs:
 sensitive-files:
  $t
`
	errPrefix := `cannot add sensitive-files plug: `
	var testCases = []struct {
		inp    string
		errStr string
	}{
		{`read: ""`, `"read" must be a list of strings`},
		{`read: [ 123 ]`, `"read" must be a list of strings`},
		{`read: [ "/foo/./bar" ]`, `"/foo/./bar" must be clean`},
		{`read: [ "../foo" ]`, `"../foo" must start with "/" or "\$HOME"`},
		{`read: [ "/foo[" ]`, `"/foo\[" contains a reserved apparmor char from .*`},
		{`write: ""`, `"write" must be a list of strings`},
		{`write: bar`, `"write" must be a list of strings`},
		{`read: [ "~/foo" ]`, `"~/foo" must start with "/" or "\$HOME"`},
		{`read: [ "/foo/~/foo" ]`, `"/foo/~/foo" contains invalid "~"`},
		{`read: [ "/foo/../foo" ]`, `"/foo/../foo" must be clean`},
		{`read: [ "/home/$HOME/foo" ]`, `\$HOME must only be used at the start of the path of "/home/\$HOME/foo"`},
		{`read: [ "/@{FOO}" ]`, `"/@{FOO}" should not use "@{"`},
		{`read: [ "/home/@{HOME}/foo" ]`, `"/home/@{HOME}/foo" should not use "@{"`},
	}

	for _, t := range testCases {
		yml := strings.Replace(mockSnapYaml, "$t", t.inp, -1)
		info := snaptest.MockInfo(c, yml, nil)
		plug := info.Plugs["sensitive-files"]

		c.Check(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, errPrefix+t.errStr, Commentf("unexpected error for %q", t.inp))
	}
}

func (s *sensitiveFilesInterfaceSuite) TestConnectedPlugAppArmorInternalError(c *C) {
	const mockPlugSnapInfo = `name: other
version: 1.0
plugs:
 sensitive-files:
  read: [ 123 , 345 ]
apps:
 app:
  command: foo
  plugs: [sensitive-files]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "sensitive-files",
		Interface: "sensitive-files",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["sensitive-files"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, ErrorMatches, `cannot connect plug sensitive-files: 123 \(int64\) is not a string`)
}

func (s *sensitiveFilesInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
