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
	"net/http"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdtool"
)

const defaultProtocolVersion = "2024-11-05"

// MCPManager handles MCP JSON-RPC requests backed by snapd state.
type MCPManager struct {
	st             *state.State
	tools          []Tool
	toolByName     map[string]Tool
	resources      []Resource
	resourceRouter *http.ServeMux
}

// Ensure implements the overlord StateManager interface.
func (p *MCPManager) Ensure() error {
	return nil
}

// Manager constructs an MCP manager.
//
// It panics if mandatory runtime dependencies are invalid or if the provided
// tool/resource registrations cannot be safely wired (for example conflicting
// resource route patterns).
func Manager(st *state.State, tools []Tool, resources []Resource) *MCPManager {
	if st == nil {
		panic("state cannot be nil")
	}

	if tools == nil {
		tools = mcp.AllTools()
	}

	if resources == nil {
		resources = mcp.AllResources()
	}

	acceptedTools := make([]Tool, 0, len(tools))
	toolByName := make(map[string]Tool, len(tools))
	for _, t := range tools {
		name := t.Descriptor().Name
		if _, exists := toolByName[name]; exists {
			logger.Noticef("WARNING: duplicate mcp tool name %q ignored", name)
			continue
		}
		toolByName[name] = t
		acceptedTools = append(acceptedTools, t)
	}

	router := http.NewServeMux()
	acceptedResources := make([]Resource, 0, len(resources))
	resourceByPattern := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		pattern := resource.Pattern()
		if _, exists := resourceByPattern[pattern]; exists {
			logger.Noticef("WARNING: duplicate mcp resource pattern %q ignored", pattern)
			continue
		}
		resourceByPattern[pattern] = struct{}{}

		registered := false
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Noticef("WARNING: mcp resource route pattern %q ignored: %v", pattern, rec)
				}
			}()

			router.Handle(pattern, resourceRouteHandler{resource: resource})
			registered = true
		}()

		if registered {
			acceptedResources = append(acceptedResources, resource)
		}
	}

	return &MCPManager{
		st:             st,
		tools:          acceptedTools,
		toolByName:     toolByName,
		resources:      acceptedResources,
		resourceRouter: router,
	}
}

type requestEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type responseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error codes defined by the JSON-RPC 2.0 specification.
// See https://www.jsonrpc.org/specification#error_object
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

// ProcessRequest processes a single JSON-RPC request payload.
//
// The JSON-RPC 2.0 specification is at https://www.jsonrpc.org/specification
//
// It returns nil response bytes for notifications (requests without an id).
func (p *MCPManager) ProcessRequest(ctx context.Context, payload []byte) ([]byte, error) {
	var req requestEnvelope
	if err := json.Unmarshal(payload, &req); err != nil {
		return json.Marshal(&responseEnvelope{
			JSONRPC: "2.0",
			Error:   &responseError{Code: rpcParseError, Message: err.Error()},
		})
	}

	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		return json.Marshal(&responseEnvelope{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &responseError{Code: rpcInvalidRequest, Message: "invalid request"},
		})
	}

	result, rpcErr := p.handleRequest(ctx, req.Method, req.Params)
	if len(req.ID) == 0 {
		msg := fmt.Sprintf("received mcp notification method=%s", req.Method)
		if len(req.Params) > 0 {
			msg += fmt.Sprintf(" params=%s", req.Params)
		}
		if rpcErr != nil {
			msg += fmt.Sprintf(" error_code=%d error=%q", rpcErr.Code, rpcErr.Message)
		}
		logger.Debugf(msg)
		return nil, nil
	}

	resp := &responseEnvelope{JSONRPC: "2.0", ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}

	return json.Marshal(resp)
}

func (p *MCPManager) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, *responseError) {
	switch method {
	case "initialize":
		return p.initialize(params)
	case "notifications/initialized":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": p.listTools()}, nil
	case "tools/call":
		return p.callTool(ctx, params)
	case "resources/list":
		return p.listResources(), nil
	case "resources/read":
		return p.readResource(ctx, params)
	default:
		return nil, &responseError{Code: rpcMethodNotFound, Message: "method not found"}
	}
}

func (p *MCPManager) initialize(params json.RawMessage) (any, *responseError) {
	version := defaultProtocolVersion
	if len(params) > 0 {
		var req initializeParams
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &responseError{Code: rpcInvalidParams, Message: "invalid initialize params"}
		}
		if strings.TrimSpace(req.ProtocolVersion) != "" {
			version = strings.TrimSpace(req.ProtocolVersion)
		}
	}

	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "snapd-mcp",
			"version": snapdtool.Version,
		},
	}, nil
}
