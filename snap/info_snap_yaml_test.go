// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	"testing"

	"github.com/ubuntu-core/snappy/snap"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type InfoSnapYamlTestSuite struct {
}

var _ = Suite(&InfoSnapYamlTestSuite{})

var mockYaml = []byte(`name: foo
version: 1.0
type: app
`)

func (s *InfoSnapYamlTestSuite) TestSimple(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)
	c.Assert(info.Name, Equals, "foo")
	c.Assert(info.Version, Equals, "1.0")
	c.Assert(info.Type, Equals, snap.TypeApp)
}

func (s *InfoSnapYamlTestSuite) TestFail(c *C) {
	_, err := snap.InfoFromSnapYaml([]byte("random-crap"))
	c.Assert(err, ErrorMatches, "(?m)info failed to parse:.*")
}

type YamlSuite struct{}

var _ = Suite(&YamlSuite{})

func (s *YamlSuite) TestUnmarshalGarbage(c *C) {
	_, err := snap.InfoFromSnapYaml([]byte(`"`))
	c.Assert(err, ErrorMatches, ".*: yaml: found unexpected end of stream")
}

func (s *YamlSuite) TestUnmarshalEmpty(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(``))
	c.Assert(err, IsNil)
	c.Assert(info.Plugs, HasLen, 0)
	c.Assert(info.Slots, HasLen, 0)
	c.Assert(info.Apps, HasLen, 0)
}

// Tests focusing on plugs

func (s *YamlSuite) TestUnmarshalStandaloneImplicitPlug(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    network-client:
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	plug := info.Plugs["network-client"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "network-client")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalStandaloneAbbreviatedPlug(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net: network-client
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	plug := info.Plugs["net"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "net")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalStandaloneMinimalisticPlug(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net:
        interface: network-client
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	plug := info.Plugs["net"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "net")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalStandaloneCompletePlug(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net:
        interface: network-client
        ipv6-aware: true
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	plug := info.Plugs["net"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "net")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Attrs, DeepEquals, map[string]interface{}{"ipv6-aware": true})
	c.Check(plug.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalLastPlugDefinitionWins(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
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
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	plug := info.Plugs["net"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "net")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Attrs, DeepEquals, map[string]interface{}{"attr": 2})
	c.Check(plug.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalPlugsExplicitlyDefinedImplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    network-client:
apps:
    app:
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 1)
	plug := info.Plugs["network-client"]
	app := info.Apps["app"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "network-client")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Attrs, HasLen, 0)
	c.Check(plug.Snap, Equals, info)
	c.Check(plug.Apps(), DeepEquals, []*snap.AppInfo{app})

	c.Assert(app, Not(IsNil))
	c.Check(app.Plugs(), DeepEquals, []*snap.PlugInfo{plug})
	c.Check(app.Slots(), HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalPlugsExplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net: network-client
apps:
    app:
        plugs: ["net"]
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 1)
	plug := info.Plugs["net"]
	app := info.Apps["app"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "net")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Attrs, HasLen, 0)
	c.Check(plug.Snap, Equals, info)
	c.Check(plug.Apps(), DeepEquals, []*snap.AppInfo{app})

	c.Assert(app, Not(IsNil))
	c.Check(app.Plugs(), DeepEquals, []*snap.PlugInfo{plug})
	c.Check(app.Slots(), HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalPlugsImplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
apps:
    app:
        plugs: ["network-client"]
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 1)
	plug := info.Plugs["network-client"]
	app := info.Apps["app"]

	c.Assert(plug, Not(IsNil))
	c.Check(plug.Name, Equals, "network-client")
	c.Check(plug.Interface, Equals, "network-client")
	c.Check(plug.Attrs, HasLen, 0)
	c.Check(plug.Snap, Equals, info)
	c.Check(plug.Apps(), DeepEquals, []*snap.AppInfo{app})

	c.Assert(app, Not(IsNil))
	c.Check(app.Plugs(), DeepEquals, []*snap.PlugInfo{plug})
	c.Check(app.Slots(), HasLen, 0)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithoutInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net:
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `plug "net" doesn't define interface name`)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithNonStringInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net:
        interface: 1.0
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `interface name on plug "net" is not a string \(found float64\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithNonStringAttributes(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net:
        1: ok
`))
	c.Assert(err, ErrorMatches, `plug "net" has attribute that is not a string \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithUnexpectedType(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net: 5
`))
	c.Assert(err, ErrorMatches, `plug "net" has malformed definition \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalReservedPlugAttribute(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
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
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    network-client:
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	slot := info.Slots["network-client"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "network-client")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalStandaloneAbbreviatedSlot(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net: network-client
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	slot := info.Slots["net"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "net")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalStandaloneMinimalisticSlot(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net:
        interface: network-client
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	slot := info.Slots["net"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "net")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalStandaloneCompleteSlot(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net:
        interface: network-client
        ipv6-aware: true
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	slot := info.Slots["net"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "net")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Attrs, DeepEquals, map[string]interface{}{"ipv6-aware": true})
	c.Check(slot.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalLastSlotDefinitionWins(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
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
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	slot := info.Slots["net"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "net")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Attrs, DeepEquals, map[string]interface{}{"attr": 2})
	c.Check(slot.Snap, Equals, info)
}

func (s *YamlSuite) TestUnmarshalSlotsExplicitlyDefinedImplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    network-client:
apps:
    app:
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 1)
	slot := info.Slots["network-client"]
	app := info.Apps["app"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "network-client")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Attrs, HasLen, 0)
	c.Check(slot.Snap, Equals, info)
	c.Check(slot.Apps(), DeepEquals, []*snap.AppInfo{app})

	c.Assert(app, Not(IsNil))
	c.Check(app.Plugs(), HasLen, 0)
	c.Check(app.Slots(), DeepEquals, []*snap.SlotInfo{slot})
}

func (s *YamlSuite) TestUnmarshalSlotsExplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net: network-client
apps:
    app:
        slots: ["net"]
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 1)
	slot := info.Slots["net"]
	app := info.Apps["app"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "net")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Attrs, HasLen, 0)
	c.Check(slot.Snap, Equals, info)
	c.Check(slot.Apps(), DeepEquals, []*snap.AppInfo{app})

	c.Assert(app, Not(IsNil))
	c.Check(app.Plugs(), HasLen, 0)
	c.Check(app.Slots(), DeepEquals, []*snap.SlotInfo{slot})
}

func (s *YamlSuite) TestUnmarshalSlotsImplicitlyDefinedExplicitlyBoundToApps(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
apps:
    app:
        slots: ["network-client"]
`))
	c.Assert(err, IsNil)
	c.Check(info.Name, Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 1)
	slot := info.Slots["network-client"]
	app := info.Apps["app"]

	c.Assert(slot, Not(IsNil))
	c.Check(slot.Name, Equals, "network-client")
	c.Check(slot.Interface, Equals, "network-client")
	c.Check(slot.Attrs, HasLen, 0)
	c.Check(slot.Snap, Equals, info)
	c.Check(slot.Apps(), DeepEquals, []*snap.AppInfo{app})

	c.Assert(app, Not(IsNil))
	c.Check(app.Plugs(), HasLen, 0)
	c.Check(app.Slots(), DeepEquals, []*snap.SlotInfo{slot})
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithoutInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net:
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `slot "net" doesn't define interface name`)
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithNonStringInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net:
        interface: 1.0
        ipv6-aware: true
`))
	c.Assert(err, ErrorMatches, `interface name on slot "net" is not a string \(found float64\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithNonStringAttributes(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net:
        1: ok
`))
	c.Assert(err, ErrorMatches, `slot "net" has attribute that is not a string \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithUnexpectedType(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net: 5
`))
	c.Assert(err, ErrorMatches, `slot "net" has malformed definition \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalReservedSlotAttribute(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    serial:
        interface: serial-port
        $baud-rate: [9600]
`))
	c.Assert(err, ErrorMatches, `slot "serial" uses reserved attribute "\$baud-rate"`)
}
