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

package mcpstate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/overlord/mcp"
)

type Tool = mcp.Tool
type ToolDescriptor = mcp.ToolDescriptor
type ToolAnnotations = mcp.ToolAnnotations
type Resource = mcp.Resource
type ResourceDescriptor = mcp.ResourceDescriptor

func (p *MCPManager) listTools() []ToolDescriptor {
	descriptors := make([]ToolDescriptor, 0, len(p.tools))
	for _, t := range p.tools {
		descriptors = append(descriptors, t.Descriptor())
	}
	return descriptors
}

func (p *MCPManager) callTool(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, &responseError{Code: rpcInvalidParams, Message: "invalid tools/call params"}
	}

	t, ok := p.toolByName[req.Name]
	if !ok {
		return nil, &responseError{Code: rpcInvalidParams, Message: "unknown tool"}
	}

	if typed, ok := t.(mcp.TypedTool); ok {
		decodedArgs, err := mcp.DecodeToolArgs(req.Arguments, typed.ArgsType())
		if err != nil {
			return nil, &responseError{Code: rpcInvalidParams, Message: err.Error()}
		}

		if err := typed.ValidateArgs(decodedArgs); err != nil {
			return nil, &responseError{Code: rpcInvalidParams, Message: fmt.Sprintf("invalid arguments: %v", err)}
		}

		result, err := typed.CallWithArgs(ctx, p.st, decodedArgs)
		if err != nil {
			return toolError(err), nil
		}
		return toolSuccess(result), nil
	}

	if err := t.Validate(req.Arguments); err != nil {
		return nil, &responseError{Code: rpcInvalidParams, Message: fmt.Sprintf("invalid arguments: %v", err)}
	}

	result, err := t.Call(ctx, p.st, req.Arguments)
	if err != nil {
		return toolError(err), nil
	}
	return toolSuccess(result), nil
}

func toolSuccess(result any) map[string]any {
	return map[string]any{
		"isError":           false,
		"structuredContent": structuredContentObject(result),
		"content": []map[string]any{{
			"type": "text",
			"text": jsonString(result),
		}},
	}
}

func structuredContentObject(result any) map[string]any {
	if result == nil {
		return map[string]any{}
	}

	data, err := json.Marshal(result)
	if err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}

	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil && obj != nil {
		return obj
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return map[string]any{"marshal_error": err.Error()}
	}

	return map[string]any{"result": value}
}

func toolError(err error) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{{
			"type": "text",
			"text": err.Error(),
		}},
	}
}

func jsonString(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(data)
}
