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

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type mcpBridgeClient interface {
	MCP(ctx context.Context, payload []byte) (client.MCPResult, error)
}

type cmdMCP struct {
	clientMixin
}

var shortMCPHelp = i18n.G("Bridge to the Model Context Protocol endpoint")
var longMCPHelp = i18n.G(`
The mcp command acts as a bridge to the Model Context Protocol (MCP)
endpoint exposed by snapd. It reads MCP requests from stdin and writes
responses to stdout.

This command is typically used by MCP clients like Copilot or Lemonade
to query information about snaps on the local system.
`)

func init() {
	cmd := addCommand("mcp",
		shortMCPHelp,
		longMCPHelp,
		func() flags.Commander {
			return &cmdMCP{}
		}, map[string]string{},
		[]argDesc{},
	)
	cmd.hidden = true
}

func (x *cmdMCP) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return bridgeToMCPEndpoint(Stdin, Stdout, x.client)
}

func bridgeToMCPEndpoint(stdin io.Reader, stdout io.Writer, cl mcpBridgeClient) error {
	reader := bufio.NewReader(stdin)
	for {
		// Read frame (skip empty lines)
		payload, err := readFrame(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			// Send parse error response
			errResp := map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32700,
					"message": fmt.Sprintf("cannot decode request: %v", err),
				},
			}
			_ = writeFrame(stdout, errResp)
			continue
		}

		// Send request to the daemon's MCP endpoint
		response, err := cl.MCP(context.Background(), payload)
		if err != nil {
			// Send error response
			errResp := map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32603,
					"message": fmt.Sprintf("cannot send request to snapd: %v", err),
				},
			}
			_ = writeFrame(stdout, errResp)
			continue
		}

		if !response.HasResponse {
			continue
		}
		if len(response.Payload) == 0 {
			return fmt.Errorf("cannot forward empty response from snapd")
		}

		// Write the extracted MCP JSON-RPC response frame
		if _, err := stdout.Write(response.Payload); err != nil {
			return err
		}
		if _, err := stdout.Write([]byte{'\n'}); err != nil {
			return err
		}
	}
}

// readFrame reads a line-delimited JSON frame from the reader, skipping empty lines.
func readFrame(r *bufio.Reader) ([]byte, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" {
				return nil, io.EOF
			}
			if err == io.EOF && len(line) > 0 {
				// partial last line with no terminating newline
				return []byte(strings.TrimRight(line, "\r\n")), nil
			}
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			continue // skip empty lines
		}
		return []byte(trimmed), nil
	}
}

// writeFrame writes a JSON-marshaled object followed by a newline.
func writeFrame(w io.Writer, payload any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	_, err = w.Write(buf)
	return err
}
