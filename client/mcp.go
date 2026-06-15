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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// MCPResult describes the JSON-RPC payload returned by the daemon MCP endpoint.
// Notifications have no corresponding response and are represented with
// HasResponse set to false.
type MCPResult struct {
	Payload     json.RawMessage
	HasResponse bool
}

// MCP sends one MCP JSON-RPC payload to the daemon MCP endpoint and returns
// the inner MCP response payload from the snapd sync envelope.
func (client *Client) MCP(ctx context.Context, payload []byte) (MCPResult, error) {
	// Force interactive authorization on every call. The MCP bridge normally
	// runs without a controlling TTY (e.g. as a background agent for Copilot
	// or similar tools), but polkit still needs to be able to pop an auth
	// dialog when the daemon requires it for the blanket /v2/mcp access.
	//
	// In the future individual tools may carry their own authorization
	// requirements, mirroring the per-endpoint access-control model already
	// used by the REST API. At that point this flag can become conditional on
	// the specific tool being invoked rather than being set unconditionally.
	client.interactive = true

	rsp, err := client.raw(ctx, "POST", "/v2/mcp", nil, nil, bytes.NewReader(payload))
	if err != nil {
		return MCPResult{}, err
	}
	defer rsp.Body.Close()

	var envelope response
	if err := decodeInto(rsp.Body, &envelope); err != nil {
		return MCPResult{}, err
	}
	if err := envelope.err(client, rsp.StatusCode); err != nil {
		return MCPResult{}, err
	}
	if envelope.Type != "sync" {
		return MCPResult{}, fmt.Errorf("expected sync response, got %q", envelope.Type)
	}

	client.warningCount = envelope.WarningCount
	client.warningTimestamp = envelope.WarningTimestamp

	if len(envelope.Result) == 0 {
		return MCPResult{}, fmt.Errorf("missing MCP response payload in sync result")
	}
	if string(envelope.Result) == "null" {
		return MCPResult{}, nil
	}

	return MCPResult{
		Payload:     envelope.Result,
		HasResponse: true,
	}, nil
}
