// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package interfaces_test

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces"
)

type YamlSuite struct{}

var _ = Suite(&YamlSuite{})

func (s *YamlSuite) TestUnmarshalGarbage(c *C) {
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`"`))
	c.Assert(err, ErrorMatches, "yaml: found unexpected end of stream")
}

func (s *YamlSuite) TestUnmarshalEmpty(c *C) {
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(``))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, HasLen, 0)
}

// Tests focusing on plugs

func (s *YamlSuite) TestUnmarshalStandaloneImplicitPlug(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    network-client:
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "network-client",
			Interface: "network-client",
		},
	})
	c.Assert(slots, HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalStandaloneAbbreviatedPlug(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net: network-client
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
		},
	})
	c.Assert(slots, HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalStandaloneMinimalisticPlug(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net:
        interface: network-client
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
		},
	})
	c.Assert(slots, HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalStandaloneCompletePlug(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net:
        interface: network-client
        ipv6-aware: true
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
			Attrs:     map[string]interface{}{"ipv6-aware": true},
		},
	})
	c.Assert(slots, HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalLastPlugDefinitionWins(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net:
        interface: network-client
        attr: 1
    net:
        interface: network-client
        attr: 2
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
			Attrs:     map[string]interface{}{"attr": 2},
		},
	})
}

func (s *YamlSuite) TestUnmarshalPlugsExplicitlyDefinedImplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    network-client:
apps:
    app:
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "network-client",
			Interface: "network-client",
			Apps:      []string{"app"},
		},
	})
	c.Assert(slots, HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalPlugsExplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net: network-client
apps:
    app:
        plugs: ["net"]
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
			Apps:      []string{"app"},
		},
	})
	c.Assert(slots, HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalPlugsImplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
apps:
    app:
        plugs: ["network-client"]
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, DeepEquals, []interfaces.Plug{
		{
			Snap:      "snap",
			Name:      "network-client",
			Interface: "network-client",
			Apps:      []string{"app"},
		},
	})
	c.Assert(slots, HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithoutInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net:
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `plug "net" doesn't define interface name`)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithNonStringInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net:
        interface: 1.0
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `interface name on plug "net" is not a string \(found float64\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithNonStringAttributes(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net:
        1: ok
`))
	c.Assert(err, ErrorMatches, `plug "net" has attribute that is not a string \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithUnexpectedType(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    net: 5
`))
	c.Assert(err, ErrorMatches, `plug "net" has malformed definition \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalReservedPlugAttribute(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
plugs:
    serial:
        interface: serial-port
        $baud-rate: [9600]
`))
	c.Assert(err, ErrorMatches, `plug "serial" uses reserved attribute "\$baud-rate"`)
}

// Tests focusing on slots

func (s *YamlSuite) TestUnmarshalStandaloneImplicitSlot(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    network-client:
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "network-client",
			Interface: "network-client",
		},
	})
}

func (s *YamlSuite) TestUnmarshalStandaloneAbbreviatedSlot(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net: network-client
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
		},
	})
}

func (s *YamlSuite) TestUnmarshalStandaloneMinimalisticSlot(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net:
        interface: network-client
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
		},
	})
}

func (s *YamlSuite) TestUnmarshalStandaloneCompleteSlot(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net:
        interface: network-client
        ipv6-aware: true
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
			Attrs:     map[string]interface{}{"ipv6-aware": true},
		},
	})
}

func (s *YamlSuite) TestUnmarshalLastSlotDefinitionWins(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net:
        interface: network-client
        attr: 1
    net:
        interface: network-client
        attr: 2
`))
	c.Assert(err, IsNil)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
			Attrs:     map[string]interface{}{"attr": 2},
		},
	})
}

func (s *YamlSuite) TestUnmarshalSlotsExplicitlyDefinedImplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    network-client:
apps:
    app:
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "network-client",
			Interface: "network-client",
			Apps:      []string{"app"},
		},
	})
}

func (s *YamlSuite) TestUnmarshalSlotsExplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net: network-client
apps:
    app:
        slots: ["net"]
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "net",
			Interface: "network-client",
			Apps:      []string{"app"},
		},
	})
}

func (s *YamlSuite) TestUnmarshalSlotsImplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	plugs, slots, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
apps:
    app:
        slots: ["network-client"]
`))
	c.Assert(err, IsNil)
	c.Assert(plugs, HasLen, 0)
	c.Assert(slots, DeepEquals, []interfaces.Slot{
		{
			Snap:      "snap",
			Name:      "network-client",
			Interface: "network-client",
			Apps:      []string{"app"},
		},
	})
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithoutInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net:
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `slot "net" doesn't define interface name`)
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithNonStringInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net:
        interface: 1.0
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `interface name on slot "net" is not a string \(found float64\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithNonStringAttributes(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net:
        1: ok
`))
	c.Assert(err, ErrorMatches, `slot "net" has attribute that is not a string \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithUnexpectedType(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    net: 5
`))
	c.Assert(err, ErrorMatches, `slot "net" has malformed definition \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalReservedSlotAttribute(c *C) {
	// NOTE: yaml content cannot use tabs, intent the section with spaces.
	_, _, err := interfaces.PlugsAndSlotsFromYaml([]byte(`
name: snap
slots:
    serial:
        interface: serial-port
        $baud-rate: [9600]
`))
	c.Assert(err, ErrorMatches, `slot "serial" uses reserved attribute "\$baud-rate"`)
}
