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

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/snapcore/snapd/overlord/state"
)

var timeType = reflect.TypeOf(time.Time{})

// ToolAnnotations carries MCP tool annotation hints.
type ToolAnnotations struct {
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`
}

// ToolTaskSupport describes whether a tool supports task-augmented execution.
type ToolTaskSupport string

const (
	ToolTaskSupportForbidden ToolTaskSupport = "forbidden"
	ToolTaskSupportOptional  ToolTaskSupport = "optional"
	ToolTaskSupportRequired  ToolTaskSupport = "required"
)

// ToolExecution carries MCP tool execution hints.
type ToolExecution struct {
	TaskSupport ToolTaskSupport `json:"taskSupport,omitempty"`
}

// ToolDescriptor describes a single MCP tool including its input schema.
type ToolDescriptor struct {
	Name         string          `json:"name"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Annotations  ToolAnnotations `json:"annotations,omitempty"`
	InputSchema  map[string]any  `json:"inputSchema"`
	OutputSchema map[string]any  `json:"outputSchema,omitempty"`
	Execution    ToolExecution   `json:"execution,omitempty"`
}

// Tool represents invocable code that LLMs can access through the Model Context Protocol.
// Tools are exposed via the tools/list RPC method and invoked via tools/call.
type TypedTool interface {
	// ArgsType returns a pointer to a struct that represents the tool's arguments.
	// This object is used as a destination for strict JSON decoding from map values.
	ArgsType() any
	// ResultType returns a pointer to a struct that represents the tool output.
	ResultType() any
	ValidateArgs(args any) error
	CallWithArgs(ctx context.Context, st *state.State, args any) (any, error)
}

// Tool represents invocable code that LLMs can access through the Model Context Protocol.
// It remains for backward compatibility; typed tools can implement TypedTool as well.
type Tool interface {
	Descriptor() ToolDescriptor
	Validate(args map[string]any) error
	Call(ctx context.Context, st *state.State, args map[string]any) (any, error)
}

// DecodeToolArgs decodes a map of raw arguments into a typed argument object.
// Unknown fields are rejected to keep validation behavior in sync with schemas.
func DecodeToolArgs(args map[string]any, argPrototype any) (any, error) {
	if argPrototype == nil {
		return nil, fmt.Errorf("arg prototype cannot be nil")
	}

	data, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal tool args: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(argPrototype); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	return argPrototype, nil
}

// ToolArgsFromMap is a convenience wrapper to decode tool args into a typed struct pointer.
func ToolArgsFromMap[T any](args map[string]any) (*T, error) {
	var typed T
	decoded, err := DecodeToolArgs(args, &typed)
	if err != nil {
		return nil, err
	}
	ret, ok := decoded.(*T)
	if !ok {
		return nil, fmt.Errorf("internal error: decoded args are not *%T", typed)
	}
	return ret, nil
}

func schemaFromType(argType any) map[string]any {
	typ := reflect.TypeOf(argType)
	if typ == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	}
	typ = indirectType(typ)
	if typ.Kind() != reflect.Struct {
		return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
	}

	return schemaFromReflectType(typ)
}

func indirectType(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	return typ
}

func schemaFromReflectType(typ reflect.Type) map[string]any {
	typ = indirectType(typ)

	if typ == timeType {
		return map[string]any{"type": "string", "format": "date-time"}
	}

	switch typ.Kind() {
	case reflect.Struct:
		return schemaFromStructType(typ)
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{
			"type":  "array",
			"items": schemaFromReflectType(typ.Elem()),
		}
	case reflect.Map:
		schema := map[string]any{"type": "object"}
		elemType := indirectType(typ.Elem())
		if elemType.Kind() != reflect.Interface {
			schema["additionalProperties"] = schemaFromReflectType(elemType)
		}
		return schema
	case reflect.Interface:
		return map[string]any{}
	default:
		return map[string]any{"type": "object"}
	}
}

func schemaFromStructType(typ reflect.Type) map[string]any {
	properties := make(map[string]any)
	required := make([]string, 0)

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}

		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			jsonName = field.Name
		}
		if jsonName == "" {
			continue
		}

		fieldSchema := schemaFromReflectType(field.Type)
		if desc := field.Tag.Get("mcp"); desc != "" {
			if strings.HasPrefix(desc, "description=") {
				fieldSchema["description"] = strings.TrimPrefix(desc, "description=")
			} else {
				fieldSchema["description"] = desc
			}
		}

		properties[jsonName] = fieldSchema
		if !strings.Contains(jsonTag, "omitempty") {
			required = append(required, jsonName)
		}
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// InputSchemaFromType returns an MCP input schema for the supplied argument struct type.
// It uses json struct tags and optional mcp:"description=..." tags.
func InputSchemaFromType(argType any) map[string]any {
	return schemaFromType(argType)
}

// OutputSchemaFromType returns an MCP output schema for the supplied result struct type.
// It mirrors InputSchemaFromType behavior for tool response objects.
func OutputSchemaFromType(argType any) map[string]any {
	return schemaFromType(argType)
}

// ResourceDescriptor describes one resource entry in resources/list.
type ResourceDescriptor struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Resource describes a readable MCP resource endpoint.
type Resource interface {
	// Descriptor returns metadata advertised via resources/list.
	Descriptor() ResourceDescriptor
	// Pattern returns the ServeMux path pattern used for resources/read routing.
	Pattern() string
	// Read resolves one resource request using explicit request context and state.
	Read(ctx context.Context, st *state.State, req *http.Request) (any, error)
}
