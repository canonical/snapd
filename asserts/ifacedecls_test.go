// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package asserts_test

import (
	"fmt"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
)

var (
	_ = Suite(&attrConstraintsSuite{})
	_ = Suite(&plugSlotRulesSuite{})
)

type attrConstraintsSuite struct{}

func attrs(yml string) (r map[string]interface{}) {
	err := yaml.Unmarshal([]byte(yml), &r)
	if err != nil {
		panic(err)
	}
	return
}

func (s *attrConstraintsSuite) TestSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar: BAR`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	})
	c.Check(err, IsNil)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	})
	c.Check(err, ErrorMatches, `attribute "bar" value "BAZ" does not match \^\(BAR\)\$`)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"baz": "BAZ",
	})
	c.Check(err, ErrorMatches, `attribute "bar" has constraints but is unset`)
}

func (s *attrConstraintsSuite) TestSimpleAnchorsVsRegexpAlt(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  bar: BAR|BAZ`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(map[string]interface{}{
		"bar": "BAR",
	})
	c.Check(err, IsNil)

	err = cstrs.Check(map[string]interface{}{
		"bar": "BARR",
	})
	c.Check(err, ErrorMatches, `attribute "bar" value "BARR" does not match \^\(BAR|BAZ\)\$`)

	err = cstrs.Check(map[string]interface{}{
		"bar": "BBAZ",
	})
	c.Check(err, ErrorMatches, `attribute "bar" value "BAZZ" does not match \^\(BAR|BAZ\)\$`)

	err = cstrs.Check(map[string]interface{}{
		"bar": "BABAZ",
	})
	c.Check(err, ErrorMatches, `attribute "bar" value "BABAZ" does not match \^\(BAR|BAZ\)\$`)

	err = cstrs.Check(map[string]interface{}{
		"bar": "BARAZ",
	})
	c.Check(err, ErrorMatches, `attribute "bar" value "BARAZ" does not match \^\(BAR|BAZ\)\$`)
}

func (s *attrConstraintsSuite) TestNested(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar:
    bar1: BAR1
    bar2: BAR2`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR2
  bar3: BAR3
baz: BAZ
`))
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar: BAZ
baz: BAZ
`))
	c.Check(err, ErrorMatches, `attribute "bar" must be a map`)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
  bar3: BAR3
baz: BAZ
`))
	c.Check(err, ErrorMatches, `attribute "bar\.bar2" value "BAR22" does not match \^\(BAR2\)\$`)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2:
    bar22: true
  bar3: BAR3
baz: BAZ
`))
	c.Check(err, ErrorMatches, `attribute "bar\.bar2" must be a scalar or list`)
}

func (s *attrConstraintsSuite) TestAlternative(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  -
    foo: FOO
    bar: BAR
  -
    foo: FOO
    bar: BAZ`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].([]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	})
	c.Check(err, IsNil)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	})
	c.Check(err, IsNil)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"bar": "BARR",
		"baz": "BAR",
	})
	c.Check(err, ErrorMatches, `no alternative matches: attribute "bar" value "BARR" does not match \^\(BAR\)\$`)
}

func (s *attrConstraintsSuite) TestNestedAlternative(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar:
    bar1: BAR1
    bar2:
      - BAR2
      - BAR22`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR2
`))
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
`))
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR3
`))
	c.Check(err, ErrorMatches, `no alternative for attribute "bar\.bar2" matches: attribute "bar\.bar2" value "BAR3" does not match \^\(BAR2\)\$`)
}

func (s *attrConstraintsSuite) TestOtherScalars(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: 1
  bar: true`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: 1
bar: true
`))
	c.Check(err, IsNil)
}

func (s *attrConstraintsSuite) TestCompileErrors(c *C) {
	_, err := asserts.CompileAttributeConstraints(map[string]interface{}{
		"foo": "[",
	})
	c.Check(err, ErrorMatches, `cannot compile "foo" constraint "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttributeConstraints(map[string]interface{}{
		"foo": []interface{}{"foo", "["},
	})
	c.Check(err, ErrorMatches, `cannot compile "foo/alt#2/" constraint "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttributeConstraints(map[string]interface{}{
		"foo": []interface{}{"foo", []interface{}{"bar", "baz"}},
	})
	c.Check(err, ErrorMatches, `cannot nest alternative constraints directly at "foo/alt#2/"`)

	_, err = asserts.CompileAttributeConstraints("FOO")
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)

	_, err = asserts.CompileAttributeConstraints([]interface{}{"FOO"})
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)
}

func (s *attrConstraintsSuite) TestMatchingListsSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: /foo/.*`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: ["/foo/x", "/foo/y"]
`))
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: ["/foo/x", "/foo"]
`))
	c.Check(err, ErrorMatches, `attribute "foo\.1" value "/foo" does not match \^\(/foo/\.\*\)\$`)
}

func (s *attrConstraintsSuite) TestMatchingListsMap(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo:
    p: /foo/.*`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: [{p: "/foo/x"}, {p: "/foo/y"}]
`))
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: [{p: "zzz"}, {p: "/foo/y"}]
`))
	c.Check(err, ErrorMatches, `attribute "foo\.0\.p" value "zzz" does not match \^\(/foo/\.\*\)\$`)
}

func (s *attrConstraintsSuite) TestAlwaysMatchAttributeConstraints(c *C) {
	c.Check(asserts.AlwaysMatchAttributes.Check(nil), IsNil)
}

func (s *attrConstraintsSuite) TestNeverMatchAttributeConstraints(c *C) {
	c.Check(asserts.NeverMatchAttributes.Check(nil), NotNil)
}

type plugSlotRulesSuite struct{}

func checkAttrs(c *C, attrs *asserts.AttributeConstraints, witness string) {
	c.Check(attrs.Check(map[string]interface{}{
		witness: "XYZ",
	}), ErrorMatches, fmt.Sprintf(`attribute "%s".*does not match.*`, witness))
}

func checkBoolPlugConnConstraints(c *C, cstrs *asserts.PlugConnectionConstraints, always bool) {
	expected := asserts.NeverMatchAttributes
	if always {
		expected = asserts.AlwaysMatchAttributes
	}
	c.Assert(cstrs, NotNil)
	c.Check(cstrs.PlugAttributes, Equals, expected)
	c.Check(cstrs.SlotAttributes, Equals, expected)
	c.Check(cstrs.SlotSnapIDs, HasLen, 0)
	c.Check(cstrs.SlotPublisherIDs, HasLen, 0)
	c.Check(cstrs.SlotSnapTypes, HasLen, 0)
}

func checkBoolSlotConnConstraints(c *C, cstrs *asserts.SlotConnectionConstraints, always bool) {
	expected := asserts.NeverMatchAttributes
	if always {
		expected = asserts.AlwaysMatchAttributes
	}
	c.Assert(cstrs, NotNil)
	c.Check(cstrs.PlugAttributes, Equals, expected)
	c.Check(cstrs.SlotAttributes, Equals, expected)
	c.Check(cstrs.PlugSnapIDs, HasLen, 0)
	c.Check(cstrs.PlugPublisherIDs, HasLen, 0)
	c.Check(cstrs.PlugSnapTypes, HasLen, 0)
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleAllAllowDenyStanzas(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    plug-attributes:
      a1: A1
  deny-installation:
    plug-attributes:
      a2: A2
  allow-connection:
    plug-attributes:
      pa3: PA3
    slot-attributes:
      sa3: SA3
  deny-connection:
    plug-attributes:
      pa4: PA5
    slot-attributes:
      sa4: SA4
  allow-auto-connection:
    plug-attributes:
      pa5: PA5
    slot-attributes:
      sa5: SA5
  deny-auto-connection:
    plug-attributes:
      pa6: PA6
    slot-attributes:
      sa6: SA6`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	checkAttrs(c, rule.AllowInstallation.PlugAttributes, "a1")
	c.Assert(rule.DenyInstallation, NotNil)
	checkAttrs(c, rule.DenyInstallation.PlugAttributes, "a2")
	// connection subrules
	c.Assert(rule.AllowConnection, NotNil)
	checkAttrs(c, rule.AllowConnection.PlugAttributes, "pa3")
	checkAttrs(c, rule.AllowConnection.SlotAttributes, "sa3")
	c.Assert(rule.DenyConnection, NotNil)
	checkAttrs(c, rule.DenyConnection.PlugAttributes, "pa4")
	checkAttrs(c, rule.DenyConnection.SlotAttributes, "sa4")
	// auto-connection subrules
	c.Assert(rule.AllowAutoConnection, NotNil)
	checkAttrs(c, rule.AllowAutoConnection.PlugAttributes, "pa5")
	checkAttrs(c, rule.AllowAutoConnection.SlotAttributes, "sa5")
	c.Assert(rule.DenyAutoConnection, NotNil)
	checkAttrs(c, rule.DenyAutoConnection.PlugAttributes, "pa6")
	checkAttrs(c, rule.DenyAutoConnection.SlotAttributes, "sa6")
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleShortcutTrue(c *C) {
	rule, err := asserts.CompilePlugRule("iface", "true")
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	c.Check(rule.AllowInstallation.PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, NotNil)
	c.Check(rule.DenyInstallation.PlugAttributes, Equals, asserts.NeverMatchAttributes)
	// connection subrules
	checkBoolPlugConnConstraints(c, rule.AllowConnection, true)
	checkBoolPlugConnConstraints(c, rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolPlugConnConstraints(c, rule.AllowAutoConnection, true)
	checkBoolPlugConnConstraints(c, rule.DenyAutoConnection, false)
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleShortcutFalse(c *C) {
	rule, err := asserts.CompilePlugRule("iface", "false")
	c.Assert(err, IsNil)

	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	c.Check(rule.AllowInstallation.PlugAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(rule.DenyInstallation, NotNil)
	c.Check(rule.DenyInstallation.PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	// connection subrules
	checkBoolPlugConnConstraints(c, rule.AllowConnection, false)
	checkBoolPlugConnConstraints(c, rule.DenyConnection, true)
	// auto-connection subrules
	checkBoolPlugConnConstraints(c, rule.AllowAutoConnection, false)
	checkBoolPlugConnConstraints(c, rule.DenyAutoConnection, true)
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleDefaults(c *C) {
	rule, err := asserts.CompilePlugRule("iface", map[string]interface{}{
		"deny-auto-connection": "true",
	})
	c.Assert(err, IsNil)

	// everything follows the defaults...

	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	c.Check(rule.AllowInstallation.PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, NotNil)
	c.Check(rule.DenyInstallation.PlugAttributes, Equals, asserts.NeverMatchAttributes)
	// connection subrules
	checkBoolPlugConnConstraints(c, rule.AllowConnection, true)
	checkBoolPlugConnConstraints(c, rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolPlugConnConstraints(c, rule.AllowAutoConnection, true)
	// ... but deny-auto-connection is on
	checkBoolPlugConnConstraints(c, rule.DenyAutoConnection, true)
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleInstalationConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompilePlugRule("iface", map[string]interface{}{
		"allow-installation": map[string]interface{}{
			"plug-snap-type": []interface{}{"core", "kernel", "gadget", "app"},
		},
	})
	c.Assert(err, IsNil)

	cstrs := rule.AllowInstallation
	c.Check(cstrs.PlugSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompilePlugRule("iface", map[string]interface{}{
		"allow-connection": map[string]interface{}{
			"slot-snap-type":    []interface{}{"core", "kernel", "gadget", "app"},
			"slot-snap-id":      []interface{}{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"},
			"slot-publisher-id": []interface{}{"pubidpubidpubidpubidpubidpubid09", "canonical", "$same"},
		},
	})
	c.Assert(err, IsNil)

	cstrs := rule.AllowConnection
	c.Check(cstrs.SlotSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
	c.Check(cstrs.SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Check(cstrs.SlotPublisherIDs, DeepEquals, []string{"pubidpubidpubidpubidpubidpubid09", "canonical", "$same"})

}

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsAttributesDefault(c *C) {
	rule, err := asserts.CompilePlugRule("iface", map[string]interface{}{
		"allow-connection": map[string]interface{}{
			"slot-snap-id": []interface{}{"snapidsnapidsnapidsnapidsnapid01"},
		},
	})
	c.Assert(err, IsNil)

	// attributes default to always matching here
	cstrs := rule.AllowConnection
	c.Check(cstrs.PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Check(cstrs.SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleErrors(c *C) {
	tests := []struct {
		stanza string
		err    string
	}{
		{`iface: foo`, `plug rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  - allow`, `plug rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  allow-installation: foo`, `allow-installation in plug rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  deny-installation: foo`, `deny-installation in plug rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  allow-connection: foo`, `allow-connection in plug rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  allow-installation:
    plug-attributes:
      a1: [`, `cannot compile plug-attributes in allow-installation in plug rule for interface "iface": cannot compile "a1" constraint .*`},
		{`iface:
  allow-connection:
    slot-attributes:
      a2: [`, `cannot compile slot-attributes in allow-connection in plug rule for interface "iface": cannot compile "a2" constraint .*`},
		{`iface:
  allow-connection:
    slot-snap-id:
      -
        foo: 1`, `slot-snap-id in allow-connection in plug rule for interface "iface" must be a list of strings`},
		{`iface:
  allow-connection:
    slot-snap-id:
      - foo`, `slot-snap-id in allow-connection in plug rule for interface "iface" contains an invalid element: "foo"`},
		{`iface:
  allow-connection:
    slot-snap-type:
      - foo`, `slot-snap-type in allow-connection in plug rule for interface "iface" contains an invalid element: "foo"`},
		{`iface:
  allow-connection:
    slot-snap-type:
      - xapp`, `slot-snap-type in allow-connection in plug rule for interface "iface" contains an invalid element: "xapp"`},
		{`iface:
  allow-connection:
    slot-snap-ids:
      - foo`, `allow-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id`},
		{`iface:
  deny-connection:
    slot-snap-ids:
      - foo`, `deny-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id`},
		{`iface:
  allow-auto-connection:
    slot-snap-ids:
      - foo`, `allow-auto-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id`},
		{`iface:
  deny-auto-connection:
    slot-snap-ids:
      - foo`, `deny-auto-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id`},
		{`iface:
  allow-connect: true`, `plug rule for interface "iface" must specify at least one of allow-installation, deny-installation, allow-connection, deny-connection, allow-auto-connection, deny-auto-connection`},
	}

	for _, t := range tests {
		m, err := asserts.ParseHeaders([]byte(t.stanza))
		c.Assert(err, IsNil, Commentf(t.stanza))

		_, err = asserts.CompilePlugRule("iface", m["iface"])
		c.Check(err, ErrorMatches, t.err, Commentf(t.stanza))
	}
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleAllAllowDenyStanzas(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    slot-attributes:
      a1: A1
  deny-installation:
    slot-attributes:
      a2: A2
  allow-connection:
    plug-attributes:
      pa3: PA3
    slot-attributes:
      sa3: SA3
  deny-connection:
    plug-attributes:
      pa4: PA5
    slot-attributes:
      sa4: SA4
  allow-auto-connection:
    plug-attributes:
      pa5: PA5
    slot-attributes:
      sa5: SA5
  deny-auto-connection:
    plug-attributes:
      pa6: PA6
    slot-attributes:
      sa6: SA6`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	checkAttrs(c, rule.AllowInstallation.SlotAttributes, "a1")
	c.Assert(rule.DenyInstallation, NotNil)
	checkAttrs(c, rule.DenyInstallation.SlotAttributes, "a2")
	// connection subrules
	c.Assert(rule.AllowConnection, NotNil)
	checkAttrs(c, rule.AllowConnection.PlugAttributes, "pa3")
	checkAttrs(c, rule.AllowConnection.SlotAttributes, "sa3")
	c.Assert(rule.DenyConnection, NotNil)
	checkAttrs(c, rule.DenyConnection.PlugAttributes, "pa4")
	checkAttrs(c, rule.DenyConnection.SlotAttributes, "sa4")
	// auto-connection subrules
	c.Assert(rule.AllowAutoConnection, NotNil)
	checkAttrs(c, rule.AllowAutoConnection.PlugAttributes, "pa5")
	checkAttrs(c, rule.AllowAutoConnection.SlotAttributes, "sa5")
	c.Assert(rule.DenyAutoConnection, NotNil)
	checkAttrs(c, rule.DenyAutoConnection.PlugAttributes, "pa6")
	checkAttrs(c, rule.DenyAutoConnection.SlotAttributes, "sa6")
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleShortcutTrue(c *C) {
	rule, err := asserts.CompileSlotRule("iface", "true")
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	c.Check(rule.AllowInstallation.SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, NotNil)
	c.Check(rule.DenyInstallation.SlotAttributes, Equals, asserts.NeverMatchAttributes)
	// connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowConnection, true)
	checkBoolSlotConnConstraints(c, rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowAutoConnection, true)
	checkBoolSlotConnConstraints(c, rule.DenyAutoConnection, false)
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleShortcutFalse(c *C) {
	rule, err := asserts.CompileSlotRule("iface", "false")
	c.Assert(err, IsNil)

	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	c.Check(rule.AllowInstallation.SlotAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(rule.DenyInstallation, NotNil)
	c.Check(rule.DenyInstallation.SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
	// connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowConnection, false)
	checkBoolSlotConnConstraints(c, rule.DenyConnection, true)
	// auto-connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowAutoConnection, false)
	checkBoolSlotConnConstraints(c, rule.DenyAutoConnection, true)
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleDefaults(c *C) {
	rule, err := asserts.CompileSlotRule("iface", map[string]interface{}{
		"deny-auto-connection": "true",
	})
	c.Assert(err, IsNil)

	// everything follows the defaults...

	// install subrules
	c.Assert(rule.AllowInstallation, NotNil)
	c.Check(rule.AllowInstallation.SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, NotNil)
	c.Check(rule.DenyInstallation.SlotAttributes, Equals, asserts.NeverMatchAttributes)
	// connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowConnection, true)
	checkBoolSlotConnConstraints(c, rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowAutoConnection, true)
	// ... but deny-auto-connection is on
	checkBoolSlotConnConstraints(c, rule.DenyAutoConnection, true)
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleInstalationConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompileSlotRule("iface", map[string]interface{}{
		"allow-installation": map[string]interface{}{
			"slot-snap-type": []interface{}{"core", "kernel", "gadget", "app"},
		},
	})
	c.Assert(err, IsNil)

	cstrs := rule.AllowInstallation
	c.Check(cstrs.SlotSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleConnectionConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompileSlotRule("iface", map[string]interface{}{
		"allow-connection": map[string]interface{}{
			"plug-snap-type":    []interface{}{"core", "kernel", "gadget", "app"},
			"plug-snap-id":      []interface{}{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"},
			"plug-publisher-id": []interface{}{"pubidpubidpubidpubidpubidpubid09", "canonical", "$same"},
		},
	})
	c.Assert(err, IsNil)

	cstrs := rule.AllowConnection
	c.Check(cstrs.PlugSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
	c.Check(cstrs.PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Check(cstrs.PlugPublisherIDs, DeepEquals, []string{"pubidpubidpubidpubidpubidpubid09", "canonical", "$same"})
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleErrors(c *C) {
	tests := []struct {
		stanza string
		err    string
	}{
		{`iface: foo`, `slot rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  - allow`, `slot rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  allow-installation: foo`, `allow-installation in slot rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  deny-installation: foo`, `deny-installation in slot rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  allow-connection: foo`, `allow-connection in slot rule for interface "iface" must be a map or one of the shortcuts 'true' or 'false'`},
		{`iface:
  allow-installation:
    slot-attributes:
      a1: [`, `cannot compile slot-attributes in allow-installation in slot rule for interface "iface": cannot compile "a1" constraint .*`},
		{`iface:
  allow-connection:
    plug-attributes:
      a2: [`, `cannot compile plug-attributes in allow-connection in slot rule for interface "iface": cannot compile "a2" constraint .*`},
		{`iface:
  allow-connection:
    plug-snap-id:
      -
        foo: 1`, `plug-snap-id in allow-connection in slot rule for interface "iface" must be a list of strings`},
		{`iface:
  allow-connection:
    plug-snap-id:
      - foo`, `plug-snap-id in allow-connection in slot rule for interface "iface" contains an invalid element: "foo"`},
		{`iface:
  allow-connection:
    plug-snap-type:
      - foo`, `plug-snap-type in allow-connection in slot rule for interface "iface" contains an invalid element: "foo"`},
		{`iface:
  allow-connection:
    plug-snap-type:
      - xapp`, `plug-snap-type in allow-connection in slot rule for interface "iface" contains an invalid element: "xapp"`},
		{`iface:
  allow-connection:
    plug-snap-ids:
      - foo`, `allow-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id`},
		{`iface:
  deny-connection:
    plug-snap-ids:
      - foo`, `deny-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id`},
		{`iface:
  allow-auto-connection:
    plug-snap-ids:
      - foo`, `allow-auto-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id`},
		{`iface:
  deny-auto-connection:
    plug-snap-ids:
      - foo`, `deny-auto-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id`},
		{`iface:
  allow-connect: true`, `slot rule for interface "iface" must specify at least one of allow-installation, deny-installation, allow-connection, deny-connection, allow-auto-connection, deny-auto-connection`},
	}

	for _, t := range tests {
		m, err := asserts.ParseHeaders([]byte(t.stanza))
		c.Assert(err, IsNil, Commentf(t.stanza))
		_, err = asserts.CompileSlotRule("iface", m["iface"])
		c.Check(err, ErrorMatches, t.err, Commentf(t.stanza))
	}
}
