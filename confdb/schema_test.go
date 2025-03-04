// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
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

package confdb_test

import (
	"fmt"
	"math"

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type schemaSuite struct{}

var _ = Suite(&schemaSuite{})

func (*schemaSuite) TestTopLevelFailsWithoutSchema(c *C) {
	schemaStr := []byte(`{
	"keys": "string"
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema: must have a "schema" constraint`)
}

func (*schemaSuite) TestSchemaMustBeMap(c *C) {
	schemaStr := []byte(`["foo"]`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema as map: json: cannot unmarshal array.*`)
}

func (*schemaSuite) TestTopLevelMustBeMapType(c *C) {
	schemaStr := []byte(`{
	"type": "string"
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse top level schema: unexpected declared type "string", should be "map" or omitted`)

	schemaStr = []byte(`{
		"type": "map",
		"schema": {

		}
}`)

	_, err = confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestTopLevelTypeWrongFormat(c *C) {
	schemaStr := []byte(`{
	"type": {
		"type": "string"
	}
}`)

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "map": must be schema definition with constraints`)
}

func (*schemaSuite) TestTypeConstraintMustBeString(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"type": 1,
			"schema": {
				"foo": "string"
			}
		}
	}
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "type" constraint in type definition: json: cannot unmarshal number into.*`)
}

func (*schemaSuite) TestMapSchemasRequireSchemaOrKeyValues(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"type": "map"
		}
	}
}`)

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: must be based on string but type was map`)

	schemaStr = []byte(`{
	"schema": {
		"snaps": {
			"keys": "int"
		}
	}
}`)

	_, err = confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: keys must be based on string but type was int`)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "snaps.foo": expected string type but value was object`)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "bar": expected string type but value was object`)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)

	// accepts the 2nd allowed combination "bar"
	input = []byte(`{
	"bar": "rab"
}`)

	schema, err = confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse map's "required" constraint: required key "baz" must have schema entry`)
}

func (*schemaSuite) TestMapSchemaWithInvalidKeyFormat(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"-foo": "string"
	}
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse map: key "-foo" doesn't conform to required format: .*`)
}

func (*schemaSuite) TestMapRejectsInputMapWithInvalidKeyFormat(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "int"
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"-foo": 1
}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept top level element: key "-foo" doesn't conform to required format: .*`)
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

		_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": expected string matching \[fb\]00 but value was "F00"`)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "pattern" constraint: error parsing regexp.*`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "string",
			"pattern": 1
		}
	}
}`)

	_, err = confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "choices" constraint:.*`)
}

func (*schemaSuite) TestStringBasedAlias(c *C) {
	schemaStr := []byte(`{
	"aliases": {
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
			"version": "2.59.5+g948.b447044",
			"status": "inactive"
		}
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestMapKeyMustBeStringAlias(c *C) {
	schemaStr := []byte(`{
	"aliases": {
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: key type "key-type" must be based on string`)
}

func (*schemaSuite) TestAliasesWrongFormat(c *C) {
	schemaStr := []byte(`{
	"aliases": ["foo"],
	"schema": {}
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse aliases map: json: cannot unmarshal.*`)
}

func (*schemaSuite) TestBadAlias(c *C) {
	schemaStr := []byte(`{
	"aliases": {
		"mytype": {
			"type": "bad-type"
		}
	},
	"schema": {}
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse alias "mytype": cannot parse unknown type "bad-type"`)
}

func (*schemaSuite) TestUnknownAlias(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"values": "$foo"
		}
	}
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot find alias "foo"`)
}

func (*schemaSuite) TestUnknownAliasInKeys(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"keys": "$foo"
		}
	}
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "keys" constraint: cannot find alias "foo"`)
}

func (*schemaSuite) TestMapBasedAliasHappy(c *C) {
	schemaStr := []byte(`{
	"aliases": {
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestAliasReferenceDoesntRequireConstraints(c *C) {
	// references to aliases don't require need constraints
	schemaStr := []byte(`{
	"aliases": {
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

}

func (*schemaSuite) TestMapInAliasRequiresConstraints(c *C) {
	// maps still require constraints even within aliases
	schemaStr := []byte(`{
	"aliases": {
		"my-type": "map"
	},
	"schema": {
		"a": "$my-type"
	}
}`)

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse alias "my-type": cannot parse "map": must be schema definition with constraints`)
}

func (*schemaSuite) TestMapBasedAliasFail(c *C) {
	schemaStr := []byte(`{
	"aliases": {
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "snaps.core20.version": expected string type but value was number`)
}

func (*schemaSuite) TestBadAliasName(c *C) {
	schemaStr := []byte(`{
	"aliases": {
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, `cannot parse alias name "-foo": must match ^[a-z](?:-?[a-z0-9])*$`)
}

func (*schemaSuite) TestIntegerHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "int"
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": 1
}`)
	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestIntRejectsOtherValues(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "int"
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, val := range []string{`1.2`, `"a"`, `false`, `[1]`} {
		input := []byte(fmt.Sprintf(`{
	"foo": %s
}`, val))
		err = schema.Validate(input)
		c.Check(err, ErrorMatches, `cannot accept element in "foo": expected int type but value was .*`)
	}
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err = confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "min" constraint: json: cannot unmarshal string into Go value of type int64`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "int",
			"max": "5"
		}
	}
}`)

	_, err = confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err = confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse "min" constraint: json: cannot unmarshal string into Go value of type float64`)

	schemaStr = []byte(`{
	"schema": {
		"foo": {
			"type": "number",
			"max": "5"
		}
	}
}`)

	_, err = confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot have "min" constraint with value greater than "max"`)
}

func (*schemaSuite) TestSimpleTypesRejectNull(c *C) {
	for _, typ := range []string{"string", "int", "any", "number", "bool"} {
		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": %q
	}
}`, typ))

		schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate([]byte(`{"foo": null}`))
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": cannot accept null value for "map" type`)
}

func (*schemaSuite) TestAliasRejectsNull(c *C) {
	schemaStr := []byte(`{
	"aliases": {
		"mytype": {
			"type": "string"
		}
	},
	"schema": {
		"foo": "$mytype"
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
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
	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": true,
	"bar": false
}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": ["a", "b"]
}`)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestArrayHappyWithAlias(c *C) {
	schemaStr := []byte(`{
	"aliases": {
		"my-type": "string"
	},
	"schema": {
		"foo": {
			"type": "array",
			"values": "$my-type"
		}
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{
	"foo": ["a", 1]
}`)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo\[1\]": expected string type but value was number`)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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

	_, err := confdb.ParseSchema(schemaStr)
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

	schema, err := confdb.ParseSchema(schemaStr)
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
	"aliases": {
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

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": {"bar": -1}}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo.bar": -1 is less than the allowed minimum 0`)
}

func (*schemaSuite) TestPathPrefixWithArrayUnderAlias(c *C) {
	schemaStr := []byte(`{
	"aliases": {
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
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	input := []byte(`{"foo": [-1]}`)
	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `cannot accept element in "foo\[0\]": -1 is less than the allowed minimum 0`)
}

func (*schemaSuite) TestPathPrefixWithArrayUnderAliasWithAContainerElementType(c *C) {
	schemaStr := []byte(`{
	"aliases": {
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
	schema, err := confdb.ParseSchema(schemaStr)
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
	schema, err := confdb.ParseSchema(schemaStr)
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
	"aliases": {
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

	schema, err := confdb.ParseSchema(schemaStr)
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
		err := &confdb.ValidationError{
			Path: tc.path,
			Err:  fmt.Errorf("base error"),
		}

		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot accept element in %q: base error`, tc.expected))
	}
}

func (*schemaSuite) TestUnexpectedTypes(c *C) {
	type testcase struct {
		schemaType   string
		expectedType string
		testValue    interface{}
	}

	tcs := []testcase{
		{
			schemaType:   `{"type": "array", "values": "any"}`,
			expectedType: "array",
			testValue:    true,
		},
		{
			schemaType:   `{"type": "map", "values": "any"}`,
			expectedType: "map",
			testValue:    true,
		},
		{
			schemaType:   `"int"`,
			expectedType: "int",
			testValue:    true,
		},
		{
			schemaType:   `"number"`,
			expectedType: "number",
			testValue:    true,
		},
		{
			schemaType:   `"string"`,
			expectedType: "string",
			testValue:    true,
		},
		{
			schemaType:   `"bool"`,
			expectedType: "bool",
			testValue:    `"bar"`,
		},
	}

	for _, tc := range tcs {
		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": %s
	}
}`, tc.schemaType))
		schema, err := confdb.ParseSchema(schemaStr)
		c.Assert(err, IsNil)

		input := []byte(fmt.Sprintf(`{"foo": %v}`, tc.testValue))
		err = schema.Validate(input)
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot accept element in "foo": expected %s type but value was %T`, tc.expectedType, tc.testValue))
	}
}

func (*schemaSuite) TestAlternativeTypesHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["string", "int"]
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, val := range []interface{}{`"one"`, `1`} {
		input := []byte(fmt.Sprintf(`{"foo":%s}`, val))
		err = schema.Validate(input)
		c.Assert(err, IsNil)
	}
}

func (*schemaSuite) TestAlternativeTypesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["string", "int"]
	}
}`)

	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, val := range []interface{}{"1.1", "true", `{"bar": 1}`, `[1, 2]`} {
		input := []byte(fmt.Sprintf(`{"foo":%s}`, val))
		err = schema.Validate(input)
		c.Assert(err, ErrorMatches, `cannot accept element in "foo": no matching schema:
	expected string .*
	or expected int .*`)
	}
}

func (*schemaSuite) TestAlternativeTypesWithConstraintsHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": [
			{
				"type": "int",
				"min": 0
			},
			{
				"type": "string",
				"pattern": "[bB]ar"
			}
		]
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, val := range []interface{}{"3", "0", `"Bar"`, `"bar"`} {
		input := []byte(fmt.Sprintf(`{"foo":%s}`, val))
		err = schema.Validate(input)
		c.Assert(err, IsNil)
	}
}

func (*schemaSuite) TestAlternativeTypesWithConstraintsFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": [
			{
				"type": "int",
				"min": 0
			},
			{
				"type": "string",
				"pattern": "[bB]ar"
			}
		]
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate([]byte(`{"foo":-1}`))
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": no matching schema:
	-1 is less than the allowed minimum 0
	or expected string type but value was number`)

	err = schema.Validate([]byte(`{"foo":"bAR"}`))
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": no matching schema:
	expected int type but value was string
	or expected string matching \[bB\]ar but value was "bAR"`)
}

func (*schemaSuite) TestAlternativeTypesNestedHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", ["number", ["string"]]]
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	for _, val := range []interface{}{`"one"`, `1`, `1.3`} {
		input := []byte(fmt.Sprintf(`{"foo":%s}`, val))
		err = schema.Validate(input)
		c.Assert(err, IsNil)
	}
}

func (*schemaSuite) TestAlternativeTypesNestedFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", ["number", ["string"]]]
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate([]byte(`{"foo":false}`))
	c.Assert(err, ErrorMatches, `cannot accept element in "foo": no matching schema:
	expected int type but value was bool
	or expected number type but value was bool
	or expected string type but value was bool`)
}

func (*schemaSuite) TestAlternativeTypesUnknownType(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["foo"]
	}
}`)
	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse alternative types: cannot parse unknown type "foo"`)
}

func (*schemaSuite) TestAlternativeTypesEmpty(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": []
	}
}`)
	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse alternative types: alternative type list cannot be empty`)
}

func (*schemaSuite) TestAlternativeTypesPathError(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"bar": [{"schema": {"baz": "int"}}, {"schema": {"baz": {"schema": {"zab": {"type": "array", "values": "string"}}}}}]
			}
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate([]byte(`{"foo":{"bar": {"baz": {"zab": [1]}}}}`))
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, `cannot accept element in "foo.bar": no matching schema:
	..."baz": expected int type but value was object
	or ..."baz.zab[0]": expected string type but value was number`)
}

func (*schemaSuite) TestInvalidTypeDefinition(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": 1
	}
}`)
	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot parse type definition: type must be expressed as map, string or list: json: cannot unmarshal number.*`)
}

func schemasToTypes(schemas []confdb.DatabagSchema) []confdb.SchemaType {
	var types []confdb.SchemaType
	for _, s := range schemas {
		types = append(types, s.Type())
	}
	return types
}

func (*schemaSuite) TestSchemaAtTopLevel(c *C) {
	type testcase struct {
		typeStr    string
		schemaType confdb.SchemaType
	}

	tcs := []testcase{
		{
			typeStr:    `{"type": "array", "values": "any"}`,
			schemaType: confdb.Array,
		},
		{
			typeStr:    `{"type": "map", "values": "any"}`,
			schemaType: confdb.Map,
		},
		{
			typeStr:    `"int"`,
			schemaType: confdb.Int,
		},
		{
			typeStr:    `"number"`,
			schemaType: confdb.Number,
		},
		{
			typeStr:    `"string"`,
			schemaType: confdb.String,
		},
		{
			typeStr:    `"bool"`,
			schemaType: confdb.Bool,
		},
		{
			typeStr:    `"any"`,
			schemaType: confdb.Any,
		},
	}

	for _, tc := range tcs {
		cmt := Commentf("type %q test", tc.typeStr)
		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": %s
	}
}`, tc.typeStr))
		schema, err := confdb.ParseSchema(schemaStr)
		c.Assert(err, IsNil, cmt)

		schemas, err := schema.SchemaAt([]string{"foo"})
		c.Assert(err, IsNil, cmt)
		types := schemasToTypes(schemas)
		c.Assert(types, testutil.DeepUnsortedMatches, []confdb.SchemaType{tc.schemaType}, cmt)
	}
}

func (*schemaSuite) TestSchemaAtNestedMapWithSchema(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"bar": "string"
			}
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Assert(schemasToTypes(schemas), DeepEquals, []confdb.SchemaType{confdb.String})
}

func (*schemaSuite) TestSchemaAtNestedInMapWithValues(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "map",
			"values": "string"
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Assert(schemasToTypes(schemas), DeepEquals, []confdb.SchemaType{confdb.String})
}

func (*schemaSuite) TestSchemaAtNestedInArray(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": "string"
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "0"})
	c.Assert(err, IsNil)
	c.Assert(schemasToTypes(schemas), DeepEquals, []confdb.SchemaType{confdb.String})
}

func (*schemaSuite) TestSchemaAtInUserDefinedType(c *C) {
	schemaStr := []byte(`{
	"aliases": {
		"my-type": {
			"type": "map",
			"schema": {
				"bar": "string"
			}
		}
	},
	"schema": {
		"foo": "$my-type"
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Assert(schemasToTypes(schemas), DeepEquals, []confdb.SchemaType{confdb.String})
}

func (*schemaSuite) TestSchemaAtExceedingSchemaLeafSchema(c *C) {
	for _, typ := range []string{"int", "number", "bool", "string"} {
		cmt := Commentf("type %q test", typ)
		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": %q
	}
}`, typ))
		schema, err := confdb.ParseSchema(schemaStr)
		c.Assert(err, IsNil, cmt)

		schemas, err := schema.SchemaAt([]string{"foo", "bar"})
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot follow path beyond %q type`, typ), cmt)
		c.Assert(schemas, IsNil, cmt)
	}
}

func (*schemaSuite) TestSchemaAtExceedingSchemaContainerSchema(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {"type": "array", "values": "string"}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "0", "bar"})
	c.Assert(err, ErrorMatches, `cannot follow path beyond "string" type`)
	c.Assert(schemas, IsNil)
}

func (*schemaSuite) TestSchemaAtBadPathArray(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {"type": "array", "values": "any"}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "b"})
	c.Assert(err, ErrorMatches, `key "b" cannot be used to index array`)
	c.Assert(schemas, IsNil)
}

func (*schemaSuite) TestSchemaAtBadPathMap(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"bar": "any"
			}
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "baz"})
	c.Assert(err, ErrorMatches, `cannot use "baz" as key in map`)
	c.Assert(schemas, IsNil)
}

func (*schemaSuite) TestSchemaAtAlternativesDifferentDepthsHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", {"schema": {"bar": "string"}}]
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "bar"})
	c.Assert(err, IsNil)
	c.Assert(schemasToTypes(schemas), DeepEquals, []confdb.SchemaType{confdb.String})
}

func (*schemaSuite) TestSchemaAtAlternativesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "string"]
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "bar"})
	c.Assert(err, ErrorMatches, `cannot follow path beyond "string" type`)
	c.Assert(schemas, IsNil)
}

func (*schemaSuite) TestSchemaAtAnyAcceptsLongerPath(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "any"
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	schemas, err := schema.SchemaAt([]string{"foo", "baz"})
	c.Assert(err, IsNil)
	c.Assert(schemas, NotNil)
}

func (*schemaSuite) TestEphemeralTopLevelSchema(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "any"
	},
	"ephemeral": true
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)
	c.Assert(schema.Ephemeral(), Equals, true)
}

func (*schemaSuite) TestEphemeralAllTypes(c *C) {
	for _, typ := range []string{"number", "int", "bool", "string", "any", `array", "values": "string`} {
		cmt := Commentf("ephemeral broken for type %q", typ)

		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": {
			"type": "%s",
			"ephemeral": true
		}
	}
}`, typ))
		schema, err := confdb.ParseSchema(schemaStr)
		c.Assert(err, IsNil, cmt)

		nestedSchema, err := schema.SchemaAt([]string{"foo"})
		c.Assert(err, IsNil, cmt)
		c.Assert(nestedSchema, HasLen, 1, cmt)
		c.Check(nestedSchema[0].Ephemeral(), Equals, true, cmt)
	}
}

func (*schemaSuite) TestAlternativesAllEphemeralOk(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": [
		{
			"type": "number",
			"ephemeral": true
		},
		"bool"
		]
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	nestedSchema, err := schema.SchemaAt([]string{"foo"})
	c.Assert(err, IsNil)
	c.Assert(nestedSchema, HasLen, 2)
	c.Assert(nestedSchema[0].Ephemeral(), Equals, true)
	c.Assert(nestedSchema[1].Ephemeral(), Equals, false)
}

func (*schemaSuite) TestUserDefinedTypeEphemeralFail(c *C) {
	schemaStr := []byte(`{
	"aliases": {
		"my-type": {
			"type": "string",
			"ephemeral": true
		}
	},
	"schema": {
		"foo": "$my-type"
	}
}`)
	_, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `cannot use "ephemeral" in user-defined type: my-type`)
}

func (*schemaSuite) TestUserTypeReferenceEphemeral(c *C) {
	schemaStr := []byte(`{
	"aliases": {
		"my-type": {
			"type": "string"
		}
	},
	"schema": {
		"foo": {
			"type": "$my-type",
			"ephemeral": true
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	nestedSchema, err := schema.SchemaAt([]string{"foo"})
	c.Assert(err, IsNil)
	c.Assert(nestedSchema, HasLen, 1)
	c.Assert(nestedSchema[0].Ephemeral(), Equals, true)
}

func (*schemaSuite) TestPruneEphemeralBasicTypes(c *C) {
	type testcase struct {
		typ  string
		data string
	}

	tcs := []testcase{
		{
			typ:  "number",
			data: `1.3`,
		},
		{
			typ:  "int",
			data: `5`,
		},
		{
			typ:  "bool",
			data: `true`,
		},
		{
			typ:  "string",
			data: `"foo"`,
		},
		{
			typ:  "any",
			data: `5.3`,
		},
	}

	for _, tc := range tcs {
		cmt := Commentf("ephemeral pruning for type %q", tc.typ)

		schemaStr := []byte(fmt.Sprintf(`{
	"schema": {
		"foo": {
			"type": "%s",
			"ephemeral": true
		},
		"bar": "string"
	}
}`, tc.typ))
		schema, err := confdb.ParseSchema(schemaStr)
		c.Assert(err, IsNil, cmt)

		data := []byte(fmt.Sprintf(`{
	"foo": %s,
	"bar": "baz"
}`, tc.data))

		data, err = schema.PruneEphemeral(data)
		c.Assert(err, IsNil, cmt)
		c.Assert(string(data), Equals, `{"bar":"baz"}`)
	}
}

func (*schemaSuite) TestPruneEphemeralContainerTypes(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"values": "string",
			"ephemeral": true
		},
		"bar": {
			"type": "array",
			"values": "int",
			"ephemeral": true
		},
		"baz": {
			"keys": {
				"type": "string",
				"ephemeral": true
			}
		},
		"nested": {
			"type": "array",
			"values": {
				"type": "string",
				"ephemeral": true
			}
		},
		"no-eph": "string"
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	data := []byte(`{
	"foo": {
		"a": "b"
	},
	"bar": [1, 5, 9],
	"baz": {
		"a": "b"
	},
	"nested": ["foo"],
	"no-eph": "foo"
}`)

	data, err = schema.PruneEphemeral(data)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"baz":{},"nested":[],"no-eph":"foo"}`)
}

func (*schemaSuite) TestPruneEphemeralAliasReference(c *C) {
	schemaStr := []byte(`{
	"aliases": {
		"my-type": {
			"type": "string"
		}
	},
	"schema": {
		"foo": {
			"type": "$my-type",
			"ephemeral": true
		},
		"bar": "$my-type"
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	data, err := schema.PruneEphemeral([]byte(`{"foo":"bar","bar":"baz"}`))
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"bar":"baz"}`)
}

func (*schemaSuite) TestPruneEphemeralTopLevel(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "any"
	},
	"ephemeral": true
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	data := []byte(`{
	"foo": "bar"
}`)

	data, err = schema.PruneEphemeral(data)
	c.Assert(err, IsNil)
	c.Assert(data, IsNil)
}

func (*schemaSuite) TestPruneEphemeralSeveralNested(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"type": "array",
			"values": {
				"schema": {
					"bar": {
						"type": "int",
						"ephemeral": true
					},
					"baz": "int"
				}
			}
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	data := []byte(`{
	"foo": [{"bar": 1, "baz": 2}, {"bar": 4}]
}`)

	data, err = schema.PruneEphemeral(data)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":[{"baz":2},{}]}`)
}

func (*schemaSuite) TestPruneEphemeralAlternativesNestedOk(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"values": [
				{
					"schema": {
						"foo": {
							"type": "string",
							"ephemeral": true
						}
					}
				}
			]
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	data := []byte(`{
	"foo": {
		"bar": {
			"foo": "bar"
		}
	}
}`)

	data, err = schema.PruneEphemeral(data)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":{"bar":{}}}`)
}

func (*schemaSuite) TestAlternativeMixingEphemeral(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"values": [
				{
					"type": "string",
					"ephemeral": true
				},
				"int",
				{
					"type": "number",
					"ephemeral": true
				}
			]
		}
	}
}`)
	schema, err := confdb.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	data := []byte(`{
	"foo": {
		"bar": "baz",
		"num": 1,
		"dec": 2.0
	}
}`)

	data, err = schema.PruneEphemeral(data)
	c.Assert(err, IsNil)
	// 1 wasn't pruned so it matched with the int not number (i.e., in order)
	c.Assert(string(data), Equals, `{"foo":{"num":1}}`)
}
