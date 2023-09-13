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

func (*schemaSuite) TestTopLevelMustBeMapType(c *C) {
	schemaStr := []byte(`{
	"type": "string"
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema: expected map but got string`)

	schemaStr = []byte(`{
		"type": "map",
		"schema": {

		}
}`)

	_, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestTopLevelTypeWrongFormat(c *C) {
	schemaStr := []byte(`{
	"type": {
		"type": "string"
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema's "type" entry: .*`)
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

func (*schemaSuite) TestMapWithBadValuesConstraint(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"values": "int"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse unknown type "int"`)
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
	c.Assert(err, ErrorMatches, "cannot validate string: json: cannot unmarshal object into Go value of type string")
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
	c.Assert(err, ErrorMatches, `cannot validate string: json: cannot unmarshal object into Go value of type string`)
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
			snippet: `{
	"schema": { "foo": "bar" },
	"keys": "string"
}`,
			err: `cannot parse map: cannot use "schema" and "keys" constraints simultaneously`,
		},
		{
			name: "schema and values",
			snippet: `{
	"schema": { "foo": "bar" },
	"values": "string"
}`,
			err: `cannot parse map: cannot use "schema" and "values" constraints simultaneously`,
		},
		{
			name: "required w/o schema",
			snippet: `{
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
	c.Assert(err, ErrorMatches, `cannot parse unknown type "blarg"`)
}

func (*schemaSuite) TestStringsWithEmptyChoices(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"keys": {
				"type": "string",
				"choices": []
			}
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: cannot have a "choices" constraint with an empty list`)
}

// NOTE: this also serves as a test for the success case of checking map keys
func (*schemaSuite) TestStringsWithChoicesHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"keys": {
				"type": "string",
				"choices": ["foo", "bar"]
			}
		}
	}
}`)

	input := []byte(`{
	"snaps": {
		"foo": "a",
		"bar": "a"
		}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

// NOTE: this also serves as a test for the failure case of checking map keys
func (*schemaSuite) TestStringsWithChoicesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"keys": {
				"type": "string",
				"choices": ["foo", "bar"]
			}
		}
	}
}`)

	input := []byte(`{
	"snaps": {
		"foo": "a",
		"bar": "a",
		"baz": "a"
		}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `string "baz" is not one of the allowed choices`)
}

func (*schemaSuite) TestStringChoicesAndPatternsFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"keys": {
				"type": "string",
				"pattern": "foo",
				"choices": ["foo"]
			}
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `.*cannot use "choices" and "pattern" constraints in same schema`)
}

func (*schemaSuite) TestStringPatternHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"pattern": {
			"keys": {
				"type": "string",
				"pattern": "[fb]oo"
			}
		}
	}
}`)

	input := []byte(`{
	"pattern": {
		"foo": "a",
		"boo": "a"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestStringPatternNoMatch(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "string",
			"pattern": "[fb]00"
		}
	}
}`)

	input := []byte(`{
	"foo": "F00"
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `string "F00" doesn't match schema pattern \[fb\]00`)
}

func (*schemaSuite) TestStringPatternWrongFormat(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "string",
			"pattern": "[fb00"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "pattern" constraint: error parsing regexp.*`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "string",
			"pattern": 1
		}
	}
}`)

	_, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "pattern" constraint:.*`)
}

func (*schemaSuite) TestStringChoicesWrongFormat(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "string",
			"choices": "one-choice"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "choices" constraint:.*`)
}
