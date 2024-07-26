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

type registrySuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&registrySuite{
	iface: builtin.MustInterface("registry"),
})

const plugYaml = `name: plugger
version: 1.0
type: app
plugs:
 read-ssid:
  interface: registry
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
 registry:
  interface: registry
`

func (s *registrySuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, plugYaml, nil, "read-ssid")
	s.slot, s.slotInfo = MockConnectedSlot(c, slotYaml, nil, "registry")
}

func (s *registrySuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "registry")
}

func (s *registrySuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *registrySuite) TestRegistrySanitizePlug(c *C) {
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
			role:    "manager",
		},
		{
			account: "my-acc",
			view:    "network/wifi",
		},
		{
			err: `registry plug must have an "account" attribute`,
		},
		{
			account: "my-acc",
			err:     `registry plug must have a "view" attribute`,
		},
		{
			account: "my-acc",
			view:    "reg/view",
			role:    "observer",
			err:     `optional registry plug "role" attribute must be "manager"`,
		},
		{
			account: "my-acc",
			view:    "foobar",
			err:     `registry plug must have a valid "view" attribute: expected registry and view names separated by a single '/': foobar`,
		},
		{
			account: "my-acc",
			view:    "0-foo/bar",
			err:     `registry plug must have a valid "view" attribute: invalid registry name: 0-foo does not match '^[a-z](?:-?[a-z0-9])*$'`,
		},
		{
			account: "my-acc",
			view:    "foo/0-bar",
			err:     `registry plug must have a valid "view" attribute: invalid view name: 0-bar does not match '^[a-z](?:-?[a-z0-9])*$'`,
		},
		{
			account: "_my-acc",
			view:    "foo/bar",
			err:     `registry plug must have a valid "account" attribute: format mismatch`,
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
  interface: registry
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

func (s *registrySuite) TestRegistryDoesntAddRules(c *C) {
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
