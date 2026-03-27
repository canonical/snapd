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
)

// Export private constants for testing.
const (
	RPCInvalidParams  = rpcInvalidParams
	RPCParseError     = rpcParseError
	RPCInvalidRequest = rpcInvalidRequest
	RPCMethodNotFound = rpcMethodNotFound
	RPCInternalError  = rpcInternalError
)

// Export private methods for testing.

func (m *MCPManager) Initialize(params json.RawMessage) (any, *ResponseError) {
	return m.initialize(params)
}

func (m *MCPManager) CallTool(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
	return m.callTool(ctx, params)
}

func (m *MCPManager) ReadResource(ctx context.Context, params json.RawMessage) (any, *ResponseError) {
	return m.readResource(ctx, params)
}

func (m *MCPManager) ListResources() map[string]any {
	return m.listResources()
}

func (m *MCPManager) ListTools() []ToolDescriptor {
	return m.listTools()
}

// ValidateResourceReadInput calls the private validateResourceReadInput function.
func ValidateResourceReadInput(uri string) error {
	return validateResourceReadInput(uri)
}

// ResponseError is the exported type for response errors.
type ResponseError = responseError
