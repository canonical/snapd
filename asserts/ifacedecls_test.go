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
	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
)

type attrConstraintsSuite struct{}

var _ = Suite(&attrConstraintsSuite{})

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

	cstrs, err := asserts.CompileAttributeContraints(m["attrs"].(map[string]interface{}))
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
	c.Check(err, ErrorMatches, `attribute "bar" value "BAZ" does not match \^BAR\$`)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"baz": "BAZ",
	})
	c.Check(err, ErrorMatches, `attribute "bar" has constraints but is unset`)
}

func (s *attrConstraintsSuite) TestNested(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar:
    bar1: BAR1
    bar2: BAR2`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeContraints(m["attrs"].(map[string]interface{}))
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
	c.Check(err, ErrorMatches, `attribute "bar\.bar2" value "BAR22" does not match \^BAR2\$`)

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

	cstrs, err := asserts.CompileAttributeContraints(m["attrs"].([]interface{}))
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
	c.Check(err, ErrorMatches, `no alternative matches: attribute "bar" value "BARR" does not match \^BAR\$`)
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

	cstrs, err := asserts.CompileAttributeContraints(m["attrs"].(map[string]interface{}))
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
	c.Check(err, ErrorMatches, `no alternative for attribute "bar\.bar2" matches: attribute "bar\.bar2" value "BAR3" does not match \^BAR2\$`)
}

func (s *attrConstraintsSuite) TestOtherScalars(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: 1
  bar: true`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeContraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: 1
bar: true
`))
	c.Check(err, IsNil)
}

func (s *attrConstraintsSuite) TestCompileErrors(c *C) {
	_, err := asserts.CompileAttributeContraints(map[string]interface{}{
		"foo": "[",
	})
	c.Check(err, ErrorMatches, `cannot compile "foo" constraint "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttributeContraints(map[string]interface{}{
		"foo": []interface{}{"foo", "["},
	})
	c.Check(err, ErrorMatches, `cannot compile "foo/alt#2/" constraint "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttributeContraints(map[string]interface{}{
		"foo": []interface{}{"foo", []interface{}{"bar", "baz"}},
	})
	c.Check(err, ErrorMatches, `cannot nest alternative constraints directly at "foo/alt#2/"`)

	_, err = asserts.CompileAttributeContraints("FOO")
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)

	_, err = asserts.CompileAttributeContraints([]interface{}{"FOO"})
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)
}

func (s *attrConstraintsSuite) TestMatchingListsSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo: /foo/.*`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeContraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: ["/foo/x", "/foo/y"]
`))
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: ["/foo/x", "/foo"]
`))
	c.Check(err, ErrorMatches, `attribute "foo\.1" value "/foo" does not match \^/foo/\.\*\$`)
}

func (s *attrConstraintsSuite) TestMatchingListsMap(c *C) {
	m, err := asserts.ParseHeaders([]byte(`attrs:
  foo:
    p: /foo/.*`))
	c.Assert(err, IsNil)

	cstrs, err := asserts.CompileAttributeContraints(m["attrs"].(map[string]interface{}))
	c.Assert(err, IsNil)

	err = cstrs.Check(attrs(`
foo: [{p: "/foo/x"}, {p: "/foo/y"}]
`))
	c.Check(err, IsNil)

	err = cstrs.Check(attrs(`
foo: [{p: "zzz"}, {p: "/foo/y"}]
`))
	c.Check(err, ErrorMatches, `attribute "foo\.0\.p" value "zzz" does not match \^/foo/\.\*\$`)
}
