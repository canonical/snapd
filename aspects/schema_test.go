// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package aspects_test

import (
	"fmt"

	"github.com/snapcore/snapd/aspects"
	. "gopkg.in/check.v1"
)

type schemaSuite struct{}

var _ = Suite(&schemaSuite{})

func (*schemaSuite) TestTopLevelFailsWithoutSchema(c *C) {
	schemaStr := []byte(`{
  "keys": "string"
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema: must have a "schema" constraint`)
}

func (*schemaSuite) TestSchemaMustBeMap(c *C) {
	schemaStr := []byte(`["foo"]`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema: must be a map`)
}

func (*schemaSuite) TestMapWithSchemaConstraint(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "snaps": {
			"type": "map",
			"schema": {
				"foo": "string",
				"bar": "string"
			}
    }
  }
}`)

	input := []byte(`{
  "snaps": {
    "foo": "abc",
		"bar": "cba"
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapWithKeysStringConstraintHappy(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "snaps": {
      "keys": "string"
    }
  }
}`)

	input := []byte(`{
  "snaps": {
    "foo": "bar"
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapWithKeysConstraintAsMap(c *C) {
	// the map constraining "keys" is assumed to be based on type string
	schemaStr := []byte(`{
  "schema": {
    "snaps": {
      "keys": {
			}
    }
  }
}`)

	input := []byte(`{
  "snaps": {
    "foo": "bar"
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapKeysConstraintMustBeStringBased(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "snaps": {
      "keys": {
				"type": "map"
			}
    }
  }
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: must be based on string but got "map"`)

	schemaStr = []byte(`{
  "schema": {
    "snaps": {
      "keys": "int"
		}
  }
}`)

	_, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: must be based on string but got "int"`)
}

// TODO: once string constraints are supported, test that keys with unmet constraints
// fail during validation (can't test now because we can't express constraints)

func (*schemaSuite) TestMapWithValuesStringConstraintHappy(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "snaps": {
      "values": "string"
    }
  }
}`)

	input := []byte(`{
  "snaps": {
    "foo": "bar"
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapWithUnmetValuesConstraint(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "snaps": {
      "values": "string"
    }
  }
}`)

	input := []byte(`{
  "snaps": {
    "foo": {}
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, "cannot parse string: unexpected object type")
}

func (*schemaSuite) TestMapSchemaMetConstraintsWithMissingEntry(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "foo": "string",
		"bar": "string"
  }
}`)

	// bar is in the schema but not the input and baz is the opposite (both aren't errors)
	input := []byte(`{
  "foo": "oof",
	"baz": {
		"a": "b"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapSchemaUnmetConstraint(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "foo": "string",
		"bar": "string"
  }
}`)

	input := []byte(`{
  "foo": "oof",
	"bar": {
		"a": "b"
	},
	"baz": "zab"
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot parse string: unexpected object type`)
}

func (*schemaSuite) TestMapSchemaWithMetRequiredConstraint(c *C) {
	// single list of required entries
	schemaStr := []byte(`{
  "schema": {
    "foo": "string",
		"bar": "string",
		"baz": "map"
  },
	"required": ["foo", "baz"]
}`)

	input := []byte(`{
  "foo": "oof",
	"baz": {
		"a": "b"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapSchemaWithUnmetRequiredConstraint(c *C) {
	// single list of required entries
	schemaStr := []byte(`{
  "schema": {
    "foo": "string",
		"bar": "string",
		"baz": "map"
  },
	"required": ["foo", "baz"]
}`)

	input := []byte(`{
  "foo": "oof",
	"bar": "rab"
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, "cannot find required combinations of keys")
}

func (*schemaSuite) TestMapSchemaWithAlternativeOfRequiredEntries(c *C) {
	// multiple alternative lists of required entries
	schemaStr := []byte(`{
  "schema": {
    "foo": "string",
		"bar": "string",
		"baz": "map"
  },
	"required": [["foo"], ["bar"]]
}`)

	// accepts the 1st allowed combination "foo"
	input := []byte(`{
  "foo": "oof"
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)

	// accepts the 2nd allowed combination "bar"
	input = []byte(`{
	"bar": "rab"
}`)

	schema, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapSchemaWithUnmetAlternativeOfRequiredEntries(c *C) {
	// multiple alternative lists of required entries
	schemaStr := []byte(`{
  "schema": {
    "foo": "string",
		"bar": "string",
		"baz": "map"
  },
	"required": [["foo"], ["bar"]]
}`)

	// accepts the 1st allowed combination "foo"
	input := []byte(`{
  "baz": {
		"a": "b"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, "cannot find required combinations of keys")
}

func (*schemaSuite) TestMapInvalidConstraintCombos(c *C) {
	type testcase struct {
		name    string
		snippet string
		err     string
	}

	tcs := []testcase{
		{
			name: "schema and keys",
			snippet: `
{
	"schema": { "foo": "bar" },
	"keys": "string"
}`,
			err: `cannot parse map: cannot use "schema" and "keys" constraints simultaneously`,
		},
		{
			name: "schema and values",
			snippet: `
{
	"schema": { "foo": "bar" },
	"values": "string"
}`,
			err: `cannot parse map: cannot use "schema" and "values" constraints simultaneously`,
		},
		{
			name: "required w/o schema",
			snippet: `
{
	"required": ["foo"]
}`,
			err: `cannot parse map: cannot use "required" without "schema" constraint`,
		},
	}

	for _, tc := range tcs {
		schemaStr := []byte(fmt.Sprintf(`{
  "schema": {
    "top": %s
  }
}`, tc.snippet))

		_, err := aspects.ParseSchema(schemaStr)
		cmt := Commentf("subtest %q", tc.name)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}

func (*schemaSuite) TestSchemaWithUnknownType(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "foo": "blarg"
  }
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse type "blarg": unknown`)
}
