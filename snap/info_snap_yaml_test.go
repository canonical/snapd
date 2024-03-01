// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"fmt"
	"regexp"
	"sort"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeout"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type InfoSnapYamlTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&InfoSnapYamlTestSuite{})

var mockYaml = []byte(`name: foo
version: 1.0
type: app
`)

func (s *InfoSnapYamlTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *InfoSnapYamlTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *InfoSnapYamlTestSuite) TestSimple(c *C) {
	info, err := snap.InfoFromSnapYaml(mockYaml)
	c.Assert(err, IsNil)
	c.Assert(info.InstanceName(), Equals, "foo")
	c.Assert(info.Version, Equals, "1.0")
	c.Assert(info.Type(), Equals, snap.TypeApp)
	c.Assert(info.Epoch, DeepEquals, snap.E("0"))
	c.Assert(info.SnapProvenance, Equals, "")
	c.Check(info.Provenance(), Equals, naming.DefaultProvenance)
}

func (s *InfoSnapYamlTestSuite) TestSnapdTypeAddedByMagic(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: snapd
version: 1.0`))
	c.Assert(err, IsNil)
	c.Assert(info.InstanceName(), Equals, "snapd")
	c.Assert(info.Version, Equals, "1.0")
	c.Assert(info.Type(), Equals, snap.TypeSnapd)
}

func (s *InfoSnapYamlTestSuite) TestNonDefaultProvenance(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: foo
provenance: delegated-prov
version: 1.0`))
	c.Assert(err, IsNil)
	c.Check(info.Provenance(), Equals, "delegated-prov")
	c.Check(info.SnapProvenance, Equals, "delegated-prov")
}

func (s *InfoSnapYamlTestSuite) TestFail(c *C) {
	_, err := snap.InfoFromSnapYaml([]byte("random-crap"))
	c.Assert(err, ErrorMatches, "(?m)cannot parse snap.yaml:.*")
}

type YamlSuite struct {
	restore func()
	testutil.BaseTest
}

var _ = Suite(&YamlSuite{})

func (s *YamlSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	hookType := snap.NewHookType(regexp.MustCompile(".*"))
	s.restore = snap.MockSupportedHookTypes([]*snap.HookType{hookType})
}

func (s *YamlSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	s.restore()
}

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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["network-client"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
		Unscoped:  true,
	})
}

func (s *YamlSuite) TestUnmarshalStandalonePlugWithIntAndListAndMap(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    iface:
        interface: complex
        i: 3
        l: [1,2,3]
        m:
          a: A
          b: B
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["iface"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "iface",
		Interface: "complex",
		Attrs: map[string]interface{}{
			"i": int64(3),
			"l": []interface{}{int64(1), int64(2), int64(3)},
			"m": map[string]interface{}{"a": "A", "b": "B"},
		},
		Unscoped: true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Assert(info.Plugs["net"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"attr": int64(2)},
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
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
		Unscoped:  true,
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "app",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},
	})
}

func (s *YamlSuite) TestUnmarshalGlobalPlugBoundToOneApp(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    network-client:
apps:
    with-plug:
        plugs: [network-client]
    without-plug:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 2)

	plug := info.Plugs["network-client"]
	withPlugApp := info.Apps["with-plug"]
	withoutPlugApp := info.Apps["without-plug"]
	c.Assert(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Apps:      map[string]*snap.AppInfo{withPlugApp.Name: withPlugApp},
	})
	c.Assert(withPlugApp, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "with-plug",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},
	})
	c.Assert(withoutPlugApp, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "without-plug",
		Plugs: map[string]*snap.PlugInfo{},
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
	c.Check(info.InstanceName(), Equals, "snap")
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
	c.Check(info.InstanceName(), Equals, "snap")
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Plugs["network-client"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Plugs["bool-file"], DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "bool-file",
		Interface: "bool-file",
		Label:     "Disk I/O indicator",
		Unscoped:  true,
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
	c.Assert(err, ErrorMatches, `plug "net" has attribute key that is not a string \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedPlugWithEmptyAttributeKey(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    net:
        "": ok
`))
	c.Assert(err, ErrorMatches, `plug "net" has an empty attribute key`)
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

func (s *YamlSuite) TestUnmarshalInvalidPlugAttribute(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    serial:
        interface: serial-port
        foo: null
`))
	c.Assert(err, ErrorMatches, `attribute "foo" of plug \"serial\": invalid scalar:.*`)
}

func (s *YamlSuite) TestUnmarshalInvalidAttributeMapKey(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    serial:
        interface: serial-port
        bar:
          baz:
          - 1: A
`))
	c.Assert(err, ErrorMatches, `attribute "bar" of plug \"serial\": non-string key: 1`)
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["network-client"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
		Unscoped:  true,
	})
}

func (s *YamlSuite) TestUnmarshalStandaloneSlotWithIntAndListAndMap(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    iface:
        interface: complex
        i: 3
        l: [1,2]
        m:
          a: "A"
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["iface"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "iface",
		Interface: "complex",
		Attrs: map[string]interface{}{
			"i": int64(3),
			"l": []interface{}{int64(1), int64(2)},
			"m": map[string]interface{}{"a": "A"},
		},
		Unscoped: true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Assert(info.Slots["net"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "net",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"attr": int64(2)},
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
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
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
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
	c.Check(info.InstanceName(), Equals, "snap")
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Slots["network-client"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "network-client",
		Interface: "network-client",
		Attrs:     map[string]interface{}{"ipv6-aware": true},
		Unscoped:  true,
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
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 0)
	c.Assert(info.Slots["led0"], DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "led0",
		Interface: "bool-file",
		Label:     "Front panel LED (red)",
		Unscoped:  true,
	})
}

func (s *YamlSuite) TestUnmarshalGlobalSlotsBindToHooks(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    test-slot:
hooks:
    test-hook:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 1)

	slot, ok := info.Slots["test-slot"]
	c.Assert(ok, Equals, true, Commentf("Expected slots to include 'test-slot'"))
	hook, ok := info.Hooks["test-hook"]
	c.Assert(ok, Equals, true, Commentf("Expected hooks to include 'test-hook'"))

	c.Check(slot, DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "test-slot",
		Interface: "test-slot",
		Unscoped:  true,
	})
	c.Check(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Slots: map[string]*snap.SlotInfo{slot.Name: slot},

		Explicit: true,
	})
}

func (s *YamlSuite) TestUnmarshalHookWithSlot(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
hooks:
    test-hook:
        slots: [test-slot]
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 1)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 1)

	slot, ok := info.Slots["test-slot"]
	c.Assert(ok, Equals, true, Commentf("Expected slots to include 'test-slot'"))
	hook, ok := info.Hooks["test-hook"]
	c.Assert(ok, Equals, true, Commentf("Expected hooks to include 'test-hook'"))

	c.Check(slot, DeepEquals, &snap.SlotInfo{
		Snap:      info,
		Name:      "test-slot",
		Interface: "test-slot",
	})
	c.Check(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Slots: map[string]*snap.SlotInfo{slot.Name: slot},

		Explicit: true,
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
	c.Assert(err, ErrorMatches, `slot "net" has attribute key that is not a string \(found int\)`)
}

func (s *YamlSuite) TestUnmarshalCorruptedSlotWithEmptyAttributeKey(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    net:
        "": ok
`))
	c.Assert(err, ErrorMatches, `slot "net" has an empty attribute key`)
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

func (s *YamlSuite) TestUnmarshalInvalidSlotAttribute(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    serial:
        interface: serial-port
        foo: null
`))
	c.Assert(err, ErrorMatches, `attribute "foo" of slot \"serial\": invalid scalar:.*`)
}

func (s *YamlSuite) TestUnmarshalHook(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
hooks:
    test-hook:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 1)

	hook, ok := info.Hooks["test-hook"]
	c.Assert(ok, Equals, true, Commentf("Expected hooks to include 'test-hook'"))

	c.Check(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Plugs: nil,

		Explicit: true,
	})
}

func (s *YamlSuite) TestUnmarshalUnsupportedHook(c *C) {
	s.restore()
	hookType := snap.NewHookType(regexp.MustCompile("not-test-hook"))
	s.restore = snap.MockSupportedHookTypes([]*snap.HookType{hookType})

	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
hooks:
    test-hook:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 0, Commentf("Expected no hooks to be loaded"))
}

func (s *YamlSuite) TestUnmarshalHookFiltersOutUnsupportedHooks(c *C) {
	s.restore()
	hookType := snap.NewHookType(regexp.MustCompile("test-.*"))
	s.restore = snap.MockSupportedHookTypes([]*snap.HookType{hookType})

	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
hooks:
    test-hook:
    foo-hook:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 1)

	hook, ok := info.Hooks["test-hook"]
	c.Assert(ok, Equals, true, Commentf("Expected hooks to include 'test-hook'"))

	c.Check(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Plugs: nil,

		Explicit: true,
	})
}

func (s *YamlSuite) TestUnmarshalHookWithPlug(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
hooks:
    test-hook:
        plugs: [test-plug]
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 1)

	plug, ok := info.Plugs["test-plug"]
	c.Assert(ok, Equals, true, Commentf("Expected plugs to include 'test-plug'"))
	hook, ok := info.Hooks["test-hook"]
	c.Assert(ok, Equals, true, Commentf("Expected hooks to include 'test-hook'"))

	c.Check(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "test-plug",
		Interface: "test-plug",
	})
	c.Check(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},

		Explicit: true,
	})
}

func (s *YamlSuite) TestUnmarshalGlobalPlugsBindToHooks(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    test-plug:
hooks:
    test-hook:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 1)

	plug, ok := info.Plugs["test-plug"]
	c.Assert(ok, Equals, true, Commentf("Expected plugs to include 'test-plug'"))
	hook, ok := info.Hooks["test-hook"]
	c.Assert(ok, Equals, true, Commentf("Expected hooks to include 'test-hook'"))

	c.Check(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "test-plug",
		Interface: "test-plug",
		Unscoped:  true,
	})
	c.Check(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},

		Explicit: true,
	})
}

func (s *YamlSuite) TestUnmarshalGlobalPlugBoundToOneHook(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    test-plug:
hooks:
    with-plug:
        plugs: [test-plug]
    without-plug:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 2)

	plug := info.Plugs["test-plug"]
	withPlugHook := info.Hooks["with-plug"]
	withoutPlugHook := info.Hooks["without-plug"]
	c.Assert(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "test-plug",
		Interface: "test-plug",
	})
	c.Assert(withPlugHook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "with-plug",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},

		Explicit: true,
	})
	c.Assert(withoutPlugHook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "without-plug",
		Plugs: map[string]*snap.PlugInfo{},

		Explicit: true,
	})
}

func (s *YamlSuite) TestUnmarshalExplicitGlobalPlugBoundToHook(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    test-plug: test-interface
hooks:
    test-hook:
        plugs: ["test-plug"]
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 0)
	c.Check(info.Hooks, HasLen, 1)

	plug, ok := info.Plugs["test-plug"]
	c.Assert(ok, Equals, true, Commentf("Expected plugs to include 'test-plug'"))
	hook, ok := info.Hooks["test-hook"]
	c.Assert(ok, Equals, true, Commentf("Expected hooks to include 'test-hook'"))

	c.Check(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "test-plug",
		Interface: "test-interface",
	})
	c.Check(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},

		Explicit: true,
	})
}

func (s *YamlSuite) TestUnmarshalGlobalPlugBoundToHookNotApp(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
plugs:
    test-plug:
hooks:
    test-hook:
        plugs: [test-plug]
apps:
    test-app:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 1)
	c.Check(info.Slots, HasLen, 0)
	c.Check(info.Apps, HasLen, 1)
	c.Check(info.Hooks, HasLen, 1)

	plug := info.Plugs["test-plug"]
	hook := info.Hooks["test-hook"]
	app := info.Apps["test-app"]
	c.Assert(plug, DeepEquals, &snap.PlugInfo{
		Snap:      info,
		Name:      "test-plug",
		Interface: "test-plug",
		Apps:      map[string]*snap.AppInfo{},
	})
	c.Assert(hook, DeepEquals, &snap.HookInfo{
		Snap:  info,
		Name:  "test-hook",
		Plugs: map[string]*snap.PlugInfo{plug.Name: plug},

		Explicit: true,
	})
	c.Assert(app, DeepEquals, &snap.AppInfo{
		Snap:  info,
		Name:  "test-app",
		Plugs: map[string]*snap.PlugInfo{},
	})
}

func (s *YamlSuite) TestUnmarshalComplexExample(c *C) {
	// NOTE: yaml content cannot use tabs, indent the section with spaces.
	info, err := snap.InfoFromSnapYaml([]byte(`
name: foo
version: 1.2
title: Foo
summary: foo app
type: app
epoch: 1*
confinement: devmode
license: GPL-3.0
description: |
    Foo provides useful services
apps:
    daemon:
       command: foo --daemon
       plugs: [network, network-bind]
       slots: [foo-socket-slot]
    foo:
       command: fooctl
       plugs: [foo-socket-plug]
hooks:
    test-hook:
       plugs: [foo-socket-plug]
       slots: [foo-socket-slot]
plugs:
    foo-socket-plug:
        interface: socket
        # $protocol: foo
    logging:
        interface: syslog
slots:
    foo-socket-slot:
        interface: socket
        path: $SNAP_DATA/socket
        protocol: foo
    tracing:
        interface: ptrace
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "foo")
	c.Check(info.Version, Equals, "1.2")
	c.Check(info.Type(), Equals, snap.TypeApp)
	c.Check(info.Epoch, DeepEquals, snap.E("1*"))
	c.Check(info.Confinement, Equals, snap.DevModeConfinement)
	c.Check(info.Title(), Equals, "Foo")
	c.Check(info.Summary(), Equals, "foo app")
	c.Check(info.Description(), Equals, "Foo provides useful services\n")
	c.Check(info.Apps, HasLen, 2)
	c.Check(info.Plugs, HasLen, 4)
	c.Check(info.Slots, HasLen, 2)
	// these don't come from snap.yaml
	c.Check(info.Publisher, Equals, snap.StoreAccount{})
	c.Check(info.Channel, Equals, "")
	c.Check(info.License, Equals, "GPL-3.0")

	app1 := info.Apps["daemon"]
	app2 := info.Apps["foo"]
	hook := info.Hooks["test-hook"]
	plug1 := info.Plugs["network"]
	plug2 := info.Plugs["network-bind"]
	plug3 := info.Plugs["foo-socket-plug"]
	plug4 := info.Plugs["logging"]
	slot1 := info.Slots["foo-socket-slot"]
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

	// hook1 has two plugs ("foo-socket", "logging") and two slots ("foo-socket", "tracing").
	// The plug "logging" and slot "tracing" are global while "foo-socket" is hook-bound.

	c.Assert(hook, NotNil)
	c.Check(hook.Snap, Equals, info)
	c.Check(hook.Name, Equals, "test-hook")
	c.Check(hook.Plugs, DeepEquals, map[string]*snap.PlugInfo{
		plug3.Name: plug3, plug4.Name: plug4})
	c.Check(hook.Slots, DeepEquals, map[string]*snap.SlotInfo{
		slot1.Name: slot1, slot2.Name: slot2})

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
	c.Check(plug3.Name, Equals, "foo-socket-plug")
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
	c.Check(slot1.Name, Equals, "foo-socket-slot")
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

func (s *YamlSuite) TestUnmarshalActivatesOn(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
slots:
    test-slot1:
    test-slot2:
apps:
    daemon:
        activates-on: ["test-slot1", "test-slot2"]
    foo:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Plugs, HasLen, 0)
	c.Check(info.Slots, HasLen, 2)
	c.Check(info.Apps, HasLen, 2)
	c.Check(info.Hooks, HasLen, 0)

	app1 := info.Apps["daemon"]
	app2 := info.Apps["foo"]
	slot1 := info.Slots["test-slot1"]
	slot2 := info.Slots["test-slot2"]

	c.Assert(app1, Not(IsNil))
	c.Check(app1.Name, Equals, "daemon")
	c.Check(app1.ActivatesOn, DeepEquals, []*snap.SlotInfo{slot1, slot2})
	// activates-on slots are implicitly added to the app
	c.Check(app1.Slots, DeepEquals, map[string]*snap.SlotInfo{
		slot1.Name: slot1, slot2.Name: slot2})

	c.Assert(app2, Not(IsNil))
	c.Check(app2.Name, Equals, "foo")
	c.Check(app2.ActivatesOn, HasLen, 0)
	// As slot has been bound to app1, it isn't implicitly applied here
	c.Check(app2.Slots, HasLen, 0)

	c.Assert(slot1, Not(IsNil))
	c.Check(slot1.Name, Equals, "test-slot1")
	c.Check(slot1.Apps, DeepEquals, map[string]*snap.AppInfo{
		app1.Name: app1})

	c.Assert(slot2, Not(IsNil))
	c.Check(slot2.Name, Equals, "test-slot2")
	c.Check(slot2.Apps, DeepEquals, map[string]*snap.AppInfo{
		app1.Name: app1})
}

func (s *YamlSuite) TestUnmarshalActivatesOnUnknownSlot(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
apps:
    daemon:
        activates-on: ["test-slot"]
`))
	c.Check(info, IsNil)
	c.Check(err, ErrorMatches, `invalid activates-on value "test-slot" on app "daemon": slot not found`)
}

// type and architectures

func (s *YamlSuite) TestSnapYamlTypeDefault(c *C) {
	y := []byte(`name: binary
version: 1.0
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Type(), Equals, snap.TypeApp)
}

func (s *YamlSuite) TestSnapYamlEpochDefault(c *C) {
	y := []byte(`name: binary
version: 1.0
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Epoch, DeepEquals, snap.E("0"))
}

func (s *YamlSuite) TestSnapYamlConfinementDefault(c *C) {
	y := []byte(`name: binary
version: 1.0
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Confinement, Equals, snap.StrictConfinement)
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
   start-timeout: 42m
   daemon: forking
   daemon-scope: system
   stop-command: stop-cmd
   post-stop-command: post-stop-cmd
   restart-condition: on-abnormal
   bus-name: busName
   sockets:
     sock1:
       listen-stream: $SNAP_DATA/sock1.socket
       socket-mode: 0666
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)

	app := snap.AppInfo{
		Snap:            info,
		Name:            "svc",
		Command:         "svc1",
		Daemon:          "forking",
		DaemonScope:     snap.SystemDaemon,
		RestartCond:     snap.RestartOnAbnormal,
		StopTimeout:     timeout.Timeout(25 * time.Second),
		StartTimeout:    timeout.Timeout(42 * time.Minute),
		StopCommand:     "stop-cmd",
		PostStopCommand: "post-stop-cmd",
		BusName:         "busName",
		Sockets:         map[string]*snap.SocketInfo{},
	}

	app.Sockets["sock1"] = &snap.SocketInfo{
		App:          &app,
		Name:         "sock1",
		ListenStream: "$SNAP_DATA/sock1.socket",
		SocketMode:   0666,
	}

	c.Check(info.Apps, DeepEquals, map[string]*snap.AppInfo{"svc": &app})
}

func (s *YamlSuite) TestDaemonUserDaemon(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 svc:
   command: svc1
   daemon: simple
   daemon-scope: user
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Check(info.Apps["svc"].DaemonScope, Equals, snap.UserDaemon)
}

func (s *YamlSuite) TestDaemonNoDaemonScope(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 svc:
   command: svc1
   daemon: simple
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)

	// If daemon-scope is unset, default to system scope
	c.Check(info.Apps["svc"].DaemonScope, Equals, snap.SystemDaemon)
}

func (s *YamlSuite) TestDaemonListenStreamAsInteger(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 svc:
   command: svc
   sockets:
     sock:
       listen-stream: 8080
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)

	app := snap.AppInfo{
		Snap:    info,
		Name:    "svc",
		Command: "svc",
		Sockets: map[string]*snap.SocketInfo{},
	}

	app.Sockets["sock"] = &snap.SocketInfo{
		App:          &app,
		Name:         "sock",
		ListenStream: "8080",
	}

	c.Check(info.Apps, DeepEquals, map[string]*snap.AppInfo{
		"svc": &app,
	})
}

func (s *YamlSuite) TestDaemonInvalidSocketMode(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 svc:
   command: svc
   sockets:
     sock:
       listen-stream: 8080
       socket-mode: asdfasdf
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Check(err.Error(), Matches, "cannot parse snap.yaml: yaml: unmarshal errors:\n"+
		"  line 9: cannot unmarshal !!str `asdfasdf` into (os|fs).FileMode")
}

func (s *YamlSuite) TestDaemonInvalidDaemonScope(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 svc:
   command: svc
   daemon-scope: invalid
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Check(err.Error(), Equals, "cannot parse snap.yaml: invalid daemon scope: \"invalid\"")
}

func (s *YamlSuite) TestSnapYamlGlobalEnvironment(c *C) {
	y := []byte(`
name: foo
version: 1.0
environment:
 foo: bar
 baz: boom
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Environment, DeepEquals, *strutil.NewOrderedMap("foo", "bar", "baz", "boom"))
}

func (s *YamlSuite) TestSnapYamlPerAppEnvironment(c *C) {
	y := []byte(`
name: foo
version: 1.0
apps:
 foo:
  environment:
   k1: v1
   k2: v2
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Apps["foo"].Environment, DeepEquals, *strutil.NewOrderedMap("k1", "v1", "k2", "v2"))
}

func (s *YamlSuite) TestSnapYamlPerHookEnvironment(c *C) {
	y := []byte(`
name: foo
version: 1.0
hooks:
 foo:
  environment:
   k1: v1
   k2: v2
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Hooks["foo"].Environment, DeepEquals, *strutil.NewOrderedMap("k1", "v1", "k2", "v2"))
}

// classic confinement
func (s *YamlSuite) TestClassicConfinement(c *C) {
	y := []byte(`
name: foo
confinement: classic
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Confinement, Equals, snap.ClassicConfinement)
}

func (s *YamlSuite) TestSnapYamlAliases(c *C) {
	y := []byte(`
name: foo
version: 1.0
apps:
  foo:
    aliases: [foo]
  bar:
    aliases: [bar, bar1]
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)

	c.Check(info.Apps["foo"].LegacyAliases, DeepEquals, []string{"foo"})
	c.Check(info.Apps["bar"].LegacyAliases, DeepEquals, []string{"bar", "bar1"})

	c.Check(info.LegacyAliases, DeepEquals, map[string]*snap.AppInfo{
		"foo":  info.Apps["foo"],
		"bar":  info.Apps["bar"],
		"bar1": info.Apps["bar"],
	})
}

func (s *YamlSuite) TestSnapYamlAliasesConflict(c *C) {
	y := []byte(`
name: foo
version: 1.0
apps:
  foo:
    aliases: [bar]
  bar:
    aliases: [bar]
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `cannot set "bar" as alias for both ("foo" and "bar"|"bar" and "foo")`)
}

func (s *YamlSuite) TestSnapYamlAppStartOrder(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 foo:
   after: [bar, zed]
 bar:
   before: [foo]
 baz:
   after: [foo]
 zed:

`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)

	c.Check(info.Apps, DeepEquals, map[string]*snap.AppInfo{
		"foo": {
			Snap:  info,
			Name:  "foo",
			After: []string{"bar", "zed"},
		},
		"bar": {
			Snap:   info,
			Name:   "bar",
			Before: []string{"foo"},
		},
		"baz": {
			Snap:  info,
			Name:  "baz",
			After: []string{"foo"},
		},
		"zed": {
			Snap: info,
			Name: "zed",
		},
	})
}

func (s *YamlSuite) TestSnapYamlWatchdog(c *C) {
	y := []byte(`
name: foo
version: 1.0
apps:
  foo:
    watchdog-timeout: 12s
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)

	c.Check(info.Apps["foo"].WatchdogTimeout, Equals, timeout.Timeout(12*time.Second))
}

func (s *YamlSuite) TestLayout(c *C) {
	y := []byte(`
name: foo
version: 1.0
layout:
  /usr/share/foo:
    bind: $SNAP/usr/share/foo
  /usr/share/bar:
    symlink: $SNAP/usr/share/bar
  /etc/froz:
    bind-file: $SNAP/etc/froz
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Assert(info.Layout["/usr/share/foo"], DeepEquals, &snap.Layout{
		Snap:  info,
		Path:  "/usr/share/foo",
		Bind:  "$SNAP/usr/share/foo",
		User:  "root",
		Group: "root",
		Mode:  0755,
	})
	c.Assert(info.Layout["/usr/share/bar"], DeepEquals, &snap.Layout{
		Snap:    info,
		Path:    "/usr/share/bar",
		Symlink: "$SNAP/usr/share/bar",
		User:    "root",
		Group:   "root",
		Mode:    0755,
	})
	c.Assert(info.Layout["/etc/froz"], DeepEquals, &snap.Layout{
		Snap:     info,
		Path:     "/etc/froz",
		BindFile: "$SNAP/etc/froz",
		User:     "root",
		Group:    "root",
		Mode:     0755,
	})
}

func (s *YamlSuite) TestLayoutsWithTypo(c *C) {
	y := []byte(`
name: foo
version: 1.0
layouts:
  /usr/share/foo:
    bind: $SNAP/usr/share/foo
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `cannot parse snap.yaml: typo detected: use singular "layout" instead of plural "layouts"`)
	c.Assert(info, IsNil)
}

func (s *YamlSuite) TestSnapYamlAppTimer(c *C) {
	y := []byte(`name: wat
version: 42
apps:
 foo:
   daemon: oneshot
   timer: mon,10:00-12:00

`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	app := info.Apps["foo"]
	c.Check(app.Timer, DeepEquals, &snap.TimerInfo{App: app, Timer: "mon,10:00-12:00"})
}

func (s *YamlSuite) TestSnapYamlAppAutostart(c *C) {
	yAutostart := []byte(`name: wat
version: 42
apps:
 foo:
   command: bin/foo
   autostart: foo.desktop

`)
	info, err := snap.InfoFromSnapYaml(yAutostart)
	c.Assert(err, IsNil)
	app := info.Apps["foo"]
	c.Check(app.Autostart, Equals, "foo.desktop")

	yNoAutostart := []byte(`name: wat
version: 42
apps:
 foo:
   command: bin/foo

`)
	info, err = snap.InfoFromSnapYaml(yNoAutostart)
	c.Assert(err, IsNil)
	app = info.Apps["foo"]
	c.Check(app.Autostart, Equals, "")
}

func (s *YamlSuite) TestSnapYamlAppCommonID(c *C) {
	yAutostart := []byte(`name: wat
version: 42
apps:
 foo:
   command: bin/foo
   common-id: org.foo
 bar:
   command: bin/foo
   common-id: org.bar
 baz:
   command: bin/foo

`)
	info, err := snap.InfoFromSnapYaml(yAutostart)
	c.Assert(err, IsNil)
	c.Check(info.Apps["foo"].CommonID, Equals, "org.foo")
	c.Check(info.Apps["bar"].CommonID, Equals, "org.bar")
	c.Check(info.Apps["baz"].CommonID, Equals, "")
	c.Assert(info.CommonIDs, HasLen, 2)
	c.Assert((info.CommonIDs[0] == "org.foo" && info.CommonIDs[1] == "org.bar") ||
		(info.CommonIDs[1] == "org.foo" && info.CommonIDs[0] == "org.bar"),
		Equals,
		true)
}

func (s *YamlSuite) TestSnapYamlCommandChain(c *C) {
	yAutostart := []byte(`name: wat
version: 42
apps:
 foo:
  command: bin/foo
  command-chain: [chain1, chain2]
hooks:
 configure:
  command-chain: [hookchain1, hookchain2]
`)
	info, err := snap.InfoFromSnapYaml(yAutostart)
	c.Assert(err, IsNil)
	app := info.Apps["foo"]
	c.Check(app.CommandChain, DeepEquals, []string{"chain1", "chain2"})
	hook := info.Hooks["configure"]
	c.Check(hook.CommandChain, DeepEquals, []string{"hookchain1", "hookchain2"})
}

func (s *YamlSuite) TestSnapYamlRestartDelay(c *C) {
	yAutostart := []byte(`name: wat
version: 42
apps:
 foo:
  command: bin/foo
  daemon: simple
  restart-delay: 12s
`)
	info, err := snap.InfoFromSnapYaml(yAutostart)
	c.Assert(err, IsNil)
	app := info.Apps["foo"]
	c.Assert(app, NotNil)
	c.Check(app.RestartDelay, Equals, timeout.Timeout(12*time.Second))
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsing(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  foo: shared
  bar:
    scope: external
  baz:
    scope: private
    attr1: norf
    attr2: corge
    attr3: ""
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, IsNil)
	c.Check(info.SystemUsernames, HasLen, 3)
	c.Assert(info.SystemUsernames["foo"], DeepEquals, &snap.SystemUsernameInfo{
		Name:  "foo",
		Scope: "shared",
	})
	c.Assert(info.SystemUsernames["bar"], DeepEquals, &snap.SystemUsernameInfo{
		Name:  "bar",
		Scope: "external",
	})
	c.Assert(info.SystemUsernames["baz"], DeepEquals, &snap.SystemUsernameInfo{
		Name:  "baz",
		Scope: "private",
		Attrs: map[string]interface{}{
			"attr1": "norf",
			"attr2": "corge",
			"attr3": "",
		},
	})
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadType(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  a: true
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `system username "a" has malformed definition \(found bool\)`)
	c.Assert(info, IsNil)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadValue(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  a: [b, c]
`)
	info, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `system username "a" has malformed definition \(found \[\]interface {}\)`)
	c.Assert(info, IsNil)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadKeyEmpty(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  "": shared
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `system username cannot be empty`)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadKeyList(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
- foo: shared
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `(?m)cannot parse snap.yaml:.*`)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadValueEmpty(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  a: ""
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `system username "a" does not specify a scope`)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadValueNull(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  a: null
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `system username "a" does not specify a scope`)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadAttrKeyEmpty(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  foo:
    scope: shared
    "": bar
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `system username "foo" has an empty attribute key`)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadAttrKeyNonString(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  foo:
    scope: shared
    1: bar
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `system username "foo" has attribute key that is not a string \(found int\)`)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadAttrValue(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  foo:
    scope: shared
    bar: null
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `attribute "bar" of system username "foo": invalid scalar:.*`)
}

func (s *YamlSuite) TestSnapYamlSystemUsernamesParsingBadScopeNonString(c *C) {
	y := []byte(`name: binary
version: 1.0
system-usernames:
  foo:
    scope: 10
`)
	_, err := snap.InfoFromSnapYaml(y)
	c.Assert(err, ErrorMatches, `scope on system username "foo" is not a string \(found int\)`)
}

func (s *YamlSuite) TestSnapYamlLinks(c *C) {
	yLinks := []byte(`name: my-snap
version: 1.0

links:
 donations:
   - https://donate.me
 contact:
   - mailto:me@toto.space
   - https://toto.space
 bug-url:
   - https://github.com/webteam-space/toto.space/issues
 website:
   - https://toto.space
 source-code:
   - https://github.com/webteam-space/toto.space
`)
	info, err := snap.InfoFromSnapYaml(yLinks)
	c.Assert(err, IsNil)
	c.Check(info.Links(), DeepEquals, map[string][]string{
		"bug-url":     {"https://github.com/webteam-space/toto.space/issues"},
		"contact":     {"mailto:me@toto.space", "https://toto.space"},
		"donations":   {"https://donate.me"},
		"source-code": {"https://github.com/webteam-space/toto.space"},
		"website":     {"https://toto.space"},
	})
	c.Check(info.Contact(), Equals, "mailto:me@toto.space")
}

func (s *YamlSuite) TestSnapYamlEmptyLinksKey(c *C) {
	yLinks := []byte(`name: my-snap
version: 1.0

links:
 "":
   - htps://toto.space
`)
	_, err := snap.InfoFromSnapYaml(yLinks)
	c.Check(err, ErrorMatches, `links key cannot be empty`)
}

func (s *YamlSuite) TestSnapYamlInvalidLinksKey(c *C) {
	yLinks := `name: my-snap
version: 1.0

links:
 %s:
    - link.website
`

	invalid := []string{
		"--",
		"1-2",
		"aa-",
	}

	for _, k := range invalid {
		_, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(yLinks, k)))
		c.Check(err, ErrorMatches, fmt.Sprintf(`links key is invalid: %s`, k))
	}
}

func (s *YamlSuite) TestUnmarshalComponents(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
components:
  test1:
    type: test
    summary: test component
    description: long component description
  test2:
    type: test
    summary: test component 2
    description: long component description 2
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")
	c.Check(info.Components, DeepEquals, map[string]*snap.Component{
		"test1": {
			Type:        "test",
			Summary:     "test component",
			Description: "long component description",
			Name:        "test1",
		},
		"test2": {
			Type:        "test",
			Summary:     "test component 2",
			Description: "long component description 2",
			Name:        "test2",
		},
	})
}

func (s *YamlSuite) TestUnmarshalComponentsHook(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
components:
  test1:
    type: test
    summary: test component
    description: long component description
    hooks:
      install:
        plugs: [network]
        command-chain: [chain1, chain2]
        environment:
          k1: v1
          k2: v2
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")

	component := info.Components["test1"]
	c.Assert(component, NotNil)

	hook := component.ExplicitHooks["install"]
	c.Assert(hook, NotNil)
	c.Check(hook.CommandChain, DeepEquals, []string{"chain1", "chain2"})
	c.Check(hook.Environment, DeepEquals, *strutil.NewOrderedMap("k1", "v1", "k2", "v2"))
}

func (s *YamlSuite) TestUnmarshalComponentsHooksWithPlugs(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
components:
  test1:
    type: test
    summary: test component
    description: long component description
    hooks:
      install:
        plugs: [network, network-bind]
      pre-refresh:
        plugs: [test-plug]
      post-refresh:
      remove:
  test2:
    hooks:
      pre-refresh:
        plugs: [test-plug, test2-plug]
plugs:
  network-client:
`))
	c.Assert(err, IsNil)
	c.Check(info.InstanceName(), Equals, "snap")

	type hook struct {
		plugs []string
	}

	type component struct {
		hooks map[string]hook
	}

	expectedComponents := map[string]component{
		"test1": {
			hooks: map[string]hook{
				"install": {
					plugs: []string{"network", "network-bind", "network-client"},
				},
				"pre-refresh": {
					plugs: []string{"network-client", "test-plug"},
				},
				"post-refresh": {
					plugs: []string{"network-client"},
				},
				"remove": {
					plugs: []string{"network-client"},
				},
			},
		},
		"test2": {
			hooks: map[string]hook{
				"pre-refresh": {
					plugs: []string{"network-client", "test-plug", "test2-plug"},
				},
			},
		},
	}

	foundComponents := make(map[string]component, len(info.Components))
	for _, foundComponent := range info.Components {
		foundHooks := make(map[string]hook)
		for _, foundHook := range foundComponent.ExplicitHooks {
			c.Check(foundHook.Explicit, Equals, true)

			foundPlugs := make([]string, 0, len(foundHook.Plugs))
			for _, foundPlug := range foundHook.Plugs {
				foundPlugs = append(foundPlugs, foundPlug.Name)
			}

			sort.Strings(foundPlugs)

			foundHooks[foundHook.Name] = hook{
				plugs: foundPlugs,
			}
		}

		foundComponents[foundComponent.Name] = component{
			hooks: foundHooks,
		}
	}

	c.Check(foundComponents, DeepEquals, expectedComponents)
}

func (s *YamlSuite) TestUnmarshalComponentsHooksUnsupported(c *C) {
	_, err := snap.InfoFromSnapYaml([]byte(`
name: snap
components:
  test1:
    type: test
    summary: test component
    description: long component description
    hooks:
      unsupported:
        plugs: [plug-for-unsupported]
plugs:
  network-client:
`))
	c.Assert(err, ErrorMatches, `unsupported component hook: "unsupported"`)
}

func (s *YamlSuite) TestUnmarshalComponentsError(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
components:
  test1:
    type: badtype
`))
	c.Assert(err.Error(), Equals, `cannot parse snap.yaml: unknown component type "badtype"`)
	c.Assert(info, IsNil)
}

func (s *YamlSuite) TestUnmarshalComponentsHookSlotsError(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`
name: snap
components:
  test1:
    type: test
    hooks:
      install:
        slots: [slot-for-install]
`))
	c.Assert(err.Error(), Equals, `component hooks cannot have slots`)
	c.Assert(info, IsNil)
}
