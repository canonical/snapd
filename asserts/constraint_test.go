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

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type attrMatcherSuite struct {
	testutil.BaseTest
}

var _ = Suite(&attrMatcherSuite{})

func vals(yml string) map[string]interface{} {
	var vs map[string]interface{}
	err := yaml.Unmarshal([]byte(yml), &vs)
	if err != nil {
		panic(err)
	}
	v, err := metautil.NormalizeValue(vs)
	if err != nil {
		panic(err)
	}
	return v.(map[string]interface{})
}

func (s *attrMatcherSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *attrMatcherSuite) TestSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar: BAR`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	values := map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	}
	err = domatch(values, nil)
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	}
	err = domatch(values, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BAZ" does not match \^\(BAR\)\$`)

	values = map[string]interface{}{
		"foo": "FOO",
		"baz": "BAZ",
	}
	err = domatch(values, nil)
	c.Check(err, ErrorMatches, `attribute "bar" has constraints but is unset`)
}

func (s *attrMatcherSuite) TestSimpleAnchorsVsRegexpAlt(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  bar: BAR|BAZ`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	values := map[string]interface{}{
		"bar": "BAR",
	}
	err = domatch(values, nil)
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"bar": "BARR",
	}
	err = domatch(values, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BARR" does not match \^\(BAR|BAZ\)\$`)

	values = map[string]interface{}{
		"bar": "BBAZ",
	}
	err = domatch(values, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BAZZ" does not match \^\(BAR|BAZ\)\$`)

	values = map[string]interface{}{
		"bar": "BABAZ",
	}
	err = domatch(values, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BABAZ" does not match \^\(BAR|BAZ\)\$`)

	values = map[string]interface{}{
		"bar": "BARAZ",
	}
	err = domatch(values, nil)
	c.Check(err, ErrorMatches, `attribute "bar" value "BARAZ" does not match \^\(BAR|BAZ\)\$`)
}

func (s *attrMatcherSuite) TestNested(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar:
    bar1: BAR1
    bar2: BAR2`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR2
  bar3: BAR3
baz: BAZ
`), nil)
	c.Check(err, IsNil)

	err = domatch(vals(`
foo: FOO
bar: BAZ
baz: BAZ
`), nil)
	c.Check(err, ErrorMatches, `attribute "bar" must be a map`)

	err = domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
  bar3: BAR3
baz: BAZ
`), nil)
	c.Check(err, ErrorMatches, `attribute "bar\.bar2" value "BAR22" does not match \^\(BAR2\)\$`)

	err = domatch(vals(`
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

func (s *attrMatcherSuite) TestAlternative(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  -
    foo: FOO
    bar: BAR
  -
    foo: FOO
    bar: BAZ`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"], nil)
	c.Assert(err, IsNil)

	values := map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	}
	err = domatch(values, nil)
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	}
	err = domatch(values, nil)
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"foo": "FOO",
		"bar": "BARR",
		"baz": "BAR",
	}
	err = domatch(values, nil)
	c.Check(err, ErrorMatches, `no alternative matches: attribute "bar" value "BARR" does not match \^\(BAR\)\$`)
}

func (s *attrMatcherSuite) TestNestedAlternative(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar:
    bar1: BAR1
    bar2:
      - BAR2
      - BAR22`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR2
`), nil)
	c.Check(err, IsNil)

	err = domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
`), nil)
	c.Check(err, IsNil)

	err = domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR3
`), nil)
	c.Check(err, ErrorMatches, `no alternative for attribute "bar\.bar2" matches: attribute "bar\.bar2" value "BAR3" does not match \^\(BAR2\)\$`)
}

func (s *attrMatcherSuite) TestAlternativeMatchingStringList(c *C) {
	toMatch := vals(`
write:
 - /var/tmp
 - /var/lib/snapd/snapshots
`)
	m, err := asserts.ParseHeaders([]byte(`attrs:
  write: /var/(tmp|lib/snapd/snapshots)`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(toMatch, nil)
	c.Check(err, IsNil)

	m, err = asserts.ParseHeaders([]byte(`attrs:
  write:
    - /var/tmp
    - /var/lib/snapd/snapshots`))
	c.Assert(err, IsNil)

	domatchLst, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatchLst(toMatch, nil)
	c.Check(err, IsNil)
}

func (s *attrMatcherSuite) TestAlternativeMatchingComplex(c *C) {
	toMatch := vals(`
mnt: [{what: "/dev/x*", where: "/foo/*", options: ["rw", "nodev"]}, {what: "/bar/*", where: "/baz/*", options: ["rw", "bind"]}]
`)

	m, err := asserts.ParseHeaders([]byte(`attrs:
  mnt:
    -
      what: /(bar/|dev/x)\*
      where: /(foo|baz)/\*
      options: rw|bind|nodev`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(toMatch, nil)
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

	domatchExtensive, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatchExtensive(toMatch, nil)
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

	domatchExtensiveNoMatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatchExtensiveNoMatch(toMatch, nil)
	c.Check(err, ErrorMatches, `no alternative for attribute "mnt\.0" matches: no alternative for attribute "mnt\.0.options\.1" matches:.*`)
}

func (s *attrMatcherSuite) TestOtherScalars(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: 1
  bar: true`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(vals(`
foo: 1
bar: true
`), nil)
	c.Check(err, IsNil)

	values := map[string]interface{}{
		"foo": int64(1),
		"bar": true,
	}
	err = domatch(values, nil)
	c.Check(err, IsNil)
}

func (s *attrMatcherSuite) TestCompileErrors(c *C) {
	_, err := asserts.CompileAttrMatcher(1, nil)
	c.Check(err, ErrorMatches, `top constraint must be a key-value map, regexp or a list of alternative constraints: 1`)

	_, err = asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": 1,
	}, nil)
	c.Check(err, ErrorMatches, `constraint "foo" must be a key-value map, regexp or a list of alternative constraints: 1`)

	_, err = asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": "[",
	}, nil)
	c.Check(err, ErrorMatches, `cannot compile "foo" constraint "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": []interface{}{"foo", "["},
	}, nil)
	c.Check(err, ErrorMatches, `cannot compile "foo/alt#2/" constraint "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": []interface{}{"foo", []interface{}{"bar", "baz"}},
	}, nil)
	c.Check(err, ErrorMatches, `cannot nest alternative constraints directly at "foo/alt#2/"`)

	_, err = asserts.CompileAttrMatcher("FOO", nil)
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)

	_, err = asserts.CompileAttrMatcher([]interface{}{"FOO"}, nil)
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)

	_, err = asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": "$FOO()",
	}, nil)
	c.Check(err, ErrorMatches, `cannot compile "foo" constraint "\$FOO\(\)": no \$OP\(\) constraints supported`)

	wrongDollarConstraints := []string{
		"$",
		"$FOO(a)",
		"$SLOT",
		"$SLOT()",
		"$SLOT(x,y)",
		"$SLOT(x,y,z)",
	}

	for _, wrong := range wrongDollarConstraints {
		_, err := asserts.CompileAttrMatcher(map[string]interface{}{
			"foo": wrong,
		}, []string{"SLOT", "OP"})
		if wrong != "$SLOT(x,y)" {
			c.Check(err, ErrorMatches, fmt.Sprintf(`cannot compile "foo" constraint "%s": not a valid \$SLOT\(\)/\$OP\(\) constraint`, regexp.QuoteMeta(wrong)))
		} else {
			c.Check(err, ErrorMatches, fmt.Sprintf(`cannot compile "foo" constraint "%s": \$SLOT\(\) constraint expects 1 argument`, regexp.QuoteMeta(wrong)))
		}

	}
}

func (s *attrMatcherSuite) TestMatchingListsSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: /foo/.*`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(vals(`
foo: ["/foo/x", "/foo/y"]
`), nil)
	c.Check(err, IsNil)

	err = domatch(vals(`
foo: ["/foo/x", "/foo"]
`), nil)
	c.Check(err, ErrorMatches, `attribute "foo\.1" value "/foo" does not match \^\(/foo/\.\*\)\$`)
}

func (s *attrMatcherSuite) TestMissingCheck(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: $MISSING`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(vals(`
bar: baz
`), nil)
	c.Check(err, IsNil)

	err = domatch(vals(`
foo: ["x"]
`), nil)
	c.Check(err, ErrorMatches, `attribute "foo" is constrained to be missing but is set`)
}

func (s *attrMatcherSuite) TestEvalCheck(c *C) {
	// TODO: consider rewriting once we have $WITHIN
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: $SLOT(foo)
  bar: $PLUG(bar.baz)`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), []string{"SLOT", "PLUG"})
	c.Assert(err, IsNil)

	err = domatch(vals(`
foo: foo
bar: bar
`), nil)
	c.Check(err, ErrorMatches, `attribute "(foo|bar)" cannot be matched without context`)

	calls := make(map[[2]string]bool)
	comp1 := func(op string, arg string) (interface{}, error) {
		calls[[2]string{op, arg}] = true
		return arg, nil
	}

	err = domatch(vals(`
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

	err = domatch(vals(`
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

	err = domatch(vals(`
foo: foo
bar: bar.baz
`), testEvalAttr{comp3})
	c.Check(err, ErrorMatches, `attribute "foo" does not match \$SLOT\(foo\): foo != other-value`)
}

func (s *attrMatcherSuite) TestMatchingListsMap(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo:
    p: /foo/.*`))
	c.Assert(err, IsNil)

	domatch, err := asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil)
	c.Assert(err, IsNil)

	err = domatch(vals(`
foo: [{p: "/foo/x"}, {p: "/foo/y"}]
`), nil)
	c.Check(err, IsNil)

	err = domatch(vals(`
foo: [{p: "zzz"}, {p: "/foo/y"}]
`), nil)
	c.Check(err, ErrorMatches, `attribute "foo\.0\.p" value "zzz" does not match \^\(/foo/\.\*\)\$`)
}
