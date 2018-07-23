// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2017 Canonical Ltd
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
	"regexp"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"

	"github.com/snapcore/snapd/testutil"
)

var (
	_ = Suite(&attrConstraintsSuite{})
	_ = Suite(&plugSlotRulesSuite{})
)

type attrConstraintsSuite struct {
	testutil.BaseTest
}

type attrerObject map[string]interface{}

func (o attrerObject) Lookup(path string) (interface{}, bool) {
	v, ok := o[path]
	return v, ok
}

func attrs(yml string) *attrerObject {
	var attrs map[string]interface{}
	err := yaml.Unmarshal([]byte(yml), &attrs)
	if err != nil {
		panic(err)
	}
	snapYaml, err := yaml.Marshal(map[string]interface{}{
		"name": "sample",
		"plugs": map[string]interface{}{
			"plug": attrs,
		},
	})
	if err != nil {
		panic(err)
	}

	// NOTE: it's important to go through snap yaml here even though we're really interested in Attrs only,
	// as InfoFromSnapYaml normalizes yaml values.
	info, err := snap.InfoFromSnapYaml(snapYaml)
	if err != nil {
		panic(err)
	}

	var ao attrerObject
	ao = info.Plugs["plug"].Attrs
	return &ao
}

func (s *attrConstraintsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *attrConstraintsSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *attrConstraintsSuite) TestSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar: BAR`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	plug := attrerObject(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, IsNil)

	plug = attrerObject(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BAZ" does not match \^\(BAR\)\$`)

	plug = attrerObject(map[string]interface{}{
		"foo": "FOO",
		"baz": "BAZ",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, ErrorMatches, `attribute "bar" has constraints but is unset`)
}

func (s *attrConstraintsSuite) TestSimpleAnchorsVsRegexpAlt(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  bar: BAR|BAZ`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	plug := attrerObject(map[string]interface{}{
		"bar": "BAR",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, IsNil)

	plug = attrerObject(map[string]interface{}{
		"bar": "BARR",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BARR" does not match \^\(BAR|BAZ\)\$`)

	plug = attrerObject(map[string]interface{}{
		"bar": "BBAZ",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BAZZ" does not match \^\(BAR|BAZ\)\$`)

	plug = attrerObject(map[string]interface{}{
		"bar": "BABAZ",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BABAZ" does not match \^\(BAR|BAZ\)\$`)

	plug = attrerObject(map[string]interface{}{
		"bar": "BARAZ",
	})
	err = cstrs.Check(plug, nil)
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
`), nil)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar: BAZ
baz: BAZ
`), nil)
	c.Check(err, ErrorMatches, `attribute "bar" must be a map`)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
  bar3: BAR3
baz: BAZ
`), nil)
	c.Check(err, ErrorMatches, `attribute "bar\.bar2" value "BAR22" does not match \^\(BAR2\)\$`)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2:
    bar22: true
  bar3: BAR3
baz: BAZ
`), nil)
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

	plug := attrerObject(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, IsNil)

	plug = attrerObject(map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	})
	err = cstrs.Check(plug, nil)
	c.Check(err, IsNil)

	plug = attrerObject(map[string]interface{}{
		"foo": "FOO",
		"bar": "BARR",
		"baz": "BAR",
	})
	err = cstrs.Check(plug, nil)
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
`), nil)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
`), nil)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR3
`), nil)
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
`), nil)
	c.Check(err, IsNil)

	plug := attrerObject(map[string]interface{}{
		"foo": int64(1),
		"bar": true,
	})
	err = cstrs.Check(plug, nil)
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

	wrongDollarConstraints := []string{
		"$",
		"$FOO(a)",
		"$SLOT",
		"$SLOT()",
	}

	for _, wrong := range wrongDollarConstraints {
		_, err := asserts.CompileAttributeConstraints(map[string]interface{}{
			"foo": wrong,
		})
		c.Check(err, ErrorMatches, fmt.Sprintf(`cannot compile "foo" constraint "%s": not a valid \$SLOT\(\)/\$PLUG\(\) constraint`, regexp.QuoteMeta(wrong)))

	}
}

func (s *attrConstraintsSuite) TestMatchingListsSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: /foo/.*`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: ["/foo/x", "/foo/y"]
`), nil)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: ["/foo/x", "/foo"]
`), nil)
	c.Check(err, ErrorMatches, `attribute "foo\.1" value "/foo" does not match \^\(/foo/\.\*\)\$`)
}

func (s *attrConstraintsSuite) TestMissingCheck(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: $MISSING`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)
	c.Check(asserts.RuleFeature(cstrs, "dollar-attr-constraints"), Equals, true)

	err = cstrs.Check(attrs(`
bar: baz
`), nil)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: ["x"]
`), nil)
	c.Check(err, ErrorMatches, `attribute "foo" is constrained to be missing but is set`)
}

type testEvalAttr struct {
	comp func(side string, arg string) (interface{}, error)
}

func (ca testEvalAttr) SlotAttr(arg string) (interface{}, error) {
	return ca.comp("slot", arg)
}

func (ca testEvalAttr) PlugAttr(arg string) (interface{}, error) {
	return ca.comp("plug", arg)
}

func (s *attrConstraintsSuite) TestEvalCheck(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: $SLOT(foo)
  bar: $PLUG(bar.baz)`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)
	c.Check(asserts.RuleFeature(cstrs, "dollar-attr-constraints"), Equals, true)

	err = cstrs.Check(attrs(`
foo: foo
bar: bar
`), nil)
	c.Check(err, ErrorMatches, `attribute "(foo|bar)" cannot be matched without context`)

	calls := make(map[[2]string]bool)
	comp1 := func(op string, arg string) (interface{}, error) {
		calls[[2]string{op, arg}] = true
		return arg, nil
	}

	err = cstrs.Check(attrs(`
foo: foo
bar: bar.baz
`), testEvalAttr{comp1})
	c.Check(err, IsNil)

	c.Check(calls, DeepEquals, map[[2]string]bool{
		{"slot", "foo"}:     true,
		{"plug", "bar.baz"}: true,
	})

	comp2 := func(op string, arg string) (interface{}, error) {
		if op == "plug" {
			return nil, fmt.Errorf("boom")
		}
		return arg, nil
	}

	err = cstrs.Check(attrs(`
foo: foo
bar: bar.baz
`), testEvalAttr{comp2})
	c.Check(err, ErrorMatches, `attribute "bar" constraint \$PLUG\(bar\.baz\) cannot be evaluated: boom`)

	comp3 := func(op string, arg string) (interface{}, error) {
		if op == "slot" {
			return "other-value", nil
		}
		return arg, nil
	}

	err = cstrs.Check(attrs(`
foo: foo
bar: bar.baz
`), testEvalAttr{comp3})
	c.Check(err, ErrorMatches, `attribute "foo" does not match \$SLOT\(foo\): foo != other-value`)
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
`), nil)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: [{p: "zzz"}, {p: "/foo/y"}]
`), nil)
	c.Check(err, ErrorMatches, `attribute "foo\.0\.p" value "zzz" does not match \^\(/foo/\.\*\)\$`)
}

func (s *attrConstraintsSuite) TestAlwaysMatchAttributeConstraints(c *C) {
	c.Check(asserts.AlwaysMatchAttributes.Check(nil, nil), IsNil)
}

func (s *attrConstraintsSuite) TestNeverMatchAttributeConstraints(c *C) {
	c.Check(asserts.NeverMatchAttributes.Check(nil, nil), NotNil)
}

type plugSlotRulesSuite struct{}

func checkAttrs(c *C, attrs *asserts.AttributeConstraints, witness, expected string) {
	plug := attrerObject(map[string]interface{}{
		witness: "XYZ",
	})
	c.Check(attrs.Check(plug, nil), ErrorMatches, fmt.Sprintf(`attribute "%s".*does not match.*`, witness))
	plug = attrerObject(map[string]interface{}{
		witness: expected,
	})
	c.Check(attrs.Check(plug, nil), IsNil)
}

func checkBoolPlugConnConstraints(c *C, cstrs []*asserts.PlugConnectionConstraints, always bool) {
	expected := asserts.NeverMatchAttributes
	if always {
		expected = asserts.AlwaysMatchAttributes
	}
	c.Assert(cstrs, HasLen, 1)
	cstrs1 := cstrs[0]
	c.Check(cstrs1.PlugAttributes, Equals, expected)
	c.Check(cstrs1.SlotAttributes, Equals, expected)
	c.Check(cstrs1.SlotSnapIDs, HasLen, 0)
	c.Check(cstrs1.SlotPublisherIDs, HasLen, 0)
	c.Check(cstrs1.SlotSnapTypes, HasLen, 0)
}

func checkBoolSlotConnConstraints(c *C, cstrs []*asserts.SlotConnectionConstraints, always bool) {
	expected := asserts.NeverMatchAttributes
	if always {
		expected = asserts.AlwaysMatchAttributes
	}
	c.Assert(cstrs, HasLen, 1)
	cstrs1 := cstrs[0]
	c.Check(cstrs1.PlugAttributes, Equals, expected)
	c.Check(cstrs1.SlotAttributes, Equals, expected)
	c.Check(cstrs1.PlugSnapIDs, HasLen, 0)
	c.Check(cstrs1.PlugPublisherIDs, HasLen, 0)
	c.Check(cstrs1.PlugSnapTypes, HasLen, 0)
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
      pa4: PA4
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
	c.Assert(rule.AllowInstallation, HasLen, 1)
	checkAttrs(c, rule.AllowInstallation[0].PlugAttributes, "a1", "A1")
	c.Assert(rule.DenyInstallation, HasLen, 1)
	checkAttrs(c, rule.DenyInstallation[0].PlugAttributes, "a2", "A2")
	// connection subrules
	c.Assert(rule.AllowConnection, HasLen, 1)
	checkAttrs(c, rule.AllowConnection[0].PlugAttributes, "pa3", "PA3")
	checkAttrs(c, rule.AllowConnection[0].SlotAttributes, "sa3", "SA3")
	c.Assert(rule.DenyConnection, HasLen, 1)
	checkAttrs(c, rule.DenyConnection[0].PlugAttributes, "pa4", "PA4")
	checkAttrs(c, rule.DenyConnection[0].SlotAttributes, "sa4", "SA4")
	// auto-connection subrules
	c.Assert(rule.AllowAutoConnection, HasLen, 1)
	checkAttrs(c, rule.AllowAutoConnection[0].PlugAttributes, "pa5", "PA5")
	checkAttrs(c, rule.AllowAutoConnection[0].SlotAttributes, "sa5", "SA5")
	c.Assert(rule.DenyAutoConnection, HasLen, 1)
	checkAttrs(c, rule.DenyAutoConnection[0].PlugAttributes, "pa6", "PA6")
	checkAttrs(c, rule.DenyAutoConnection[0].SlotAttributes, "sa6", "SA6")
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleAllAllowDenyOrStanzas(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    -
      plug-attributes:
        a1: A1
    -
      plug-attributes:
        a1: A1alt
  deny-installation:
    -
      plug-attributes:
        a2: A2
    -
      plug-attributes:
        a2: A2alt
  allow-connection:
    -
      plug-attributes:
        pa3: PA3
      slot-attributes:
        sa3: SA3
    -
      plug-attributes:
        pa3: PA3alt
  deny-connection:
    -
      plug-attributes:
        pa4: PA4
      slot-attributes:
        sa4: SA4
    -
      plug-attributes:
        pa4: PA4alt
  allow-auto-connection:
    -
      plug-attributes:
        pa5: PA5
      slot-attributes:
        sa5: SA5
    -
      plug-attributes:
        pa5: PA5alt
  deny-auto-connection:
    -
      plug-attributes:
        pa6: PA6
      slot-attributes:
        sa6: SA6
    -
      plug-attributes:
        pa6: PA6alt`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, HasLen, 2)
	checkAttrs(c, rule.AllowInstallation[0].PlugAttributes, "a1", "A1")
	checkAttrs(c, rule.AllowInstallation[1].PlugAttributes, "a1", "A1alt")
	c.Assert(rule.DenyInstallation, HasLen, 2)
	checkAttrs(c, rule.DenyInstallation[0].PlugAttributes, "a2", "A2")
	checkAttrs(c, rule.DenyInstallation[1].PlugAttributes, "a2", "A2alt")
	// connection subrules
	c.Assert(rule.AllowConnection, HasLen, 2)
	checkAttrs(c, rule.AllowConnection[0].PlugAttributes, "pa3", "PA3")
	checkAttrs(c, rule.AllowConnection[0].SlotAttributes, "sa3", "SA3")
	checkAttrs(c, rule.AllowConnection[1].PlugAttributes, "pa3", "PA3alt")
	c.Assert(rule.DenyConnection, HasLen, 2)
	checkAttrs(c, rule.DenyConnection[0].PlugAttributes, "pa4", "PA4")
	checkAttrs(c, rule.DenyConnection[0].SlotAttributes, "sa4", "SA4")
	checkAttrs(c, rule.DenyConnection[1].PlugAttributes, "pa4", "PA4alt")
	// auto-connection subrules
	c.Assert(rule.AllowAutoConnection, HasLen, 2)
	checkAttrs(c, rule.AllowAutoConnection[0].PlugAttributes, "pa5", "PA5")
	checkAttrs(c, rule.AllowAutoConnection[0].SlotAttributes, "sa5", "SA5")
	checkAttrs(c, rule.AllowAutoConnection[1].PlugAttributes, "pa5", "PA5alt")
	c.Assert(rule.DenyAutoConnection, HasLen, 2)
	checkAttrs(c, rule.DenyAutoConnection[0].PlugAttributes, "pa6", "PA6")
	checkAttrs(c, rule.DenyAutoConnection[0].SlotAttributes, "sa6", "SA6")
	checkAttrs(c, rule.DenyAutoConnection[1].PlugAttributes, "pa6", "PA6alt")
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleShortcutTrue(c *C) {
	rule, err := asserts.CompilePlugRule("iface", "true")
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, HasLen, 1)
	c.Check(rule.AllowInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, HasLen, 1)
	c.Check(rule.DenyInstallation[0].PlugAttributes, Equals, asserts.NeverMatchAttributes)
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
	c.Assert(rule.AllowInstallation, HasLen, 1)
	c.Check(rule.AllowInstallation[0].PlugAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(rule.DenyInstallation, HasLen, 1)
	c.Check(rule.DenyInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
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
	c.Assert(rule.AllowInstallation, HasLen, 1)
	c.Check(rule.AllowInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, HasLen, 1)
	c.Check(rule.DenyInstallation[0].PlugAttributes, Equals, asserts.NeverMatchAttributes)
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

	c.Assert(rule.AllowInstallation, HasLen, 1)
	cstrs := rule.AllowInstallation[0]
	c.Check(cstrs.PlugSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleInstallationConstraintsOnClassic(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, IsNil)

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    on-classic: false`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    on-classic: true`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    on-classic:
      - ubuntu
      - debian`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true, SystemIDs: []string{"ubuntu", "debian"}})
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompilePlugRule("iface", map[string]interface{}{
		"allow-connection": map[string]interface{}{
			"slot-snap-type":    []interface{}{"core", "kernel", "gadget", "app"},
			"slot-snap-id":      []interface{}{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"},
			"slot-publisher-id": []interface{}{"pubidpubidpubidpubidpubidpubid09", "canonical", "$SAME"},
		},
	})
	c.Assert(err, IsNil)

	c.Assert(rule.AllowConnection, HasLen, 1)
	cstrs := rule.AllowConnection[0]
	c.Check(cstrs.SlotSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
	c.Check(cstrs.SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Check(cstrs.SlotPublisherIDs, DeepEquals, []string{"pubidpubidpubidpubidpubidpubid09", "canonical", "$SAME"})

}

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsOnClassic(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, IsNil)

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-connection:
    on-classic: false`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-connection:
    on-classic: true`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-connection:
    on-classic:
      - ubuntu
      - debian`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true, SystemIDs: []string{"ubuntu", "debian"}})
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsAttributesDefault(c *C) {
	rule, err := asserts.CompilePlugRule("iface", map[string]interface{}{
		"allow-connection": map[string]interface{}{
			"slot-snap-id": []interface{}{"snapidsnapidsnapidsnapidsnapid01"},
		},
	})
	c.Assert(err, IsNil)

	// attributes default to always matching here
	cstrs := rule.AllowConnection[0]
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
  allow-connection:
    - foo`, `alternative 1 of allow-connection in plug rule for interface "iface" must be a map`},
		{`iface:
  allow-connection:
    - true`, `alternative 1 of allow-connection in plug rule for interface "iface" must be a map`},
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
      - foo`, `allow-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, on-classic`},
		{`iface:
  deny-connection:
    slot-snap-ids:
      - foo`, `deny-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, on-classic`},
		{`iface:
  allow-auto-connection:
    slot-snap-ids:
      - foo`, `allow-auto-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, on-classic`},
		{`iface:
  deny-auto-connection:
    slot-snap-ids:
      - foo`, `deny-auto-connection in plug rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, on-classic`},
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

func (s *plugSlotRulesSuite) TestPlugRuleFeatures(c *C) {
	combos := []struct {
		subrule         string
		attrConstraints []string
	}{
		{"allow-installation", []string{"plug-attributes"}},
		{"deny-installation", []string{"plug-attributes"}},
		{"allow-connection", []string{"plug-attributes", "slot-attributes"}},
		{"deny-connection", []string{"plug-attributes", "slot-attributes"}},
		{"allow-auto-connection", []string{"plug-attributes", "slot-attributes"}},
		{"deny-auto-connection", []string{"plug-attributes", "slot-attributes"}},
	}

	for _, combo := range combos {
		for _, attrConstr := range combo.attrConstraints {
			attrConstraintMap := map[string]interface{}{
				"a":     "ATTR",
				"other": []interface{}{"x", "y"},
			}
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					attrConstr: attrConstraintMap,
				},
			}

			rule, err := asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, false, Commentf("%v", ruleMap))

			attrConstraintMap["a"] = "$MISSING"
			rule, err = asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, true, Commentf("%v", ruleMap))

			// covers also alternation
			attrConstraintMap["a"] = []interface{}{"$SLOT(a)"}
			rule, err = asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, true, Commentf("%v", ruleMap))

		}
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
      pa4: PA4
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
	c.Assert(rule.AllowInstallation, HasLen, 1)
	checkAttrs(c, rule.AllowInstallation[0].SlotAttributes, "a1", "A1")
	c.Assert(rule.DenyInstallation, HasLen, 1)
	checkAttrs(c, rule.DenyInstallation[0].SlotAttributes, "a2", "A2")
	// connection subrules
	c.Assert(rule.AllowConnection, HasLen, 1)
	checkAttrs(c, rule.AllowConnection[0].PlugAttributes, "pa3", "PA3")
	checkAttrs(c, rule.AllowConnection[0].SlotAttributes, "sa3", "SA3")
	c.Assert(rule.DenyConnection, HasLen, 1)
	checkAttrs(c, rule.DenyConnection[0].PlugAttributes, "pa4", "PA4")
	checkAttrs(c, rule.DenyConnection[0].SlotAttributes, "sa4", "SA4")
	// auto-connection subrules
	c.Assert(rule.AllowAutoConnection, HasLen, 1)
	checkAttrs(c, rule.AllowAutoConnection[0].PlugAttributes, "pa5", "PA5")
	checkAttrs(c, rule.AllowAutoConnection[0].SlotAttributes, "sa5", "SA5")
	c.Assert(rule.DenyAutoConnection, HasLen, 1)
	checkAttrs(c, rule.DenyAutoConnection[0].PlugAttributes, "pa6", "PA6")
	checkAttrs(c, rule.DenyAutoConnection[0].SlotAttributes, "sa6", "SA6")
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleAllAllowDenyOrStanzas(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    -
      slot-attributes:
        a1: A1
    -
      slot-attributes:
        a1: A1alt
  deny-installation:
    -
      slot-attributes:
        a2: A2
    -
      slot-attributes:
        a2: A2alt
  allow-connection:
    -
      plug-attributes:
        pa3: PA3
      slot-attributes:
        sa3: SA3
    -
      slot-attributes:
        sa3: SA3alt
  deny-connection:
    -
      plug-attributes:
        pa4: PA4
      slot-attributes:
        sa4: SA4
    -
      slot-attributes:
        sa4: SA4alt
  allow-auto-connection:
    -
      plug-attributes:
        pa5: PA5
      slot-attributes:
        sa5: SA5
    -
      slot-attributes:
        sa5: SA5alt
  deny-auto-connection:
    -
      plug-attributes:
        pa6: PA6
      slot-attributes:
        sa6: SA6
    -
      slot-attributes:
        sa6: SA6alt`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, HasLen, 2)
	checkAttrs(c, rule.AllowInstallation[0].SlotAttributes, "a1", "A1")
	checkAttrs(c, rule.AllowInstallation[1].SlotAttributes, "a1", "A1alt")
	c.Assert(rule.DenyInstallation, HasLen, 2)
	checkAttrs(c, rule.DenyInstallation[0].SlotAttributes, "a2", "A2")
	checkAttrs(c, rule.DenyInstallation[1].SlotAttributes, "a2", "A2alt")
	// connection subrules
	c.Assert(rule.AllowConnection, HasLen, 2)
	checkAttrs(c, rule.AllowConnection[0].PlugAttributes, "pa3", "PA3")
	checkAttrs(c, rule.AllowConnection[0].SlotAttributes, "sa3", "SA3")
	checkAttrs(c, rule.AllowConnection[1].SlotAttributes, "sa3", "SA3alt")
	c.Assert(rule.DenyConnection, HasLen, 2)
	checkAttrs(c, rule.DenyConnection[0].PlugAttributes, "pa4", "PA4")
	checkAttrs(c, rule.DenyConnection[0].SlotAttributes, "sa4", "SA4")
	checkAttrs(c, rule.DenyConnection[1].SlotAttributes, "sa4", "SA4alt")
	// auto-connection subrules
	c.Assert(rule.AllowAutoConnection, HasLen, 2)
	checkAttrs(c, rule.AllowAutoConnection[0].PlugAttributes, "pa5", "PA5")
	checkAttrs(c, rule.AllowAutoConnection[0].SlotAttributes, "sa5", "SA5")
	checkAttrs(c, rule.AllowAutoConnection[1].SlotAttributes, "sa5", "SA5alt")
	c.Assert(rule.DenyAutoConnection, HasLen, 2)
	checkAttrs(c, rule.DenyAutoConnection[0].PlugAttributes, "pa6", "PA6")
	checkAttrs(c, rule.DenyAutoConnection[0].SlotAttributes, "sa6", "SA6")
	checkAttrs(c, rule.DenyAutoConnection[1].SlotAttributes, "sa6", "SA6alt")
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleShortcutTrue(c *C) {
	rule, err := asserts.CompileSlotRule("iface", "true")
	c.Assert(err, IsNil)

	c.Check(rule.Interface, Equals, "iface")
	// install subrules
	c.Assert(rule.AllowInstallation, HasLen, 1)
	c.Check(rule.AllowInstallation[0].SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, HasLen, 1)
	c.Check(rule.DenyInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
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
	c.Assert(rule.AllowInstallation, HasLen, 1)
	c.Check(rule.AllowInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(rule.DenyInstallation, HasLen, 1)
	c.Check(rule.DenyInstallation[0].SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
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
	c.Assert(rule.AllowInstallation, HasLen, 1)
	c.Check(rule.AllowInstallation[0].SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(rule.DenyInstallation, HasLen, 1)
	c.Check(rule.DenyInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
	// connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowConnection, true)
	checkBoolSlotConnConstraints(c, rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolSlotConnConstraints(c, rule.AllowAutoConnection, true)
	// ... but deny-auto-connection is on
	checkBoolSlotConnConstraints(c, rule.DenyAutoConnection, true)
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleInstallationConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompileSlotRule("iface", map[string]interface{}{
		"allow-installation": map[string]interface{}{
			"slot-snap-type": []interface{}{"core", "kernel", "gadget", "app"},
		},
	})
	c.Assert(err, IsNil)

	c.Assert(rule.AllowInstallation, HasLen, 1)
	cstrs := rule.AllowInstallation[0]
	c.Check(cstrs.SlotSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleInstallationConstraintsOnClassic(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, IsNil)

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    on-classic: false`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    on-classic: true`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-installation:
    on-classic:
      - ubuntu
      - debian`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true, SystemIDs: []string{"ubuntu", "debian"}})
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleConnectionConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompileSlotRule("iface", map[string]interface{}{
		"allow-connection": map[string]interface{}{
			"plug-snap-type":    []interface{}{"core", "kernel", "gadget", "app"},
			"plug-snap-id":      []interface{}{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"},
			"plug-publisher-id": []interface{}{"pubidpubidpubidpubidpubidpubid09", "canonical", "$SAME"},
		},
	})
	c.Assert(err, IsNil)

	c.Assert(rule.AllowConnection, HasLen, 1)
	cstrs := rule.AllowConnection[0]
	c.Check(cstrs.PlugSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
	c.Check(cstrs.PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Check(cstrs.PlugPublisherIDs, DeepEquals, []string{"pubidpubidpubidpubidpubidpubid09", "canonical", "$SAME"})
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleConnectionConstraintsOnClassic(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, IsNil)

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-connection:
    on-classic: false`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-connection:
    on-classic: true`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true})

	m, err = asserts.ParseHeaders([]byte(`iface:
  allow-connection:
    on-classic:
      - ubuntu
      - debian`))
	c.Assert(err, IsNil)

	rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: true, SystemIDs: []string{"ubuntu", "debian"}})
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
  allow-connection:
    - foo`, `alternative 1 of allow-connection in slot rule for interface "iface" must be a map`},
		{`iface:
  allow-connection:
    - true`, `alternative 1 of allow-connection in slot rule for interface "iface" must be a map`},
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
    on-classic:
      x: 1`, `on-classic in allow-connection in slot rule for interface \"iface\" must be 'true', 'false' or a list of operating system IDs`},
		{`iface:
  allow-connection:
    on-classic:
      - zoom!`, `on-classic in allow-connection in slot rule for interface \"iface\" contains an invalid element: \"zoom!\"`},
		{`iface:
  allow-connection:
    plug-snap-ids:
      - foo`, `allow-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id, on-classic`},
		{`iface:
  deny-connection:
    plug-snap-ids:
      - foo`, `deny-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id, on-classic`},
		{`iface:
  allow-auto-connection:
    plug-snap-ids:
      - foo`, `allow-auto-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id, on-classic`},
		{`iface:
  deny-auto-connection:
    plug-snap-ids:
      - foo`, `deny-auto-connection in slot rule for interface "iface" must specify at least one of plug-attributes, slot-attributes, plug-snap-type, plug-publisher-id, plug-snap-id, on-classic`},
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

func (s *plugSlotRulesSuite) TestSlotRuleFeatures(c *C) {
	combos := []struct {
		subrule         string
		attrConstraints []string
	}{
		{"allow-installation", []string{"slot-attributes"}},
		{"deny-installation", []string{"slot-attributes"}},
		{"allow-connection", []string{"plug-attributes", "slot-attributes"}},
		{"deny-connection", []string{"plug-attributes", "slot-attributes"}},
		{"allow-auto-connection", []string{"plug-attributes", "slot-attributes"}},
		{"deny-auto-connection", []string{"plug-attributes", "slot-attributes"}},
	}

	for _, combo := range combos {
		for _, attrConstr := range combo.attrConstraints {
			attrConstraintMap := map[string]interface{}{
				"a": "ATTR",
			}
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					attrConstr: attrConstraintMap,
				},
			}

			rule, err := asserts.CompileSlotRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, false, Commentf("%v", ruleMap))

			attrConstraintMap["a"] = "$PLUG(a)"
			rule, err = asserts.CompileSlotRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, true, Commentf("%v", ruleMap))

		}
	}
}
