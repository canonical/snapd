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

func (*schemaSuite) TestParseSchemaExample(c *C) {
	schemaStr := []byte(`{
  "types": {
    "snap-name": {
      "type": "string",
      "pattern": "^[a-z0-9-]*[a-z][a-z0-9-]*$"
    }
  },
  "schema": {
    "snaps": {
      "keys": "$snap-name",
      "values": {
        "schema": {
          "name": "$snap-name",
          "version": "string",
          "status": "string"
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
      "status": "active"
    }
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestParseAndValidateSchemaWithStringsHappy(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "snaps": {
      "keys": "string",
      "values": {
        "schema": {
          "name": "string"
        }
      }
    }
  }
}`)

	input := []byte(`{
  "snaps": {
    "core20": {
      "name": "core20"
    },
    "snapd": {
      "name": "snapd"
    }
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestStringChoicesHappy(c *C) {
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
			"foo": 1,
			"bar": 2
		}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestStringNotAllowedChoice(c *C) {
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
			"foo": 1,
			"boo": 2
		}
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `string "boo" is not one of the allowed choices`)
}

func (*schemaSuite) TestStringBadChoices(c *C) {
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
	c.Assert(err, ErrorMatches, `.*cannot have "choices" constraint with empty list: field must be populated or not exist`)
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

func (*schemaSuite) TestMapKeysStringBased(c *C) {
	schemaStr := []byte(`{
  "types": {
    "patt": {
      "type": "string",
      "pattern": "[fb]oo"
    }
  },
  "schema": {
    "pattern": {
      "keys": {
        "pattern": "[fb]oo"
      }
    },
    "userType": {
      "keys": "$patt"
    }
  }
}`)

	input := []byte(`{
  "pattern": {
    "foo": 1
  },
  "userType": {
    "boo": 2
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestUnknownFieldInMap(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "foo": "string"
  }
}`)

	input := []byte(`{
  "foo": "bar",
  "oof": "baz"
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `unexpected field "oof" in map`)
}

func (*schemaSuite) TestFieldNoMatch(c *C) {
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

func (*schemaSuite) TestIntegerMustMatchChoices(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "foo": {
      "type": "int",
      "choices": [1,  3]
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
			c.Assert(err, ErrorMatches, fmt.Sprintf(`integer %d is not one of the allowed choices`, num))
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
		if num < 1 {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`integer %d is less than allowed minimum %d`, num, min))
		} else if num > max {

			c.Assert(err, ErrorMatches, fmt.Sprintf(`integer %d is greater than allowed maximum %d`, num, max))
		} else {
			c.Assert(err, IsNil)
		}
	}
}

func (*schemaSuite) TestIntegerMinMaxAndChoicesFail(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "foo": {
      "type": "int",
			"min": 0,
			"max": 100,
			"choices": [1, 2]
    }
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, ErrorMatches, `.*cannot have "choices" and "min" constraints`)
	c.Assert(schema, IsNil)
}
