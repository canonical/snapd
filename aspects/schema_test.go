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
	"math"

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
	c.Assert(err, ErrorMatches, `cannot parse top level schema as map: json: cannot unmarshal array.*`)
}

func (*schemaSuite) TestTopLevelMustBeMapType(c *C) {
	schemaStr := []byte(`{
	"type": "string"
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema: unexpected declared type "string", should be "map" or omitted`)

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

func (*schemaSuite) TestMapSchemasRequireConstraints(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"schema": {
				"foo": "map"
			}
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "map": must be schema definition with constraints`)
}

func (*schemaSuite) TestMapSchemasRequireSchemaOrKeyValues(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"type": "map"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse map: must have "schema" or "keys"/"values" constraint`)
}

func (*schemaSuite) TestMapWithUnexpectedKey(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"schema": {
				"foo": "string"
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
	c.Assert(err, ErrorMatches, `cannot accept element in "snaps": map contains unexpected key "bar"`)
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
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: keys must be based on string but got "int"`)
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
			"values": "foo"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse unknown type "foo"`)
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
	c.Assert(err, ErrorMatches, `cannot accept element in "snaps.foo": cannot parse string: json: cannot unmarshal object into Go value of type string`)
}

func (*schemaSuite) TestMapSchemaMetConstraintsWithMissingEntry(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "string",
		"bar": "string"
	}
}`)

	input := []byte(`{
	"foo": "oof"
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
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "bar": cannot parse string: json: cannot unmarshal object into Go value of type string`)
}

func (*schemaSuite) TestMapSchemaWithMetRequiredConstraint(c *C) {
	// single list of required entries
	schemaStr := []byte(`{
	"schema": {
		"foo": "string",
		"bar": "string",
		"baz": "int"
	},
	"required": ["foo", "baz"]
}`)

	input := []byte(`{
	"foo": "oof",
	"baz": 3
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
		"baz": "int"
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
	c.Assert(err, ErrorMatches, `cannot accept top level element: cannot find required combinations of keys`)
}

func (*schemaSuite) TestMapSchemaWithAlternativeOfRequiredEntries(c *C) {
	// multiple alternative lists of required entries
	schemaStr := []byte(`{
	"schema": {
		"foo": "string",
		"bar": "string",
		"baz": "int"
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
		"baz": "int"
	},
	"required": [["foo"], ["bar"]]
}`)

	input := []byte(`{
	"baz": 1
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept top level element: cannot find required combinations of keys`)
}

func (*schemaSuite) TestMapSchemaRequiredNotInSchema(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "string",
		"bar": "string"
	},
	"required": ["foo", "baz"]
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse map's "required" constraint: required key "baz" must have schema entry`)
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
	c.Assert(err, ErrorMatches, `cannot accept element in "snaps.baz": string "baz" is not one of the allowed choices`)
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
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": string "F00" doesn't match schema pattern \[fb\]00`)
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

func (*schemaSuite) TestStringBasedUserType(c *C) {
	schemaStr := []byte(`{
	"types": {
		"snap-name": {
			"type": "string",
			"pattern": "^[a-z0-9-]*[a-z][a-z0-9-]*$"
		},
		"status": {
			"type": "string",
			"choices": ["active", "inactive"]
		}
	},
	"schema": {
		"snaps": {
			"keys": "$snap-name",
			"values": {
				"schema": {
					"name": "$snap-name",
					"version": "string",
					"status": "$status"
				}
			}
		}
	}
}`)

	input := []byte(`{
	"snaps": {
		"core20": {
			"name": "core20",
			"version": "20230503",
			"status": "active"
		},
		"snapd": {
			"name": "snapd",
			"version": "2.59.5+git948.gb447044",
			"status": "inactive"
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapKeyMustBeStringUserType(c *C) {
	schemaStr := []byte(`{
	"types": {
		"key-type": {
			"type": "map",
			"schema": {}
		}
	},
	"schema": {
		"snaps": {
			"keys": "$key-type"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: key type "key-type" must be based on string`)
}

func (*schemaSuite) TestUserDefinedTypesWrongFormat(c *C) {
	schemaStr := []byte(`{
	"types": ["foo"],
	"schema": {}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse user-defined types map: json: cannot unmarshal.*`)
}

func (*schemaSuite) TestBadUserDefinedType(c *C) {
	schemaStr := []byte(`{
	"types": {
		"mytype": {
			"type": "bad-type"
		}
	},
	"schema": {}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse user-defined type "mytype": cannot parse unknown type "bad-type"`)
}

func (*schemaSuite) TestUnknownUserDefinedType(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"values": "$foo"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot find user-defined type "foo"`)
}

func (*schemaSuite) TestUnknownUserDefinedTypeInKeys(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"keys": "$foo"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: cannot find user-defined type "foo"`)
}

func (*schemaSuite) TestMapBasedUserDefinedTypeHappy(c *C) {
	schemaStr := []byte(`{
	"types": {
		"snap": {
			"schema": {
				"name": "string",
				"status": "string"
			}
		}
	},
	"schema": {
		"snaps": {
			"values": "$snap"
		}
	}
}`)

	input := []byte(`{
	"snaps": {
		"core20": {
			"name": "core20",
			"status": "active"
		},
		"snapd": {
			"name": "snapd",
			"status": "inactive"
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestUserTypeReferenceDoesntRequireConstraints(c *C) {
	// references to user-defined types don't require need constraints
	schemaStr := []byte(`{
	"types": {
		"my-type": {
			"schema": {
				"foo": "string"
			}
		}
	},
	"schema": {
		"a": "$my-type"
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

}

func (*schemaSuite) TestMapInUserTypeRequiresConstraints(c *C) {
	// maps still require constraints even within user-defined types
	schemaStr := []byte(`{
	"types": {
		"my-type": "map"
	},
	"schema": {
		"a": "$my-type"
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse user-defined type "my-type": cannot parse "map": must be schema definition with constraints`)
}

func (*schemaSuite) TestMapBasedUserDefinedTypeFail(c *C) {
	schemaStr := []byte(`{
	"types": {
		"snap": {
			"schema": {
				"name": "string",
				"version": "string"
			}
		}
	},
	"schema": {
		"snaps": {
			"values": "$snap"
		}
	}
}`)

	input := []byte(`{
	"snaps": {
		"core20": {
			"name": "core20",
			"version": 123
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "snaps.core20.version": cannot parse string: json: .*`)
}

func (*schemaSuite) TestBadUserDefinedTypeName(c *C) {
	schemaStr := []byte(`{
	"types": {
		"-foo": {
			"schema": {
				"name": "string",
				"version": "string"
			}
		}
	},
	"schema": {
		"snaps": {
			"values": "$-foo"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, `cannot parse user-defined type name "-foo": must match ^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$`)
}

func (*schemaSuite) TestIntegerHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "int"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": 1
}`)
	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestIntegerMustMatchChoices(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"choices": [1,	3]
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, num := range []int{0, 1, 2, 3, 4} {
		input := []byte(fmt.Sprintf(`{
	"foo": %d
}`, num))

		err := schema.Validate(input)
		if num == 1 || num == 3 {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": %d is not one of the allowed choices`, num))
		}
	}
}

func (*schemaSuite) TestIntegerMustMatchMinMax(c *C) {
	min, max := 1, 3
	schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": {
			"type": "int",
			"min": %d,
			"max": %d
		}
	}
}`, min, max))

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, num := range []int{0, 1, 2, 3, 4} {
		input := []byte(fmt.Sprintf(`{
	"foo": %d
}`, num))

		err := schema.Validate(input)
		if num < min {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": %d is less than the allowed minimum %d`, num, min))
		} else if num > max {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": %d is greater than the allowed maximum %d`, num, max))
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (*schemaSuite) TestIntegerWithWrongTypes(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "int"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": "bar"
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": json: cannot unmarshal string into Go value of type int64`)

	input = []byte(`{
	"foo": 3.14
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": json: cannot unmarshal number 3.14 into Go value of type int64`)
}

func (*schemaSuite) TestIntegerChoicesAndMinMaxFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"min": 0,
			"choices": [0]
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "choices" and "min" constraints`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"max": 0,
			"choices": [0]
		}
	}
}`)

	_, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "choices" and "max" constraints`)
}

func (*schemaSuite) TestIntegerEmptyChoicesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"choices": []
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "choices" constraint with empty list`)
}

func (*schemaSuite) TestIntegerBadChoicesConstraint(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"choices": 5
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "choices" constraint: json: cannot unmarshal number into Go value of type \[\]int64`)
}

func (*schemaSuite) TestIntegerBadMinMaxConstraints(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"min": "5"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "min" constraint: json: cannot unmarshal string into Go value of type int64`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"max": "5"
		}
	}
}`)

	_, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "max" constraint: json: cannot unmarshal string into Go value of type int64`)
}

func (*schemaSuite) TestIntegerMinGreaterThanMaxConstraintFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"min": 5,
			"max": 1
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "min" constraint with value greater than "max"`)
}

func (*schemaSuite) TestIntegerMinMaxOver32Bits(c *C) {
	schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": {
			"type": "int",
			"min": %d,
			"max": %d
		}
	}
}`, int64(math.MinInt64), int64(math.MaxInt64)))

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(fmt.Sprintf(`{
	"foo": %d
}`, int64(math.MinInt64)))

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestIntegerChoicesOver32Bits(c *C) {
	schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": {
			"type": "int",
			"choices": [%d, %d]
		}
	}
}`, int64(math.MinInt64), int64(math.MaxInt64)))

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, num := range []int64{math.MinInt64, math.MaxInt64} {
		input := []byte(fmt.Sprintf(`{
	"foo": %d
}`, num))

		err = schema.Validate(input)
		c.Assert(err, IsNil)
	}
}

func (*schemaSuite) TestAnyTypeAcceptsAllTypes(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "any"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, val := range []string{`"bar"`, `123`, `{ "a": 1, "b": 2 }`, `0.1`, `false`} {
		input := []byte(fmt.Sprintf(`{
			"foo": %s
		}`, val))

		err = schema.Validate(input)
		c.Assert(err, IsNil, Commentf(`"any" type didn't accept expected value: %s`, val))
	}
}

func (*schemaSuite) TestAnyTypeWithMapDefinition(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "any"
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
			"foo": "string"
		}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestAnyTypeRejectsBadJSON(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "any"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": .
}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept top level element: invalid character .*`)
}

func (*schemaSuite) TestNumberValidFloatAndInt(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "number",
		"bar": "number"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": 1.2,
	"bar": 1
}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestNumberMustMatchChoices(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"choices": [1,	3.0]
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, num := range []float64{0, 1, 2, 3, 4} {
		input := []byte(fmt.Sprintf(`{
	"foo": %f
}`, num))

		err := schema.Validate(input)
		if num == 1 || num == 3 {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": %v is not one of the allowed choices`, num))
		}
	}
}

func (*schemaSuite) TestNumberMustMatchMinMax(c *C) {
	min, max := float32(0.1), float32(3)
	schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": {
			"type": "number",
			"min": %.1f,
			"max": %f
		}
	}
}`, min, max))

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, num := range []float32{0, 0.1, 2, 3, 4} {
		input := []byte(fmt.Sprintf(`{
	"foo": %.25f
}`, num))

		err := schema.Validate(input)
		if num < min {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": %v is less than the allowed minimum %v`, num, min))
		} else if num > max {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": %v is greater than the allowed maximum %v`, num, max))
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (*schemaSuite) TestNumberWithWrongTypes(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "number"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": "bar"
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": json: cannot unmarshal string into Go value of type float64`)
}

func (*schemaSuite) TestNumberChoicesAndMinMaxFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"min": 0,
			"choices": [0]
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "choices" and "min" constraints`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"max": 0,
			"choices": [0]
		}
	}
}`)

	_, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "choices" and "max" constraints`)
}

func (*schemaSuite) TestNumberEmptyChoicesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"choices": []
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "choices" constraint with empty list`)
}

func (*schemaSuite) TestNumberBadChoicesConstraint(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"choices": 5
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "choices" constraint: json: cannot unmarshal number into Go value of type \[\]float64`)
}

func (*schemaSuite) TestNumberBadMinMaxConstraints(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"min": "5"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "min" constraint: json: cannot unmarshal string into Go value of type float64`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"max": "5"
		}
	}
}`)

	_, err = aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "max" constraint: json: cannot unmarshal string into Go value of type float64`)
}

func (*schemaSuite) TestNumberMinGreaterThanMaxConstraintFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"min": 5,
			"max": 1
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "min" constraint with value greater than "max"`)
}

func (*schemaSuite) TestSimpleTypesRejectNull(c *C) {
	for _, typ := range []string{"string", "int", "any", "number", "bool"} {
		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": %q
	}
}`, typ))

		schema, err := aspects.ParseSchema(schemaStr)
		c.Assert(err, IsNil)

		err = schema.Validate([]byte(`{"foo": null}`))
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": cannot accept null value for %q type`, typ))
	}
}

func (*schemaSuite) TestMapTypeRejectsNull(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"a": "int"
			}
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate([]byte(`{"foo": null}`))
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": cannot accept null value for "map" type`)
}

func (*schemaSuite) TestUserDefinedTypeRejectsNull(c *C) {
	schemaStr := []byte(`{
	"types": {
		"mytype": {
			"type": "string"
		}
	},
	"schema": {
		"foo": "$mytype"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate([]byte(`{"foo": null}`))
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": cannot accept null value for "string" type`)
}

func (*schemaSuite) TestArrayRejectsNull(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "int"
		}
	}
}`)
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": null}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": cannot accept null value for "array" type`)
}

func (*schemaSuite) TestBooleanHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "bool",
		"bar": "bool"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": true,
	"bar": false
}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestBooleanWrongType(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "bool"
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": 1
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": json: cannot unmarshal number into Go value of type bool`)
}

func (*schemaSuite) TestArrayHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "string"
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": ["a", "b"]
}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestArrayHappyWithUserDefinedType(c *C) {
	schemaStr := []byte(`{
	"types": {
		"my-type": "string"
	},
	"schema": {
		"foo": {
			"type": "array",
			"values": "$my-type"
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": ["a", "b"]
}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestArrayRequireConstraints(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "array"
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "array": must be schema definition with constraints`)
}

func (*schemaSuite) TestArrayRequireValueConstraint(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "array": must have "values" constraint`)
}

func (*schemaSuite) TestArrayFailsWithBadElementType(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "foo"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "array" values type: cannot parse unknown type "foo"`)
}

func (*schemaSuite) TestArrayEnforcesOnlyOneType(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "string"
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": ["a", 1]
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo\[1\]": cannot parse string: json:.*`)
}

func (*schemaSuite) TestArrayWithUniqueRejectsDuplicates(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "string",
			"unique": true
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": ["a", "a"]
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": cannot accept duplicate values for array with "unique" constraint`)
}

func (*schemaSuite) TestArrayWithoutUniqueAcceptsDuplicates(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "string",
			"unique": false
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": ["a", "b"]
}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestArrayFailsWithBadUniqueType(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "string",
			"unique": "true"
		}
	}
}`)

	_, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse array's "unique" constraint: json: cannot unmarshal string into Go value of type bool`)
}

func (*schemaSuite) TestErrorContainsPathPrefixes(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"bar": {
					"schema": {
						"baz": "string"
					}
				}
			}
		}
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	type testcase struct {
		name  string
		input []byte
		err   string
	}

	testcases := []testcase{
		{
			name:  "top level",
			input: []byte(`{"bar": 1}`),
			err:   `cannot accept top level element: map contains unexpected key "bar"`,
		},
		{
			name:  "1 level of nesting",
			input: []byte(`{"foo": {"baz": 1}}`),
			err:   `cannot accept element in "foo": map contains unexpected key "baz"`,
		},
		{
			name:  "2 levels of nesting",
			input: []byte(`{"foo": {"bar": {"boo": 1}}}`),
			err:   `cannot accept element in "foo.bar": map contains unexpected key "boo"`,
		},
	}

	for _, tc := range testcases {
		err = schema.Validate(tc.input)
		c.Assert(err, ErrorMatches, tc.err, Commentf("test case %q failed", tc.name))
	}
}

func (*schemaSuite) TestPathPrefixWithMapUnderUserType(c *C) {
	schemaStr := []byte(`{
	"types": {
		"my-type": {
			"schema": {
				"bar": {
					"type": "int",
					"min": 0
				}
			}
		}
	},
	"schema": {
		"foo": "$my-type"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": {"bar": -1}}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo.bar": -1 is less than the allowed minimum 0`)
}

func (*schemaSuite) TestPathPrefixWithArrayUnderUserType(c *C) {
	schemaStr := []byte(`{
	"types": {
		"my-type": {
			"type": "int",
			"min": 0
		}
	},
	"schema": {
		"foo": {
			"type": "array",
			"values": "$my-type"
		}
	}
}`)
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": [-1]}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo\[0\]": -1 is less than the allowed minimum 0`)
}

func (*schemaSuite) TestPathPrefixWithArrayUnderUserWithAContainerElementType(c *C) {
	schemaStr := []byte(`{
	"types": {
		"my-type": {
			"type": "array",
			"values": {
				"schema": {
					"bar": {
						"type": "int",
						"min": 0
					}
				}
			}
		}
	},
	"schema": {
		"foo": "$my-type"
	}
}`)
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": [{"bar": 1}, {"bar": -1}]}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo\[1\].bar": -1 is less than the allowed minimum 0`)
}

func (*schemaSuite) TestPathPrefixWithKeyOrValueConstraints(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "map",
			"keys": {
				"type": "string",
				"choices": ["my-key"]
			},
			"values": {
				"type": "int",
				"min": 0
			}
		}
	}
}`)
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": {"other-key": 1}}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo.other-key": string "other-key" is not one of the allowed choices`)

	input = []byte(`{"foo": {"my-key": -1}}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo.my-key": -1 is less than the allowed minimum 0`)
}

func (*schemaSuite) TestPathManyUserDefinedTypeReferences(c *C) {
	schemaStr := []byte(`{
	"types": {
		"my-type": {
			"type": "map",
			"values": {
				"type": "int",
				"min": 0
			}
		}
	},
	"schema": {
		"foo": "$my-type",
		"bar": "$my-type"
	}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": { "one": 1 }, "bar": { "two": -1 } }`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "bar.two": -1 is less than the allowed minimum 0`)
}

func (*schemaSuite) TestValidationError(c *C) {
	type testcase struct {
		path     []interface{}
		expected string
	}

	cases := []testcase{
		{
			path:     []interface{}{"foo", "bar"},
			expected: "foo.bar",
		},
		{
			path:     []interface{}{"foo", 1, "bar"},
			expected: "foo[1].bar",
		},
		{
			path:     []interface{}{"foo", 1, 2, "bar"},
			expected: "foo[1][2].bar",
		},
		{
			path:     []interface{}{"foo", 2.9, 1},
			expected: "foo.<n/a>[1]",
		},
	}

	for _, tc := range cases {
		err := &aspects.ValidationError{
			Path: tc.path,
			Err:  fmt.Errorf("base error"),
		}

		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot accept element in %q: base error`, tc.expected))
	}
}
