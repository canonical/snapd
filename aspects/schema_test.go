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
	"github.com/snapcore/snapd/aspects"
	. "gopkg.in/check.v1"
)

type schemaSuite struct{}

var _ = Suite(&schemaSuite{})

func (*schemaSuite) TestParseSchema(c *C) {
	schemaStr := `{
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
}`

	input := `{
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
}`

	schema, err := aspects.ParseSchema([]byte(schemaStr))
	c.Assert(err, IsNil)
	c.Assert(schema, NotNil)

	err = schema.Validate([]byte(input))
	c.Assert(err, IsNil)
}
