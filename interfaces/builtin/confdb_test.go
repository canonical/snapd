// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type confdbSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&confdbSuite{
	iface: builtin.MustInterface("confdb"),
})

const plugYaml = `name: plugger
version: 1.0
type: app
plugs:
 read-ssid:
  interface: confdb
  account: foo
  view: bar/baz
apps:
 app:
  command: foo
  plugs: [read-ssid]
`

const slotYaml = `name: core
version: 1.0
type: os
slots:
 confdb:
  interface: confdb
`

func (s *confdbSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, plugYaml, nil, "read-ssid")
	s.slot, s.slotInfo = MockConnectedSlot(c, slotYaml, nil, "confdb")
}

func (s *confdbSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "confdb")
}

func (s *confdbSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *confdbSuite) TestConfdbSanitizePlug(c *C) {
	type testcase struct {
		account string
		view    string
		role    string
		err     string
	}

	tcs := []testcase{
		{
			account: "my-acc",
			view:    "network/wifi",
			role:    "custodian",
		},
		{
			account: "my-acc",
			view:    "network/wifi",
		},
		{
			err: `confdb plug must have an "account" attribute`,
		},
		{
			account: "my-acc",
			err:     `confdb plug must have a "view" attribute`,
		},
		{
			account: "my-acc",
			view:    "reg/view",
			role:    "observer",
			err:     `optional confdb plug "role" attribute must be "custodian"`,
		},
		{
			account: "my-acc",
			view:    "foobar",
			err:     `confdb plug must have a valid "view" attribute: expected confdb and view names separated by a single '/': foobar`,
		},
		{
			account: "my-acc",
			view:    "0-foo/bar",
			err:     `confdb plug must have a valid "view" attribute: invalid confdb name: 0-foo does not match '^[a-z](?:-?[a-z0-9])*$'`,
		},
		{
			account: "my-acc",
			view:    "foo/0-bar",
			err:     `confdb plug must have a valid "view" attribute: invalid view name: 0-bar does not match '^[a-z](?:-?[a-z0-9])*$'`,
		},
		{
			account: "_my-acc",
			view:    "foo/bar",
			err:     `confdb plug must have a valid "account" attribute: format mismatch`,
		},
	}

	for _, tc := range tcs {
		var accLine, viewLine, roleLine string
		if tc.account != "" {
			accLine = fmt.Sprintf("  account: %s\n", tc.account)
		}
		if tc.view != "" {
			viewLine = fmt.Sprintf("  view: %s\n", tc.view)
		}
		if tc.role != "" {
			roleLine = fmt.Sprintf("  role: %s\n", tc.role)
		}

		mockSnapYaml := `name: ssid-reader
version: 1.0
plugs:
 read-ssid:
  interface: confdb
` + accLine + viewLine + roleLine

		info := snaptest.MockInfo(c, mockSnapYaml, nil)
		plug := info.Plugs["read-ssid"]
		err := interfaces.BeforePreparePlug(s.iface, plug)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, NotNil)
			c.Assert(err.Error(), Equals, tc.err)
		}
	}
}

func (s *confdbSuite) TestConfdbDoesntAddRules(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)
	c.Assert(apparmorSpec.SnippetForTag("snap.plugger.app"), Equals, "")

	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), HasLen, 0)
	c.Check(seccompSpec.SnippetForTag("snap.plugger.app"), Equals, "")

	udevSpec := udev.NewSpecification(s.plug.AppSet())
	err = udevSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 0)
}
