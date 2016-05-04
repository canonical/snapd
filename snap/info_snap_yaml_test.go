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
	"time"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
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
	c.Assert(info.Name(), Equals, "foo")
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["network-client"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
	})
}

func (s *YamlSuite) TestUnmarshalStandaloneAbbreviatedPlug(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net: network-client
`))
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"attr": 2},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 1)

	plug := info.Plugs["network-client"]
	app := info.Apps["app"]
	c.Assert(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Apps:      map[string]*snap.AppInfo{app.Name: app},
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "app",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 1)
	plug := info.Plugs["net"]
	app := info.Apps["app"]
	c.Assert(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Apps:      map[string]*snap.AppInfo{app.Name: app},
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "app",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 1)
	plug := info.Plugs["network-client"]
	app := info.Apps["app"]
	c.Assert(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Apps:      map[string]*snap.AppInfo{app.Name: app},
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "app",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},
	})
}

func (s *YamlSuite) TestUnmarshalPlugWithoutInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    network-client:
        ipv6-aware: true
`))
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Plugs["network-client"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
	})
}

func (s *YamlSuite) TestUnmarshalPlugWithLabel(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    bool-file:
        label: Disk I/O indicator
`))
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Plugs["bool-file"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "bool-file",
		Interface: "bool-file",
		Label:     "Disk I/O indicator",
	})
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

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithNonStringLabel(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    bool-file:
        label: 1.0
`))
	c.Assert(err, ErrorMatches, `label of plug "bool-file" is not a string \(found float64\)`)
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["network-client"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
	})
}

func (s *YamlSuite) TestUnmarshalStandaloneAbbreviatedSlot(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net: network-client
`))
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"attr": 2},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 1)
	slot := info.Slots["network-client"]
	app := info.Apps["app"]
	c.Assert(slot, DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Apps:      map[string]*snap.AppInfo{app.Name: app},
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "app",
		Slots: map[string]*snap.SlotInfo{slot.Name: slot},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 1)
	slot := info.Slots["net"]
	app := info.Apps["app"]
	c.Assert(slot, DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Apps:      map[string]*snap.AppInfo{app.Name: app},
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "app",
		Slots: map[string]*snap.SlotInfo{slot.Name: slot},
	})
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
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 1)
	slot := info.Slots["network-client"]
	app := info.Apps["app"]
	c.Assert(slot, DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Apps:      map[string]*snap.AppInfo{app.Name: app},
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "app",
		Slots: map[string]*snap.SlotInfo{slot.Name: slot},
	})
}

func (s *YamlSuite) TestUnmarshalSlotWithoutInterfaceName(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    network-client:
        ipv6-aware: true
`))
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Slots["network-client"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
	})
}

func (s *YamlSuite) TestUnmarshalSlotWithLabel(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    led0:
        interface: bool-file
        label: Front panel LED (red)
`))
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Slots["led0"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "led0",
		Interface: "bool-file",
		Label:     "Front panel LED (red)",
	})
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

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithNonStringLabel(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    bool-file:
        label: 1.0
`))
	c.Assert(err, ErrorMatches, `label of slot "bool-file" is not a string \(found float64\)`)
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

func (s *YamlSuite) TestUnmarshalComplexExample(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: foo
version: 1.2
summary: foo app
type: app
description: |
    Foo provides useful services
apps:
    daemon:
       command: foo --daemon
       plugs: [network, network-bind]
       slots: [foo-socket]
    foo:
       command: fooctl
       plugs: [foo-socket]
plugs:
    foo-socket:
        interface: socket
        # $protocol: foo
    logging:
        interface: syslog
slots:
    foo-socket:
        interface: socket
        path: $SNAP_DATA/socket
        protocol: foo
    tracing:
        interface: ptrace
`))
	c.Assert(err, IsNil)
	c.Check(info.Name(), Equals, "foo")
	c.Check(info.Version, Equals, "1.2")
	c.Check(info.Type, Equals, snap.TypeApp)
	c.Check(info.Summary(), Equals, "foo app")
	c.Check(info.Description(), Equals, "Foo provides useful services\n")
	c.Check(info.Apps, HasLen, 2)
	c.Check(info.Plugs, HasLen, 4)
	c.Check(info.Slots, HasLen, 2)
	// these don't come from snap.yaml
	c.Check(info.Developer, Equals, "")
	c.Check(info.Channel, Equals, "")

	app1 := info.Apps["daemon"]
	app2 := info.Apps["foo"]
	plug1 := info.Plugs["network"]
	plug2 := info.Plugs["network-bind"]
	plug3 := info.Plugs["foo-socket"]
	plug4 := info.Plugs["logging"]
	slot1 := info.Slots["foo-socket"]
	slot2 := info.Slots["tracing"]

	// app1 ("daemon") has three plugs ("network", "network-bind", "logging")
	// and two slots ("foo-socket", "tracing"). The slot "tracing" and plug
	// "logging" are global, everything else is app-bound.

	c.Assert(app1, Not(IsNil))
	c.Check(app1.Snap, Equals, info)
	c.Check(app1.Name, Equals, "daemon")
	c.Check(app1.Command, Equals, "foo --daemon")
	c.Check(app1.Plugs, DeepEquals, map[string]*snap.PlugInfo{
		plug1.Name: plug1, plug2.Name: plug2, plug4.Name: plug4})
	c.Check(app1.Slots, DeepEquals, map[string]*snap.SlotInfo{
		slot1.Name: slot1, slot2.Name: slot2})

	// app2 ("foo") has two plugs ("foo-socket", "logging") and one slot
	// ("tracing"). The slot "tracing" and plug "logging" are  global while
	// "foo-socket" is app-bound.

	c.Assert(app2, Not(IsNil))
	c.Check(app2.Snap, Equals, info)
	c.Check(app2.Name, Equals, "foo")
	c.Check(app2.Command, Equals, "fooctl")
	c.Check(app2.Plugs, DeepEquals, map[string]*snap.PlugInfo{
		plug3.Name: plug3, plug4.Name: plug4})
	c.Check(app2.Slots, DeepEquals, map[string]*snap.SlotInfo{
		slot2.Name: slot2})

	// plug1 ("network") is implicitly defined and app-bound to "daemon"

	c.Assert(plug1, Not(IsNil))
	c.Check(plug1.Snap, Equals, info)
	c.Check(plug1.Name, Equals, "network")
	c.Check(plug1.Interface, Equals, "network")
	c.Check(plug1.Attrs, HasLen, 0)
	c.Check(plug1.Label, Equals, "")
	c.Check(plug1.Apps, DeepEquals, map[string]*snap.AppInfo{app1.Name: app1})

	// plug2 ("network-bind") is implicitly defined and app-bound to "daemon"

	c.Assert(plug2, Not(IsNil))
	c.Check(plug2.Snap, Equals, info)
	c.Check(plug2.Name, Equals, "network-bind")
	c.Check(plug2.Interface, Equals, "network-bind")
	c.Check(plug2.Attrs, HasLen, 0)
	c.Check(plug2.Label, Equals, "")
	c.Check(plug2.Apps, DeepEquals, map[string]*snap.AppInfo{app1.Name: app1})

	// plug3 ("foo-socket") is app-bound to "foo"

	c.Assert(plug3, Not(IsNil))
	c.Check(plug3.Snap, Equals, info)
	c.Check(plug3.Name, Equals, "foo-socket")
	c.Check(plug3.Interface, Equals, "socket")
	c.Check(plug3.Attrs, HasLen, 0)
	c.Check(plug3.Label, Equals, "")
	c.Check(plug3.Apps, DeepEquals, map[string]*snap.AppInfo{app2.Name: app2})

	// plug4 ("logging") is global so it is bound to all apps

	c.Assert(plug4, Not(IsNil))
	c.Check(plug4.Snap, Equals, info)
	c.Check(plug4.Name, Equals, "logging")
	c.Check(plug4.Interface, Equals, "syslog")
	c.Check(plug4.Attrs, HasLen, 0)
	c.Check(plug4.Label, Equals, "")
	c.Check(plug4.Apps, DeepEquals, map[string]*snap.AppInfo{
		app1.Name: app1, app2.Name: app2})

	// slot1 ("foo-socket") is app-bound to "daemon"

	c.Assert(slot1, Not(IsNil))
	c.Check(slot1.Snap, Equals, info)
	c.Check(slot1.Name, Equals, "foo-socket")
	c.Check(slot1.Interface, Equals, "socket")
	c.Check(slot1.Attrs, DeepEquals, map[string]interface{}{
		"protocol": "foo", "path": "$SNAP_DATA/socket"})
	c.Check(slot1.Label, Equals, "")
	c.Check(slot1.Apps, DeepEquals, map[string]*snap.AppInfo{app1.Name: app1})

	// slot2 ("tracing") is global so it is bound to all apps

	c.Assert(slot2, Not(IsNil))
	c.Check(slot2.Snap, Equals, info)
	c.Check(slot2.Name, Equals, "tracing")
	c.Check(slot2.Interface, Equals, "ptrace")
	c.Check(slot2.Attrs, HasLen, 0)
	c.Check(slot2.Label, Equals, "")
	c.Check(slot2.Apps, DeepEquals, map[string]*snap.AppInfo{
		app1.Name: app1, app2.Name: app2})
}

// type and architectures

func (s *YamlSuite) TestSnapYamlTypeDefault(c *C) {
	y := []byte(`name: binary
version: 1.0
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Type, Equals, snap.TypeApp)
}

func (s *YamlSuite) TestSnapYamlMultipleArchitecturesParsing(c *C) {
	y := []byte(`name: binary
version: 1.0
architectures: [i386, armhf]
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Architectures, DeepEquals, []string{"i386", "armhf"})
}

func (s *YamlSuite) TestSnapYamlSingleArchitecturesParsing(c *C) {
	y := []byte(`name: binary
version: 1.0
architectures: [i386]
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Architectures, DeepEquals, []string{"i386"})
}

func (s *YamlSuite) TestSnapYamlAssumesParsing(c *C) {
	y := []byte(`name: binary
version: 1.0
assumes: [feature2, feature1]
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Assumes, DeepEquals, []string{"feature1", "feature2"})
}

func (s *YamlSuite) TestSnapYamlNoArchitecturesParsing(c *C) {
	y := []byte(`name: binary
version: 1.0
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Architectures, DeepEquals, []string{"all"})
}

func (s *YamlSuite) TestSnapYamlBadArchitectureParsing(c *C) {
	y := []byte(`name: binary
version: 1.0
architectures:
  armhf:
    no
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, NotNil)
}

func (s *YamlSuite) TestSnapYamlLicenseParsing(c *C) {
	y := []byte(`
name: foo
version: 1.0
license-agreement: explicit
license-version: 12`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.LicenseAgreement, Equals, "explicit")
	c.Assert(info.LicenseVersion, Equals, "12")
}

// apps

func (s *YamlSuite) TestSimpleAppExample(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 cm:
   command: cm0
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Check(info.Apps, DeepEquals, map[string]*snap.AppInfo{
		"cm": {
			Snap:    info,
			Name:    "cm",
			Command: "cm0",
		},
	})
}

func (s *YamlSuite) TestDaemonEverythingExample(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 svc:
   command: svc1
   description: svc one
   stop-timeout: 25s
   daemon: forking
   stop-command: stop-cmd
   post-stop-command: post-stop-cmd
   restart-condition: on-abnormal
   socket-mode: socket_mode
   listen-stream: listen_stream
   bus-name: busName
   socket: yes
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Check(info.Apps, DeepEquals, map[string]*snap.AppInfo{
		"svc": {
			Snap:            info,
			Name:            "svc",
			Command:         "svc1",
			Daemon:          "forking",
			RestartCond:     systemd.RestartOnAbnormal,
			StopTimeout:     timeout.Timeout(25 * time.Second),
			StopCommand:     "stop-cmd",
			PostStopCommand: "post-stop-cmd",
			Socket:          true,
			SocketMode:      "socket_mode",
			ListenStream:    "listen_stream",
			BusName:         "busName",
		},
	})
}
