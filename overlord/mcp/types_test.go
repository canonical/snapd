// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package mcp_test

import (
	"time"

	"github.com/snapcore/snapd/overlord/mcp"
	. "gopkg.in/check.v1"
)

type typesSuite struct{}

var _ = Suite(&typesSuite{})

type decodeArgs struct {
	Name string `json:"name"`
	Age  int    `json:"age,omitempty"`
}

type schemaInput struct {
	Name    string            `json:"name" mcp:"description=tool name"`
	Enabled bool              `json:"enabled,omitempty"`
	Count   int               `json:"count"`
	Ratio   float64           `json:"ratio,omitempty"`
	Meta    map[string]string `json:"meta,omitempty" mcp:"metadata map"`
	Labels  []string          `json:"labels,omitempty"`
	Child   struct {
		ID string `json:"id"`
	} `json:"child,omitempty"`
	Dynamic map[string]struct {
		Value int `json:"value"`
	} `json:"dynamic,omitempty"`
	When     time.Time `json:"when,omitempty"`
	PtrCount *int      `json:"ptrCount,omitempty"`
	Hidden   string    `json:"-"`
	Alias    string
}

func (s *typesSuite) TestDecodeToolArgsNilPrototype(c *C) {
	_, err := mcp.DecodeToolArgs(map[string]any{"name": "alpha"}, nil)
	c.Assert(err, ErrorMatches, "arg prototype cannot be nil")
}

func (s *typesSuite) TestDecodeToolArgsMarshalFailure(c *C) {
	_, err := mcp.DecodeToolArgs(map[string]any{"cb": func() {}}, &decodeArgs{})
	c.Assert(err, ErrorMatches, "cannot marshal tool args:.*")
}

func (s *typesSuite) TestDecodeToolArgsRejectsUnknownFields(c *C) {
	_, err := mcp.DecodeToolArgs(map[string]any{"name": "alpha", "extra": "nope"}, &decodeArgs{})
	c.Assert(err, ErrorMatches, "invalid arguments: json: unknown field \"extra\"")
}

func (s *typesSuite) TestDecodeToolArgsRejectsWrongTypes(c *C) {
	_, err := mcp.DecodeToolArgs(map[string]any{"name": 99}, &decodeArgs{})
	c.Assert(err, ErrorMatches, "invalid arguments: json: cannot unmarshal number into Go struct field decodeArgs.name of type string")
}

func (s *typesSuite) TestDecodeToolArgsSuccess(c *C) {
	decoded, err := mcp.DecodeToolArgs(map[string]any{"name": "alpha", "age": 42}, &decodeArgs{})
	c.Assert(err, IsNil)

	typed, ok := decoded.(*decodeArgs)
	c.Assert(ok, Equals, true)
	c.Check(typed.Name, Equals, "alpha")
	c.Check(typed.Age, Equals, 42)
}

func (s *typesSuite) TestToolArgsFromMap(c *C) {
	typed, err := mcp.ToolArgsFromMap[decodeArgs](map[string]any{"name": "alpha", "age": 1})
	c.Assert(err, IsNil)
	c.Check(typed.Name, Equals, "alpha")
	c.Check(typed.Age, Equals, 1)

	_, err = mcp.ToolArgsFromMap[decodeArgs](map[string]any{"unknown": true})
	c.Assert(err, ErrorMatches, "invalid arguments: json: unknown field \"unknown\"")
}

func (s *typesSuite) TestInputSchemaFromTypeNilAndNonStruct(c *C) {
	nilSchema := mcp.InputSchemaFromType(nil)
	c.Check(nilSchema["type"], Equals, "object")
	c.Check(nilSchema["additionalProperties"], Equals, false)
	c.Check(nilSchema["properties"], DeepEquals, map[string]any{})

	nonStructSchema := mcp.InputSchemaFromType(5)
	c.Check(nonStructSchema["type"], Equals, "object")
	c.Check(nonStructSchema["additionalProperties"], Equals, false)
	c.Check(nonStructSchema["properties"], DeepEquals, map[string]any{})
}

func (s *typesSuite) TestInputSchemaFromTypeStruct(c *C) {
	schema := mcp.InputSchemaFromType(&schemaInput{})

	c.Check(schema["type"], Equals, "object")
	c.Check(schema["additionalProperties"], Equals, false)

	properties := schema["properties"].(map[string]any)
	c.Check(properties["name"].(map[string]any)["type"], Equals, "string")
	c.Check(properties["name"].(map[string]any)["description"], Equals, "tool name")
	c.Check(properties["enabled"].(map[string]any)["type"], Equals, "boolean")
	c.Check(properties["count"].(map[string]any)["type"], Equals, "integer")
	c.Check(properties["ratio"].(map[string]any)["type"], Equals, "number")
	c.Check(properties["meta"].(map[string]any)["type"], Equals, "object")
	c.Check(properties["meta"].(map[string]any)["description"], Equals, "metadata map")
	c.Check(properties["when"].(map[string]any)["type"], Equals, "string")
	c.Check(properties["when"].(map[string]any)["format"], Equals, "date-time")
	c.Check(properties["ptrCount"].(map[string]any)["type"], Equals, "integer")
	c.Check(properties["Alias"].(map[string]any)["type"], Equals, "string")

	_, hasHidden := properties["hidden"]
	c.Check(hasHidden, Equals, false)
	_, hasPrivate := properties["private"]
	c.Check(hasPrivate, Equals, false)

	required, ok := schema["required"].([]string)
	c.Assert(ok, Equals, true)
	c.Check(required, DeepEquals, []string{"name", "count", "Alias"})
}

func (s *typesSuite) TestOutputSchemaFromType(c *C) {
	input := &struct {
		Status string `json:"status"`
		Items  []struct {
			Name string `json:"name"`
		} `json:"items,omitempty"`
	}{Status: "ok"}

	schema := mcp.OutputSchemaFromType(input)
	properties := schema["properties"].(map[string]any)
	c.Check(properties["status"].(map[string]any)["type"], Equals, "string")
	c.Check(properties["items"].(map[string]any), DeepEquals, map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"name": map[string]any{"type": "string"}},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
	})
}
