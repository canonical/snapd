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

	"github.com/snapcore/snapd/asserts"
)

type headersSuite struct{}

var _ = Suite(&headersSuite{})

func (s *headersSuite) TestParseHeadersSimple(c *C) {
	m, err := asserts.ParseHeaders([]byte(`foo: 1
bar: baz`))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": "1",
		"bar": "baz",
	})
}

func (s *headersSuite) TestParseHeadersMultiline(c *C) {
	m, err := asserts.ParseHeaders([]byte(`foo:
    abc
    
bar: baz`))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": "abc\n",
		"bar": "baz",
	})

	// XXX: do we want to support exactly this?
	m, err = asserts.ParseHeaders([]byte(`foo: 1
bar:
    baz`))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": "1",
		"bar": "baz",
	})

	m, err = asserts.ParseHeaders([]byte(`foo: 1
bar:
    baz
    `))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": "1",
		"bar": "baz\n",
	})

	m, err = asserts.ParseHeaders([]byte(`foo: 1
bar:
    baz
    
    baz2`))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": "1",
		"bar": "baz\n\nbaz2",
	})
}

func (s *headersSuite) TestParseHeadersSimpleList(c *C) {
	m, err := asserts.ParseHeaders([]byte(`foo:
  - x
  - y
  - z
bar: baz`))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": []interface{}{"x", "y", "z"},
		"bar": "baz",
	})
}

func (s *headersSuite) TestParseHeadersListNestedMultiline(c *C) {
	// XXX: could support skipping one level of indent for multiline text
	// when in another nesting structure, but less consistent?
	m, err := asserts.ParseHeaders([]byte(`foo:
  - x
  -
        y1
        y2
        
  - z
bar: baz`))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": []interface{}{"x", "y1\ny2\n", "z"},
		"bar": "baz",
	})

	m, err = asserts.ParseHeaders([]byte(`bar: baz
foo:
  -
      - u1
      - u2
  -
        y1
        y2
        `))
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]interface{}{
		"foo": []interface{}{[]interface{}{"u1", "u2"}, "y1\ny2\n"},
		"bar": "baz",
	})
}
