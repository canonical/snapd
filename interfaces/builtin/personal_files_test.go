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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type personalFilesInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&personalFilesInterfaceSuite{
	iface: builtin.MustInterface("personal-files"),
})

func (s *personalFilesInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "personal-files")
}

func (s *personalFilesInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	const mockSnapYaml = `name: personal-files-plug-snap
version: 1.0
plugs:
 personal-files:
  $t
`
	errPrefix := `cannot add personal-files plug: `
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
		plug := info.Plugs["personal-files"]

		c.Check(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, errPrefix+t.errStr, Commentf("unexpected error for %q", t.inp))
	}
}

func (s *personalFilesInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
