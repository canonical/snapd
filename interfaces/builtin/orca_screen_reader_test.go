// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"github.com/snapcore/snapd/testutil"
)

type AccessibilityLegacyInterfaceSuite struct {
	iface    interfaces.Interface
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const accessibilityLegacyConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [orca-screen-reader]
`

var _ = Suite(&AccessibilityLegacyInterfaceSuite{
	iface: builtin.MustInterface("orca-screen-reader"),
})

func (s *AccessibilityLegacyInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, accessibilityLegacyConsumerYaml, nil, "orca-screen-reader")
}

func (s *AccessibilityLegacyInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "orca-screen-reader")
}

// check if a string is part of any of the strings in a slice
func checkDBusRules(dbus_rules []string, matches []string) bool {
	for _, rule := range dbus_rules {
		found := 0
		for _, match := range matches {
			if strings.HasSuffix(match, ",") {
				match = strings.TrimSuffix(match, ",")
			}
			if strings.Contains(rule, match+"\n") {
				found++
			} else {
				found = -1
				break
			}
		}
		if found == len(matches) {
			return true
		}
	}
	return false
}

func getDBusRules(spec *apparmor.Specification) []string {
	dbus_rules := strings.Split(spec.SnippetForTag("snap.consumer.app"), "dbus ")
	out := []string{}
	for _, rule := range dbus_rules {
		out = append(out, strings.Split(rule, ",\n")[0]+"\n")
	}
	return out
}

func (s *AccessibilityLegacyInterfaceSuite) TestAppArmorSpecForConnectedPlug(c *C) {
	// connected plug to core slot
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil), IsNil)

	dbus_rules := getDBusRules(spec)
	c.Assert(checkDBusRules(dbus_rules, []string{"(send, receive)", "bus=accessibility"}), Equals, true)
	c.Assert(checkDBusRules(dbus_rules, []string{"(send, receive)", "bus=session", "path=/org/a11y/bus{,/**}"}), Equals, true)
	c.Assert(checkDBusRules(dbus_rules, []string{"(bind)", "bus=session", "name=\"org.gnome.Orca.Service\""}), Equals, true)

	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/dbus-accessibility-strict>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/run/user/[0-9]*/at-spi{,2-[0-9A-Z]*}/ rw,")
}

func (s *AccessibilityLegacyInterfaceSuite) TestAppArmorSpecForNotConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	dbus_rules := getDBusRules(spec)
	c.Assert(checkDBusRules(dbus_rules, []string{"(send, receive)", "bus=accessibility"}), Equals, false)
	c.Assert(checkDBusRules(dbus_rules, []string{"(send, receive)", "bus=session", "path=/org/a11y/bus{,/**}"}), Equals, false)
	c.Assert(checkDBusRules(dbus_rules, []string{"(bind)", "bus=session", "name=\"org.gnome.Orca.Service\""}), Equals, false)

	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "#include <abstractions/dbus-accessibility-strict>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "/run/user/[0-9]*/at-spi{,2-[0-9A-Z]*}/ rw,")
}
