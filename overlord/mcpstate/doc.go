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

// Package mcpstate implements a Model Context Protocol server.
//
// The end-to-end request flow is:
//
//  1. An MCP host writes line-delimited JSON-RPC frames to "snap mcp" over
//     stdio.
//  2. The "snap mcp" bridge forwards each frame to snapd as POST /v2/mcp over
//     the snapd socket.
//  3. The daemon endpoint retrieves the MCP manager from the overlord and calls
//     MCPManager.ProcessRequest with the request body.
//  4. MCPManager routes the JSON-RPC method to initialize, tools/list,
//     tools/call, resources/list, or resources/read handlers. Tools are
//     interface implementations resolved by name; resources are interface
//     implementations resolved through an internal stdlib HTTP router.
//  5. For daemon-side use, state-backed helpers read snap and connection data
//     directly from overlord state, including manual connection flags.
//  6. MCPManager returns a JSON-RPC response payload.
//  7. The daemon wraps the response and the "snap mcp" bridge unwraps it
//     and writes the inner JSON-RPC response to stdout.
package mcpstate
