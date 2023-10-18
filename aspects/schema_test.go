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
	c.Assert(err, ErrorMatches, `map contains unexpected key "bar"`)
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
	c.Assert(err, ErrorMatches, "cannot validate string: json: cannot unmarshal object into Go value of type string")
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
	c.Assert(err, ErrorMatches, `cannot validate string: json: .*`)
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
			c.Assert(err, ErrorMatches, fmt.Sprintf(`%d is not one of the allowed choices`, num))
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
			c.Assert(err, ErrorMatches, fmt.Sprintf(`%d is less than allowed minimum %d`, num, min))
		} else if num > max {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`%d is greater than allowed maximum %d`, num, max))
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
	c.Assert(err, ErrorMatches, `json: cannot unmarshal string into Go value of type int64`)

	input = []byte(`{
	"foo": 3.14
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `json: cannot unmarshal number 3.14 into Go value of type int64`)
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

	for _, val := range []string{`"bar"`, `123`, `{ "a": 1, "b": 2 }`} {
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
	c.Assert(err, ErrorMatches, `invalid character .*`)
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
			c.Assert(err, ErrorMatches, fmt.Sprintf(`%v is not one of the allowed choices`, num))
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
			c.Assert(err, ErrorMatches, fmt.Sprintf(`%v is less than allowed minimum %v`, num, min))
		} else if num > max {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`%v is greater than allowed maximum %v`, num, max))
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
	c.Assert(err, ErrorMatches, `json: cannot unmarshal string into Go value of type float64`)
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

func (*schemaSuite) TestTypesRejectNull(c *C) {
	for _, typ := range []string{"map", "string", "int", "any", "number"} {
		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": %q
	}
}`, typ))

		schema, err := aspects.ParseSchema(schemaStr)
		c.Assert(err, IsNil)

		err = schema.Validate([]byte(`{"foo": null}`))
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept null value for %q type`, typ))
	}
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
	c.Assert(err, ErrorMatches, `cannot accept null value for "string" type`)
}
