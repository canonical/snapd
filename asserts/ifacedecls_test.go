// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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
	"strings"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var (
	_ = Suite(&attrConstraintsSuite{})
	_ = Suite(&nameConstraintsSuite{})
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

	ao := attrerObject(info.Plugs["plug"].Attrs)
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

func (s *attrConstraintsSuite) TestMissingCheck(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: $MISSING`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)
	c.Check(asserts.RuleFeature(cstrs, "dollar-attr-constraints"), Equals, true)
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

func (s *attrConstraintsSuite) TestAlternativeMatchingComplex(c *C) {
	toMatch := attrs(`
mnt: [{what: "/dev/x*", where: "/foo/*", options: ["rw", "nodev"]}, {what: "/bar/*", where: "/baz/*", options: ["rw", "bind"]}]
`)

	m, err := asserts.ParseHeaders([]byte(`attrs:
  mnt:
    -
      what: /(bar/|dev/x)\*
      where: /(foo|baz)/\*
      options: rw|bind|nodev`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(toMatch, nil)
	c.Check(err, IsNil)

	m, err = asserts.ParseHeaders([]byte(`attrs:
  mnt:
    -
      what: /dev/x\*
      where: /foo/\*
      options:
        - nodev
        - rw
    -
      what: /bar/\*
      where: /baz/\*
      options:
        - rw
        - bind`))
	c.Assert(err, IsNil)

	cstrsExtensive, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrsExtensive.Check(toMatch, nil)
	c.Check(err, IsNil)

	// not matching case
	m, err = asserts.ParseHeaders([]byte(`attrs:
  mnt:
    -
      what: /dev/x\*
      where: /foo/\*
      options:
        - rw
    -
      what: /bar/\*
      where: /baz/\*
      options:
        - rw
        - bind`))
	c.Assert(err, IsNil)

	cstrsExtensiveNoMatch, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrsExtensiveNoMatch.Check(toMatch, nil)
	c.Check(err, ErrorMatches, `no alternative for attribute "mnt\.0" matches: no alternative for attribute "mnt\.0.options\.1" matches:.*`)
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
}

func (s *attrConstraintsSuite) TestCompileErrors(c *C) {
	_, err := asserts.CompileAttributeConstraints(map[string]interface{}{
		"foo": "[",
	})
	c.Check(err, ErrorMatches, `cannot compile "foo" constraint "\[": error parsing regexp:.*`)

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
		c.Check(err, ErrorMatches, fmt.Sprintf(`cannot compile "foo" constraint "%s": not a valid \$SLOT\(\)/\$PLUG\(\)/\$PLUG_PUBLISHER_ID/\$SLOT_PUBLISHER_ID constraint`, regexp.QuoteMeta(wrong)))

	}
}

type testEvalAttr struct {
	comp            func(side string, arg string) (interface{}, error)
	plugPublisherID string
	slotPublisherID string
}

func (ca testEvalAttr) SlotAttr(arg string) (interface{}, error) {
	return ca.comp("slot", arg)
}

func (ca testEvalAttr) PlugAttr(arg string) (interface{}, error) {
	return ca.comp("plug", arg)
}

func (ca testEvalAttr) PlugPublisherID() string {
	return ca.plugPublisherID
}

func (ca testEvalAttr) SlotPublisherID() string {
	return ca.slotPublisherID
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
`), testEvalAttr{comp: comp1})
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
`), testEvalAttr{comp: comp2})
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
`), testEvalAttr{comp: comp3})
	c.Check(err, ErrorMatches, `attribute "foo" does not match \$SLOT\(foo\): foo != other-value`)
}

func (s *attrConstraintsSuite) TestCheckWithAttrPlugPublisherID(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  my-attr: $PLUG_PUBLISHER_ID`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)
	c.Check(asserts.RuleFeature(cstrs, "publisher-id-constraints"), Equals, true)

	helper := testEvalAttr{plugPublisherID: "my-account"}

	err = cstrs.Check(attrs(`
my-attr: my-account
`), nil)
	c.Check(err, ErrorMatches, `attribute "my-attr" cannot be matched without context`)

	err = cstrs.Check(attrs(`
my-attr: my-account
`), helper)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
my-attr: other-account
`), helper)
	c.Check(err, ErrorMatches, `.*attribute "my-attr" does not match \$PLUG_PUBLISHER_ID\: other-account != my-account`)

	err = cstrs.Check(attrs(`
my-attr: 1
`), helper)
	c.Check(err, ErrorMatches, `.*attribute "my-attr" is not expected string type: int64`)
}

func (s *attrConstraintsSuite) TestCheckWithAttrSlotPublisherID(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  my-attr: $SLOT_PUBLISHER_ID`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeConstraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)
	c.Check(asserts.RuleFeature(cstrs, "publisher-id-constraints"), Equals, true)

	helper := testEvalAttr{slotPublisherID: "my-account"}

	err = cstrs.Check(attrs(`
my-attr: my-account
`), nil)
	c.Check(err, ErrorMatches, `attribute "my-attr" cannot be matched without context`)

	err = cstrs.Check(attrs(`
my-attr: my-account
`), helper)
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
my-attr: other-account
`), helper)
	c.Check(err, ErrorMatches, `.*attribute "my-attr" does not match \$SLOT_PUBLISHER_ID\: other-account != my-account`)

	err = cstrs.Check(attrs(`
my-attr: 1
`), helper)
	c.Check(err, ErrorMatches, `.*attribute "my-attr" is not expected string type: int64`)
}

func (s *attrConstraintsSuite) TestNeverMatchAttributeConstraints(c *C) {
	c.Check(asserts.NeverMatchAttributes.Check(nil, nil), NotNil)
}

type nameConstraintsSuite struct{}

func (s *nameConstraintsSuite) TestCompileErrors(c *C) {
	_, err := asserts.CompileNameConstraints("slot-names", "true")
	c.Check(err, ErrorMatches, `slot-names constraints must be a list of regexps and special \$ values`)

	_, err = asserts.CompileNameConstraints("slot-names", []interface{}{map[string]interface{}{"foo": "bar"}})
	c.Check(err, ErrorMatches, `slot-names constraint entry must be a regexp or special \$ value`)

	_, err = asserts.CompileNameConstraints("plug-names", []interface{}{"["})
	c.Check(err, ErrorMatches, `cannot compile plug-names constraint entry "\[":.*`)

	_, err = asserts.CompileNameConstraints("plug-names", []interface{}{"$"})
	c.Check(err, ErrorMatches, `plug-names constraint entry special value "\$" is invalid`)

	_, err = asserts.CompileNameConstraints("slot-names", []interface{}{"$12"})
	c.Check(err, ErrorMatches, `slot-names constraint entry special value "\$12" is invalid`)

	_, err = asserts.CompileNameConstraints("plug-names", []interface{}{"a b"})
	c.Check(err, ErrorMatches, `plug-names constraint entry regexp contains unexpected spaces`)
}

func (s *nameConstraintsSuite) TestCheck(c *C) {
	nc, err := asserts.CompileNameConstraints("slot-names", []interface{}{"foo[0-9]", "bar"})
	c.Assert(err, IsNil)

	for _, matching := range []string{"foo0", "foo1", "bar"} {
		c.Check(nc.Check("slot name", matching, nil), IsNil)
	}

	for _, notMatching := range []string{"baz", "fooo", "foo12"} {
		c.Check(nc.Check("slot name", notMatching, nil), ErrorMatches, fmt.Sprintf(`slot name %q does not match constraints`, notMatching))
	}

}

func (s *nameConstraintsSuite) TestCheckSpecial(c *C) {
	nc, err := asserts.CompileNameConstraints("slot-names", []interface{}{"$INTERFACE"})
	c.Assert(err, IsNil)

	c.Check(nc.Check("slot name", "foo", nil), ErrorMatches, `slot name "foo" does not match constraints`)
	c.Check(nc.Check("slot name", "foo", map[string]string{"$INTERFACE": "foo"}), IsNil)
	c.Check(nc.Check("slot name", "bar", map[string]string{"$INTERFACE": "foo"}), ErrorMatches, `slot name "bar" does not match constraints`)
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

var (
	sideArityAny = asserts.SideArityConstraint{N: -1}
	sideArityOne = asserts.SideArityConstraint{N: 1}
)

func checkBoolPlugConnConstraints(c *C, subrule string, cstrs []*asserts.PlugConnectionConstraints, always bool) {
	expected := asserts.NeverMatchAttributes
	if always {
		expected = asserts.AlwaysMatchAttributes
	}
	c.Assert(cstrs, HasLen, 1)
	cstrs1 := cstrs[0]
	c.Check(cstrs1.PlugAttributes, Equals, expected)
	c.Check(cstrs1.SlotAttributes, Equals, expected)
	if strings.HasPrefix(subrule, "deny-") {
		undef := asserts.SideArityConstraint{}
		c.Check(cstrs1.SlotsPerPlug, Equals, undef)
		c.Check(cstrs1.PlugsPerSlot, Equals, undef)
	} else {
		c.Check(cstrs1.PlugsPerSlot, Equals, sideArityAny)
		if strings.HasSuffix(subrule, "-auto-connection") {
			c.Check(cstrs1.SlotsPerPlug, Equals, sideArityOne)
		} else {
			c.Check(cstrs1.SlotsPerPlug, Equals, sideArityAny)
		}
	}
	c.Check(cstrs1.SlotSnapIDs, HasLen, 0)
	c.Check(cstrs1.SlotPublisherIDs, HasLen, 0)
	c.Check(cstrs1.SlotSnapTypes, HasLen, 0)
}

func checkBoolSlotConnConstraints(c *C, subrule string, cstrs []*asserts.SlotConnectionConstraints, always bool) {
	expected := asserts.NeverMatchAttributes
	if always {
		expected = asserts.AlwaysMatchAttributes
	}
	c.Assert(cstrs, HasLen, 1)
	cstrs1 := cstrs[0]
	c.Check(cstrs1.PlugAttributes, Equals, expected)
	c.Check(cstrs1.SlotAttributes, Equals, expected)
	if strings.HasPrefix(subrule, "deny-") {
		undef := asserts.SideArityConstraint{}
		c.Check(cstrs1.SlotsPerPlug, Equals, undef)
		c.Check(cstrs1.PlugsPerSlot, Equals, undef)
	} else {
		c.Check(cstrs1.PlugsPerSlot, Equals, sideArityAny)
		if strings.HasSuffix(subrule, "-auto-connection") {
			c.Check(cstrs1.SlotsPerPlug, Equals, sideArityOne)
		} else {
			c.Check(cstrs1.SlotsPerPlug, Equals, sideArityAny)
		}
	}
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
	checkBoolPlugConnConstraints(c, "allow-connection", rule.AllowConnection, true)
	checkBoolPlugConnConstraints(c, "deny-connection", rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolPlugConnConstraints(c, "allow-auto-connection", rule.AllowAutoConnection, true)
	checkBoolPlugConnConstraints(c, "deny-auto-connection", rule.DenyAutoConnection, false)
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
	checkBoolPlugConnConstraints(c, "allow-connection", rule.AllowConnection, false)
	checkBoolPlugConnConstraints(c, "deny-connection", rule.DenyConnection, true)
	// auto-connection subrules
	checkBoolPlugConnConstraints(c, "allow-auto-connection", rule.AllowAutoConnection, false)
	checkBoolPlugConnConstraints(c, "deny-auto-connection", rule.DenyAutoConnection, true)
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
	checkBoolPlugConnConstraints(c, "allow-connection", rule.AllowConnection, true)
	checkBoolPlugConnConstraints(c, "deny-connection", rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolPlugConnConstraints(c, "allow-auto-connection", rule.AllowAutoConnection, true)
	// ... but deny-auto-connection is on
	checkBoolPlugConnConstraints(c, "deny-auto-connection", rule.DenyAutoConnection, true)
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleInstalationConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompilePlugRule("iface", map[string]interface{}{
		"allow-installation": map[string]interface{}{
			"plug-snap-type": []interface{}{"core", "kernel", "gadget", "app"},
			"plug-snap-id":   []interface{}{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"},
		},
	})
	c.Assert(err, IsNil)

	c.Assert(rule.AllowInstallation, HasLen, 1)
	cstrs := rule.AllowInstallation[0]
	c.Check(cstrs.PlugSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
	c.Check(cstrs.PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
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

func (s *plugSlotRulesSuite) TestCompilePlugRuleInstallationConstraintsDeviceScope(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].DeviceScope, IsNil)

	tests := []struct {
		rule     string
		expected asserts.DeviceScopeConstraint
	}{
		{`iface:
  allow-installation:
    on-store:
      - my-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store"}}},
		{`iface:
  allow-installation:
    on-store:
      - my-store
      - other-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store", "other-store"}}},
		{`iface:
  allow-installation:
    on-brand:
      - my-brand
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT`, asserts.DeviceScopeConstraint{Brand: []string{"my-brand", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT"}}},
		{`iface:
  allow-installation:
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
		{`iface:
  allow-installation:
    on-store:
      - store1
      - store2
    on-brand:
      - my-brand
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{
			Store: []string{"store1", "store2"},
			Brand: []string{"my-brand"},
			Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
	}

	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowInstallation[0].DeviceScope, DeepEquals, &t.expected)
	}
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleInstallationConstraintsPlugNames(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].PlugNames, IsNil)

	tests := []struct {
		rule        string
		matching    []string
		notMatching []string
	}{
		{`iface:
  allow-installation:
    plug-names:
      - foo`, []string{"foo"}, []string{"bar"}},
		{`iface:
  allow-installation:
    plug-names:
      - foo
      - bar`, []string{"foo", "bar"}, []string{"baz"}},
		{`iface:
  allow-installation:
    plug-names:
      - foo[0-9]
      - bar`, []string{"foo0", "foo1", "bar"}, []string{"baz", "fooo", "foo12"}},
	}
	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		for _, matching := range t.matching {
			c.Check(rule.AllowInstallation[0].PlugNames.Check("plug name", matching, nil), IsNil)
		}
		for _, notMatching := range t.notMatching {
			c.Check(rule.AllowInstallation[0].PlugNames.Check("plug name", notMatching, nil), NotNil)
		}
	}
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

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsDeviceScope(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].DeviceScope, IsNil)

	tests := []struct {
		rule     string
		expected asserts.DeviceScopeConstraint
	}{
		{`iface:
  allow-connection:
    on-store:
      - my-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store"}}},
		{`iface:
  allow-connection:
    on-store:
      - my-store
      - other-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store", "other-store"}}},
		{`iface:
  allow-connection:
    on-brand:
      - my-brand
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT`, asserts.DeviceScopeConstraint{Brand: []string{"my-brand", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT"}}},
		{`iface:
  allow-connection:
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
		{`iface:
  allow-connection:
    on-store:
      - store1
      - store2
    on-brand:
      - my-brand
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{
			Store: []string{"store1", "store2"},
			Brand: []string{"my-brand"},
			Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
	}

	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowConnection[0].DeviceScope, DeepEquals, &t.expected)
	}
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsPlugNamesSlotNames(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].PlugNames, IsNil)
	c.Check(rule.AllowConnection[0].SlotNames, IsNil)

	tests := []struct {
		rule        string
		matching    []string
		notMatching []string
	}{
		{`iface:
  allow-connection:
    plug-names:
      - Pfoo
    slot-names:
      - Sfoo`, []string{"foo"}, []string{"bar"}},
		{`iface:
  allow-connection:
    plug-names:
      - Pfoo
      - Pbar
    slot-names:
      - Sfoo
      - Sbar`, []string{"foo", "bar"}, []string{"baz"}},
		{`iface:
  allow-connection:
    plug-names:
      - Pfoo[0-9]
      - Pbar
    slot-names:
      - Sfoo[0-9]
      - Sbar`, []string{"foo0", "foo1", "bar"}, []string{"baz", "fooo", "foo12"}},
	}
	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		for _, matching := range t.matching {
			c.Check(rule.AllowConnection[0].PlugNames.Check("plug name", "P"+matching, nil), IsNil)

			c.Check(rule.AllowConnection[0].SlotNames.Check("slot name", "S"+matching, nil), IsNil)
		}

		for _, notMatching := range t.notMatching {
			c.Check(rule.AllowConnection[0].SlotNames.Check("plug name", "P"+notMatching, nil), NotNil)

			c.Check(rule.AllowConnection[0].SlotNames.Check("slot name", "S"+notMatching, nil), NotNil)
		}
	}
}

func (s *plugSlotRulesSuite) TestCompilePlugRuleConnectionConstraintsSideArityConstraints(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-auto-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	// defaults
	c.Check(rule.AllowAutoConnection[0].SlotsPerPlug, Equals, asserts.SideArityConstraint{N: 1})
	c.Check(rule.AllowAutoConnection[0].PlugsPerSlot.Any(), Equals, true)

	c.Check(rule.AllowConnection[0].SlotsPerPlug.Any(), Equals, true)
	c.Check(rule.AllowConnection[0].PlugsPerSlot.Any(), Equals, true)

	// test that the arity constraints get normalized away to any
	// under allow-connection
	// see https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-plugs/12438
	allowConnTests := []string{
		`iface:
  allow-connection:
    slots-per-plug: 1
    plugs-per-slot: 2`,
		`iface:
  allow-connection:
    slots-per-plug: *
    plugs-per-slot: 1`,
		`iface:
  allow-connection:
    slots-per-plug: 2
    plugs-per-slot: *`,
	}

	for _, t := range allowConnTests {
		m, err = asserts.ParseHeaders([]byte(t))
		c.Assert(err, IsNil)

		rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowConnection[0].SlotsPerPlug.Any(), Equals, true)
		c.Check(rule.AllowConnection[0].PlugsPerSlot.Any(), Equals, true)
	}

	// test that under allow-auto-connection:
	// slots-per-plug can be * (any) or otherwise gets normalized to 1
	// plugs-per-slot gets normalized to any (*)
	// see https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-plugs/12438
	allowAutoConnTests := []struct {
		rule         string
		slotsPerPlug asserts.SideArityConstraint
	}{
		{`iface:
  allow-auto-connection:
    slots-per-plug: 1
    plugs-per-slot: 2`, sideArityOne},
		{`iface:
  allow-auto-connection:
    slots-per-plug: *
    plugs-per-slot: 1`, sideArityAny},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 2
    plugs-per-slot: *`, sideArityOne},
	}

	for _, t := range allowAutoConnTests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompilePlugRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowAutoConnection[0].SlotsPerPlug, Equals, t.slotsPerPlug)
		c.Check(rule.AllowAutoConnection[0].PlugsPerSlot.Any(), Equals, true)
	}
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
      - foo`, `allow-connection in plug rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  deny-connection:
    slot-snap-ids:
      - foo`, `deny-connection in plug rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  allow-auto-connection:
    slot-snap-ids:
      - foo`, `allow-auto-connection in plug rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  deny-auto-connection:
    slot-snap-ids:
      - foo`, `deny-auto-connection in plug rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, slot-publisher-id, slot-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  allow-connect: true`, `plug rule for interface "iface" must specify at least one of allow-installation, deny-installation, allow-connection, deny-connection, allow-auto-connection, deny-auto-connection`},
		{`iface:
  allow-installation:
    on-store: true`, `on-store in allow-installation in plug rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-installation:
    on-store: store1`, `on-store in allow-installation in plug rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-installation:
    on-store:
      - zoom!`, `on-store in allow-installation in plug rule for interface \"iface\" contains an invalid element: \"zoom!\"`},
		{`iface:
  allow-connection:
    on-brand: true`, `on-brand in allow-connection in plug rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-connection:
    on-brand: brand1`, `on-brand in allow-connection in plug rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-connection:
    on-brand:
      - zoom!`, `on-brand in allow-connection in plug rule for interface \"iface\" contains an invalid element: \"zoom!\"`},
		{`iface:
  allow-auto-connection:
    on-model: true`, `on-model in allow-auto-connection in plug rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-auto-connection:
    on-model: foo/bar`, `on-model in allow-auto-connection in plug rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-auto-connection:
    on-model:
      - foo/!qz`, `on-model in allow-auto-connection in plug rule for interface \"iface\" contains an invalid element: \"foo/!qz"`},
		{`iface:
  allow-installation:
    slots-per-plug: 1`, `allow-installation in plug rule for interface "iface" cannot specify a slots-per-plug constraint, they apply only to allow-\*connection`},
		{`iface:
  deny-connection:
    slots-per-plug: 1`, `deny-connection in plug rule for interface "iface" cannot specify a slots-per-plug constraint, they apply only to allow-\*connection`},
		{`iface:
  allow-auto-connection:
    plugs-per-slot: any`, `plugs-per-slot in allow-auto-connection in plug rule for interface "iface" must be an integer >=1 or \*`},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 00`, `slots-per-plug in allow-auto-connection in plug rule for interface "iface" has invalid prefix zeros: 00`},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 99999999999999999999`, `slots-per-plug in allow-auto-connection in plug rule for interface "iface" is out of range: 99999999999999999999`},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 0`, `slots-per-plug in allow-auto-connection in plug rule for interface "iface" must be an integer >=1 or \*`},
		{`iface:
  allow-auto-connection:
    slots-per-plug:
      what: 1`, `slots-per-plug in allow-auto-connection in plug rule for interface "iface" must be an integer >=1 or \*`},
		{`iface:
  allow-auto-connection:
    plug-names: true`, `plug-names constraints must be a list of regexps and special \$ values`},
		{`iface:
  allow-auto-connection:
    slot-names: true`, `slot-names constraints must be a list of regexps and special \$ values`},
	}

	for _, t := range tests {
		m, err := asserts.ParseHeaders([]byte(t.stanza))
		c.Assert(err, IsNil, Commentf(t.stanza))

		_, err = asserts.CompilePlugRule("iface", m["iface"])
		c.Check(err, ErrorMatches, t.err, Commentf(t.stanza))
	}
}

var (
	deviceScopeConstrs = map[string][]interface{}{
		"on-store": {"store"},
		"on-brand": {"brand"},
		"on-model": {"brand/model"},
	}
)

func (s *plugSlotRulesSuite) TestPlugRuleFeatures(c *C) {
	combos := []struct {
		subrule             string
		constraintsPrefixes []string
	}{
		{"allow-installation", []string{"plug-"}},
		{"deny-installation", []string{"plug-"}},
		{"allow-connection", []string{"plug-", "slot-"}},
		{"deny-connection", []string{"plug-", "slot-"}},
		{"allow-auto-connection", []string{"plug-", "slot-"}},
		{"deny-auto-connection", []string{"plug-", "slot-"}},
	}

	for _, combo := range combos {
		for _, attrConstrPrefix := range combo.constraintsPrefixes {
			attrConstraintMap := map[string]interface{}{
				"a":     "ATTR",
				"other": []interface{}{"x", "y"},
			}
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					attrConstrPrefix + "attributes": attrConstraintMap,
				},
			}

			rule, err := asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, false, Commentf("%v", ruleMap))

			c.Check(asserts.RuleFeature(rule, "device-scope-constraints"), Equals, false, Commentf("%v", ruleMap))
			c.Check(asserts.RuleFeature(rule, "name-constraints"), Equals, false, Commentf("%v", ruleMap))

			attrConstraintMap["a"] = "$MISSING"
			rule, err = asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, true, Commentf("%v", ruleMap))

			// covers also alternation
			attrConstraintMap["a"] = []interface{}{"$SLOT(a)"}
			rule, err = asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, true, Commentf("%v", ruleMap))

			c.Check(asserts.RuleFeature(rule, "device-scope-constraints"), Equals, false, Commentf("%v", ruleMap))
			c.Check(asserts.RuleFeature(rule, "name-constraints"), Equals, false, Commentf("%v", ruleMap))

		}

		for deviceScopeConstr, value := range deviceScopeConstrs {
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					deviceScopeConstr: value,
				},
			}

			rule, err := asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "device-scope-constraints"), Equals, true, Commentf("%v", ruleMap))
		}

		for _, nameConstrPrefix := range combo.constraintsPrefixes {
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					nameConstrPrefix + "names": []interface{}{"foo"},
				},
			}

			rule, err := asserts.CompilePlugRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "name-constraints"), Equals, true, Commentf("%v", ruleMap))
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
	checkBoolSlotConnConstraints(c, "allow-connection", rule.AllowConnection, true)
	checkBoolSlotConnConstraints(c, "deny-connection", rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolSlotConnConstraints(c, "allow-auto-connection", rule.AllowAutoConnection, true)
	checkBoolSlotConnConstraints(c, "deny-auto-connection", rule.DenyAutoConnection, false)
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
	checkBoolSlotConnConstraints(c, "allwo-connection", rule.AllowConnection, false)
	checkBoolSlotConnConstraints(c, "deny-connection", rule.DenyConnection, true)
	// auto-connection subrules
	checkBoolSlotConnConstraints(c, "allow-auto-connection", rule.AllowAutoConnection, false)
	checkBoolSlotConnConstraints(c, "deny-auto-connection", rule.DenyAutoConnection, true)
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
	checkBoolSlotConnConstraints(c, "allow-connection", rule.AllowConnection, true)
	checkBoolSlotConnConstraints(c, "deny-connection", rule.DenyConnection, false)
	// auto-connection subrules
	checkBoolSlotConnConstraints(c, "allow-auto-connection", rule.AllowAutoConnection, true)
	// ... but deny-auto-connection is on
	checkBoolSlotConnConstraints(c, "deny-auto-connection", rule.DenyAutoConnection, true)
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleInstallationConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompileSlotRule("iface", map[string]interface{}{
		"allow-installation": map[string]interface{}{
			"slot-snap-type": []interface{}{"core", "kernel", "gadget", "app"},
			"slot-snap-id":   []interface{}{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"},
		},
	})
	c.Assert(err, IsNil)

	c.Assert(rule.AllowInstallation, HasLen, 1)
	cstrs := rule.AllowInstallation[0]
	c.Check(cstrs.SlotSnapTypes, DeepEquals, []string{"core", "kernel", "gadget", "app"})
	c.Check(cstrs.SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
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

func (s *plugSlotRulesSuite) TestCompileSlotRuleInstallationConstraintsDeviceScope(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].DeviceScope, IsNil)

	tests := []struct {
		rule     string
		expected asserts.DeviceScopeConstraint
	}{
		{`iface:
  allow-installation:
    on-store:
      - my-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store"}}},
		{`iface:
  allow-installation:
    on-store:
      - my-store
      - other-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store", "other-store"}}},
		{`iface:
  allow-installation:
    on-brand:
      - my-brand
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT`, asserts.DeviceScopeConstraint{Brand: []string{"my-brand", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT"}}},
		{`iface:
  allow-installation:
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
		{`iface:
  allow-installation:
    on-store:
      - store1
      - store2
    on-brand:
      - my-brand
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{
			Store: []string{"store1", "store2"},
			Brand: []string{"my-brand"},
			Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
	}

	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowInstallation[0].DeviceScope, DeepEquals, &t.expected)
	}
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleInstallationConstraintsSlotNames(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-installation: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowInstallation[0].SlotNames, IsNil)

	tests := []struct {
		rule        string
		matching    []string
		notMatching []string
	}{
		{`iface:
  allow-installation:
    slot-names:
      - foo`, []string{"foo"}, []string{"bar"}},
		{`iface:
  allow-installation:
    slot-names:
      - foo
      - bar`, []string{"foo", "bar"}, []string{"baz"}},
		{`iface:
  allow-installation:
    slot-names:
      - foo[0-9]
      - bar`, []string{"foo0", "foo1", "bar"}, []string{"baz", "fooo", "foo12"}},
	}
	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		for _, matching := range t.matching {
			c.Check(rule.AllowInstallation[0].SlotNames.Check("slot name", matching, nil), IsNil)
		}
		for _, notMatching := range t.notMatching {
			c.Check(rule.AllowInstallation[0].SlotNames.Check("slot name", notMatching, nil), NotNil)
		}
	}
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleConnectionConstraintsIDConstraints(c *C) {
	rule, err := asserts.CompileSlotRule("iface", map[string]interface{}{
		"allow-connection": map[string]interface{}{
			"slot-snap-type":    []interface{}{"core"},
			"plug-snap-type":    []interface{}{"core", "kernel", "gadget", "app"},
			"plug-snap-id":      []interface{}{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"},
			"plug-publisher-id": []interface{}{"pubidpubidpubidpubidpubidpubid09", "canonical", "$SAME"},
		},
	})
	c.Assert(err, IsNil)

	c.Assert(rule.AllowConnection, HasLen, 1)
	cstrs := rule.AllowConnection[0]
	c.Check(cstrs.SlotSnapTypes, DeepEquals, []string{"core"})
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

func (s *plugSlotRulesSuite) TestCompileSlotRuleConnectionConstraintsDeviceScope(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].DeviceScope, IsNil)

	tests := []struct {
		rule     string
		expected asserts.DeviceScopeConstraint
	}{
		{`iface:
  allow-connection:
    on-store:
      - my-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store"}}},
		{`iface:
  allow-connection:
    on-store:
      - my-store
      - other-store`, asserts.DeviceScopeConstraint{Store: []string{"my-store", "other-store"}}},
		{`iface:
  allow-connection:
    on-brand:
      - my-brand
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT`, asserts.DeviceScopeConstraint{Brand: []string{"my-brand", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT"}}},
		{`iface:
  allow-connection:
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
		{`iface:
  allow-connection:
    on-store:
      - store1
      - store2
    on-brand:
      - my-brand
    on-model:
      - my-brand/bar
      - s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz`, asserts.DeviceScopeConstraint{
			Store: []string{"store1", "store2"},
			Brand: []string{"my-brand"},
			Model: []string{"my-brand/bar", "s9zGdwb16ysLeRW6nRivwZS5r9puP8JT/baz"}}},
	}

	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowConnection[0].DeviceScope, DeepEquals, &t.expected)
	}
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleConnectionConstraintsPlugNamesSlotNames(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	c.Check(rule.AllowConnection[0].PlugNames, IsNil)
	c.Check(rule.AllowConnection[0].SlotNames, IsNil)

	tests := []struct {
		rule        string
		matching    []string
		notMatching []string
	}{
		{`iface:
  allow-connection:
    plug-names:
      - Pfoo
    slot-names:
      - Sfoo`, []string{"foo"}, []string{"bar"}},
		{`iface:
  allow-connection:
    plug-names:
      - Pfoo
      - Pbar
    slot-names:
      - Sfoo
      - Sbar`, []string{"foo", "bar"}, []string{"baz"}},
		{`iface:
  allow-connection:
    plug-names:
      - Pfoo[0-9]
      - Pbar
    slot-names:
      - Sfoo[0-9]
      - Sbar`, []string{"foo0", "foo1", "bar"}, []string{"baz", "fooo", "foo12"}},
	}
	for _, t := range tests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		for _, matching := range t.matching {
			c.Check(rule.AllowConnection[0].PlugNames.Check("plug name", "P"+matching, nil), IsNil)

			c.Check(rule.AllowConnection[0].SlotNames.Check("slot name", "S"+matching, nil), IsNil)
		}

		for _, notMatching := range t.notMatching {
			c.Check(rule.AllowConnection[0].SlotNames.Check("plug name", "P"+notMatching, nil), NotNil)

			c.Check(rule.AllowConnection[0].SlotNames.Check("slot name", "S"+notMatching, nil), NotNil)
		}
	}
}

func (s *plugSlotRulesSuite) TestCompileSlotRuleConnectionConstraintsSideArityConstraints(c *C) {
	m, err := asserts.ParseHeaders([]byte(`iface:
  allow-auto-connection: true`))
	c.Assert(err, IsNil)

	rule, err := asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
	c.Assert(err, IsNil)

	// defaults
	c.Check(rule.AllowAutoConnection[0].SlotsPerPlug, Equals, asserts.SideArityConstraint{N: 1})
	c.Check(rule.AllowAutoConnection[0].PlugsPerSlot.Any(), Equals, true)

	c.Check(rule.AllowConnection[0].SlotsPerPlug.Any(), Equals, true)
	c.Check(rule.AllowConnection[0].PlugsPerSlot.Any(), Equals, true)

	// test that the arity constraints get normalized away to any
	// under allow-connection
	// see https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-plugs/12438
	allowConnTests := []string{
		`iface:
  allow-connection:
    slots-per-plug: 1
    plugs-per-slot: 2`,
		`iface:
  allow-connection:
    slots-per-plug: *
    plugs-per-slot: 1`,
		`iface:
  allow-connection:
    slots-per-plug: 2
    plugs-per-slot: *`,
	}

	for _, t := range allowConnTests {
		m, err = asserts.ParseHeaders([]byte(t))
		c.Assert(err, IsNil)

		rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowConnection[0].SlotsPerPlug.Any(), Equals, true)
		c.Check(rule.AllowConnection[0].PlugsPerSlot.Any(), Equals, true)
	}

	// test that under allow-auto-connection:
	// slots-per-plug can be * (any) or otherwise gets normalized to 1
	// plugs-per-slot gets normalized to any (*)
	// see https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-plugs/12438
	allowAutoConnTests := []struct {
		rule         string
		slotsPerPlug asserts.SideArityConstraint
	}{
		{`iface:
  allow-auto-connection:
    slots-per-plug: 1
    plugs-per-slot: 2`, sideArityOne},
		{`iface:
  allow-auto-connection:
    slots-per-plug: *
    plugs-per-slot: 1`, sideArityAny},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 2
    plugs-per-slot: *`, sideArityOne},
	}

	for _, t := range allowAutoConnTests {
		m, err = asserts.ParseHeaders([]byte(t.rule))
		c.Assert(err, IsNil)

		rule, err = asserts.CompileSlotRule("iface", m["iface"].(map[string]interface{}))
		c.Assert(err, IsNil)

		c.Check(rule.AllowAutoConnection[0].SlotsPerPlug, Equals, t.slotsPerPlug)
		c.Check(rule.AllowAutoConnection[0].PlugsPerSlot.Any(), Equals, true)
	}
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
    slot-snap-type:
      - foo`, `slot-snap-type in allow-connection in slot rule for interface "iface" contains an invalid element: "foo"`},
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
      - foo`, `allow-connection in slot rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, plug-snap-type, plug-publisher-id, plug-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  deny-connection:
    plug-snap-ids:
      - foo`, `deny-connection in slot rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, plug-snap-type, plug-publisher-id, plug-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  allow-auto-connection:
    plug-snap-ids:
      - foo`, `allow-auto-connection in slot rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, plug-snap-type, plug-publisher-id, plug-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  deny-auto-connection:
    plug-snap-ids:
      - foo`, `deny-auto-connection in slot rule for interface "iface" must specify at least one of plug-names, slot-names, plug-attributes, slot-attributes, slot-snap-type, plug-snap-type, plug-publisher-id, plug-snap-id, slots-per-plug, plugs-per-slot, on-classic, on-core-desktop, on-store, on-brand, on-model`},
		{`iface:
  allow-connect: true`, `slot rule for interface "iface" must specify at least one of allow-installation, deny-installation, allow-connection, deny-connection, allow-auto-connection, deny-auto-connection`},
		{`iface:
  allow-installation:
    on-store: true`, `on-store in allow-installation in slot rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-installation:
    on-store: store1`, `on-store in allow-installation in slot rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-installation:
    on-store:
      - zoom!`, `on-store in allow-installation in slot rule for interface \"iface\" contains an invalid element: \"zoom!\"`},
		{`iface:
  allow-connection:
    on-brand: true`, `on-brand in allow-connection in slot rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-connection:
    on-brand: brand1`, `on-brand in allow-connection in slot rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-connection:
    on-brand:
      - zoom!`, `on-brand in allow-connection in slot rule for interface \"iface\" contains an invalid element: \"zoom!\"`},
		{`iface:
  allow-auto-connection:
    on-model: true`, `on-model in allow-auto-connection in slot rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-auto-connection:
    on-model: foo/bar`, `on-model in allow-auto-connection in slot rule for interface \"iface\" must be a list of strings`},
		{`iface:
  allow-auto-connection:
    on-model:
      - foo//bar`, `on-model in allow-auto-connection in slot rule for interface \"iface\" contains an invalid element: \"foo//bar"`},
		{`iface:
  allow-installation:
    slots-per-plug: 1`, `allow-installation in slot rule for interface "iface" cannot specify a slots-per-plug constraint, they apply only to allow-\*connection`},
		{`iface:
  deny-auto-connection:
    slots-per-plug: 1`, `deny-auto-connection in slot rule for interface "iface" cannot specify a slots-per-plug constraint, they apply only to allow-\*connection`},
		{`iface:
  allow-auto-connection:
    plugs-per-slot: any`, `plugs-per-slot in allow-auto-connection in slot rule for interface "iface" must be an integer >=1 or \*`},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 00`, `slots-per-plug in allow-auto-connection in slot rule for interface "iface" has invalid prefix zeros: 00`},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 99999999999999999999`, `slots-per-plug in allow-auto-connection in slot rule for interface "iface" is out of range: 99999999999999999999`},
		{`iface:
  allow-auto-connection:
    slots-per-plug: 0`, `slots-per-plug in allow-auto-connection in slot rule for interface "iface" must be an integer >=1 or \*`},
		{`iface:
  allow-auto-connection:
    slots-per-plug:
      what: 1`, `slots-per-plug in allow-auto-connection in slot rule for interface "iface" must be an integer >=1 or \*`},
		{`iface:
  allow-auto-connection:
    plug-names: true`, `plug-names constraints must be a list of regexps and special \$ values`},
		{`iface:
  allow-auto-connection:
    slot-names: true`, `slot-names constraints must be a list of regexps and special \$ values`},
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
		subrule             string
		constraintsPrefixes []string
	}{
		{"allow-installation", []string{"slot-"}},
		{"deny-installation", []string{"slot-"}},
		{"allow-connection", []string{"plug-", "slot-"}},
		{"deny-connection", []string{"plug-", "slot-"}},
		{"allow-auto-connection", []string{"plug-", "slot-"}},
		{"deny-auto-connection", []string{"plug-", "slot-"}},
	}

	for _, combo := range combos {
		for _, attrConstrPrefix := range combo.constraintsPrefixes {
			attrConstraintMap := map[string]interface{}{
				"a": "ATTR",
			}
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					attrConstrPrefix + "attributes": attrConstraintMap,
				},
			}

			rule, err := asserts.CompileSlotRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, false, Commentf("%v", ruleMap))

			c.Check(asserts.RuleFeature(rule, "device-scope-constraints"), Equals, false, Commentf("%v", ruleMap))

			attrConstraintMap["a"] = "$PLUG(a)"
			rule, err = asserts.CompileSlotRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "dollar-attr-constraints"), Equals, true, Commentf("%v", ruleMap))

			c.Check(asserts.RuleFeature(rule, "device-scope-constraints"), Equals, false, Commentf("%v", ruleMap))
		}

		for deviceScopeConstr, value := range deviceScopeConstrs {
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					deviceScopeConstr: value,
				},
			}

			rule, err := asserts.CompileSlotRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "device-scope-constraints"), Equals, true, Commentf("%v", ruleMap))
		}

		for _, nameConstrPrefix := range combo.constraintsPrefixes {
			ruleMap := map[string]interface{}{
				combo.subrule: map[string]interface{}{
					nameConstrPrefix + "names": []interface{}{"foo"},
				},
			}

			rule, err := asserts.CompileSlotRule("iface", ruleMap)
			c.Assert(err, IsNil)
			c.Check(asserts.RuleFeature(rule, "name-constraints"), Equals, true, Commentf("%v", ruleMap))
		}

	}
}

func (s *plugSlotRulesSuite) TestValidOnStoreBrandModel(c *C) {
	// more extensive testing is now done in deviceScopeConstraintSuite
	tests := []struct {
		constr string
		value  string
		valid  bool
	}{
		{"on-store", "", false},
		{"on-store", "foo", true},
		{"on-store", "F_o-O88", true},
		{"on-store", "foo!", false},
		{"on-brand", "", false},
		// custom set brands (length 2-28)
		{"on-brand", "dwell", true},
		{"on-brand", "Dwell", false},
		{"on-brand", "dwell-88", true},
		{"on-brand", "dwell_88", false},
		{"on-brand", "0123456789012345678901234567", true},
		// snappy id brands (fixed length 32)
		{"on-brand", "01234567890123456789012345678", false},
		{"on-brand", "01234567890123456789012345678901", true},
		{"on-model", "", false},
		{"on-model", "dwell/dwell1", true},
		{"on-model", "dwell", false},
		{"on-model", "dwell/", false},
	}

	check := func(constr, value string, valid bool) {
		ruleMap := map[string]interface{}{
			"allow-auto-connection": map[string]interface{}{
				constr: []interface{}{value},
			},
		}

		_, err := asserts.CompilePlugRule("iface", ruleMap)
		if valid {
			c.Check(err, IsNil, Commentf("%v", ruleMap))
		} else {
			c.Check(err, ErrorMatches, fmt.Sprintf(`%s in allow-auto-connection in plug rule for interface "iface" contains an invalid element: %q`, constr, value), Commentf("%v", ruleMap))
		}
	}

	for _, t := range tests {
		check(t.constr, t.value, t.valid)

		if t.constr == "on-brand" {
			// reuse and double check all brands also in the context of on-model!

			check("on-model", t.value+"/foo", t.valid)
		}
	}
}
