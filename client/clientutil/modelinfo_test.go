// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018-2020 Canonical Ltd
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

package clientutil_test

import (
	"bytes"
	"text/tabwriter"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/testutil"
)

type modelInfoSuite struct {
	testutil.BaseTest
}

func (s *modelInfoSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

var _ = Suite(&modelInfoSuite{})

func (*modelInfoSuite) TestBasicObjectAnonymousRootYaml(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_YAML_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, "foo:bar\nbaz:qux\n")
}

func (*modelInfoSuite) TestBasicObjectYaml(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_YAML_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("project")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, "project:\n  foo:\tbar\n  baz:\tqux\n")
}

func (*modelInfoSuite) TestNestedObjectYaml(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_YAML_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("project")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartObject("sub")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `project:
  foo:	bar
  baz:	qux
  sub:
    foo:bar
    baz:qux
`)
}

func (*modelInfoSuite) TestNestedObjectWithArrayYaml(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_YAML_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("project")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartObject("sub")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartArray("items", false)
	writer.WriteStringValue("item1")
	writer.WriteStringValue("item2")
	writer.EndArray()
	writer.WriteStringPair("xxx", "yyy")
	writer.EndObject()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `project:
  foo:	bar
  baz:	qux
  sub:
    foo:	bar
    baz:	qux
    items:	
      - item1
      - item2
    xxx:yyy
`)
}

func (*modelInfoSuite) TestArrayOfObjectsYaml(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_YAML_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("project")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartArray("objects", false)
	writer.StartObject("object1")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()
	writer.StartObject("object2")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()
	writer.EndArray()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `project:
  foo:		bar
  baz:		qux
  objects:	
    - name:	object1
      foo:	bar
      baz:	qux
    - name:	object2
      foo:	bar
      baz:	qux
`)
}

func (*modelInfoSuite) TestObjectWithArrayYaml(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_YAML_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("project")
	writer.StartArray("foo", false)
	writer.WriteStringValue("bar")
	writer.WriteStringValue("baz")

	// this should have no effect as it's not allowed to write pairs
	// in arrays, instead we should use StartObject
	writer.WriteStringPair("qux", "quux")

	writer.EndArray()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `project:
  foo:	
    - bar
    - baz
`)
}

func (*modelInfoSuite) TestObjectWithInlineArrayYaml(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_YAML_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("project")
	writer.StartArray("foo", true)
	writer.WriteStringValue("bar")
	writer.WriteStringValue("baz")

	// this should have no effect as it's not allowed to write pairs
	// in arrays, instead we should use StartObject
	writer.WriteStringPair("qux", "quux")

	writer.EndArray()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `project:
  foo:	[bar, baz]
`)
}

func (*modelInfoSuite) TestBasicObjectAnonymousRootJson(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_JSON_FORMAT)

	writer.StartObject("")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, "{\n  \"foo\": \"bar\",\n  \"baz\": \"qux\"\n}\n")
}

func (*modelInfoSuite) TestBasicObjectJson(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_JSON_FORMAT)

	// the root object of json is ignored always, as json always has a anonymous root
	writer.StartObject("")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, "{\n  \"foo\": \"bar\",\n  \"baz\": \"qux\"\n}\n")
}

func (*modelInfoSuite) TestNestedObjectJson(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_JSON_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartObject("sub")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `{
  "foo": "bar",
  "baz": "qux",
  "sub": {
    "foo": "bar",
    "baz": "qux"
  }
}
`)
}

func (*modelInfoSuite) TestNestedObjectWithArrayJson(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_JSON_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartObject("sub")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartArray("items", false)
	writer.WriteStringValue("item1")
	writer.WriteStringValue("item2")
	writer.EndArray()
	writer.WriteStringPair("xxx", "yyy")
	writer.EndObject()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `{
  "foo": "bar",
  "baz": "qux",
  "sub": {
    "foo": "bar",
    "baz": "qux",
    "items": [
      "item1",
      "item2"
    ],
    "xxx": "yyy"
  }
}
`)
}

func (*modelInfoSuite) TestArrayOfObjectsJson(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_JSON_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.StartArray("objects", false)
	writer.StartObject("object1")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()
	writer.StartObject("object2")
	writer.WriteStringPair("foo", "bar")
	writer.WriteStringPair("baz", "qux")
	writer.EndObject()
	writer.EndArray()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `{
  "foo": "bar",
  "baz": "qux",
  "objects": [
    {
      "name": "object1",
      "foo": "bar",
      "baz": "qux"
    },
    {
      "name": "object2",
      "foo": "bar",
      "baz": "qux"
    }
  ]
}
`)
}

func (*modelInfoSuite) TestObjectWithArrayJson(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_JSON_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("")
	writer.StartArray("foo", false)
	writer.WriteStringValue("bar")
	writer.WriteStringValue("baz")

	// this should have no effect as it's not allowed to write pairs
	// in arrays, instead we should use StartObject
	writer.WriteStringPair("qux", "quux")

	writer.EndArray()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `{
  "foo": [
    "bar",
    "baz"
  ]
}
`)
}

func (*modelInfoSuite) TestObjectWithInlineArrayJson(c *C) {
	var buffer bytes.Buffer
	var tbw tabwriter.Writer
	tbw.Init(&buffer, 4, 4, 0, '\t', 0)

	writer := clientutil.NewModelWriter(&tbw, clientutil.MODELWRITER_JSON_FORMAT)

	// test basic object functionality with yaml, with empty name to mark
	// the root object as root.
	writer.StartObject("")
	writer.StartArray("foo", true)
	writer.WriteStringValue("bar")
	writer.WriteStringValue("baz")

	// this should have no effect as it's not allowed to write pairs
	// in arrays, instead we should use StartObject
	writer.WriteStringPair("qux", "quux")

	writer.EndArray()
	writer.EndObject()

	tbw.Flush()
	c.Check(buffer.String(), Equals, `{
  "foo": [
    "bar",
    "baz"
  ]
}
`)
}
