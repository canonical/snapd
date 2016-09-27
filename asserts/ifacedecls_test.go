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
	c.Check(err, ErrorMatches, `"bar" mismatch: "BAZ" does not match \^BAR\$`)

	err = cstrs.Check(map[string]interface{}{
		"foo": "FOO",
		"baz": "BAZ",
	})
	c.Check(err, ErrorMatches, `"bar" has constraints but is unset`)
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
	c.Check(err, ErrorMatches, `"bar" mismatch: cannot match key-value constraints against: BAZ`)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
  bar3: BAR3
baz: BAZ
`))
	c.Check(err, ErrorMatches, `"bar" mismatch: "bar2" mismatch: "BAR22" does not match \^BAR2\$`)

	err = cstrs.Check(attrs(`
foo: FOO
bar:
  bar1: BAR1
  bar2:
    bar22: true
  bar3: BAR3
baz: BAZ
`))
	c.Check(err, ErrorMatches, `"bar" mismatch: "bar2" mismatch: cannot match regexp constraint against:.*`)
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
	c.Check(err, ErrorMatches, `no alternative matches: "bar" mismatch: "BARR" does not match \^BAR\$`)
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
	c.Check(err, ErrorMatches, `"bar" mismatch: "bar2" mismatch: no alternative matches: "BAR3" does not match \^BAR2\$`)
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
	c.Check(err, ErrorMatches, `constraint for "foo": cannot compile "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttributeContraints(map[string]interface{}{
		"foo": []interface{}{"foo", "["},
	})
	c.Check(err, ErrorMatches, `constraint for "foo": alternative 2: cannot compile "\[": error parsing regexp:.*`)

	_, err = asserts.CompileAttributeContraints("FOO")
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)

	_, err = asserts.CompileAttributeContraints([]interface{}{"FOO"})
	c.Check(err, ErrorMatches, `alternative 1: first level of non alternative constraints must be a set of key-value contraints`)
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
	c.Check(err, ErrorMatches, `"foo" mismatch: element 1: "/foo" does not match \^/foo/\.\*\$`)
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
	c.Check(err, ErrorMatches, `"foo" mismatch: element 0: "p" mismatch: "zzz" does not match \^/foo/\.\*\$`)
}
