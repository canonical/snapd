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
	c.Assert(schema, NotNil)

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
	c.Assert(schema, NotNil)

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
	c.Assert(schema, NotNil)

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
	c.Assert(schema, NotNil)

	err = schema.Validate(input)
	c.Assert(err, ErrorMatches, `string "F00" doesn't match schema pattern \[fb\]00`)
}

func (*schemaSuite) TestParseAndValidateSchemaWithInts(c *C) {
	schemaStr := []byte(`{
  "schema": {
    "foo": "int",
		"bar": {
			"type": "int",
			"min": 0,
			"max": 100,
			"choices": [1, 2, 3]
		}
  }
}`)

	input := []byte(`{
  "foo": 5,
	"bar": 3
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)
	c.Assert(schema, NotNil)

	err = schema.Validate(input)
	c.Assert(err, IsNil)
}

func (*schemaSuite) TestIntegerMustMatchConstraints(c *C) {
	schemaStr := []byte(`{
  "schema": {
		"foo": {
			"type": "int",
			"min": 0,
			"max": 100,
			"choices": [1, 2, 3]
		}
  }
}`)

	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)
	c.Assert(schema, NotNil)

	type testcase struct {
		name string
		num  int
		err  string
	}
	tcs := []testcase{
		{
			name: "less than min",
			num:  -1,
			err:  `integer -1 is less than allowed minimum 0`,
		},
		{
			name: "greater than max",
			num:  101,
			err:  `integer 101 is greater than allowed maximum 100`,
		},
		{
			name: "not one of allowed choices",
			num:  4,
			err:  `integer 4 is not one of the allowed choices`,
		},
	}
	for _, tc := range tcs {
		input := []byte(fmt.Sprintf(`{
  "foo": %d
}`, tc.num))

		cmt := Commentf("subtest: %s", tc.name)
		err = schema.Validate(input)
		c.Assert(err, ErrorMatches, tc.err, cmt)
	}
}
