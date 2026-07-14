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
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/snapcore/snapd/client"
	. "gopkg.in/check.v1"
)

type mcpSuite struct{}

var _ = Suite(&mcpSuite{})

type fakeMCPBridgeClient struct {
	response client.MCPResult
	err      error
	body     string
}

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func (f *fakeMCPBridgeClient) MCP(ctx context.Context, payload []byte) (client.MCPResult, error) {
	_ = ctx
	f.body = string(payload)
	if f.err != nil {
		return client.MCPResult{}, f.err
	}
	return f.response, nil
}

func (s *mcpSuite) TestBridgeToMCPEndpointWritesDaemonResponse(c *C) {
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	stdout := &bytes.Buffer{}
	client := &fakeMCPBridgeClient{
		response: client.MCPResult{Payload: []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`), HasResponse: true},
	}

	err := bridgeToMCPEndpoint(stdin, stdout, client)
	c.Assert(err, IsNil)
	c.Check(client.body, Equals, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	c.Check(stdout.String(), Equals, `{"jsonrpc":"2.0","id":1,"result":{}}`+"\n")
}

func (s *mcpSuite) TestBridgeSkipsNotificationWithNullResult(c *C) {
	stdin := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	stdout := &bytes.Buffer{}
	client := &fakeMCPBridgeClient{
		response: client.MCPResult{},
	}

	err := bridgeToMCPEndpoint(stdin, stdout, client)
	c.Assert(err, IsNil)
	// Notifications receive no MCP response
	c.Check(stdout.String(), Equals, "")
}

func (s *mcpSuite) TestBridgeToMCPEndpointReportsDecodeError(c *C) {
	// Malformed JSON (single open brace) is forwarded to the daemon as-is.
	stdin := strings.NewReader("{\n")
	stdout := &bytes.Buffer{}
	client := &fakeMCPBridgeClient{
		response: client.MCPResult{Payload: []byte(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"cannot decode request: unexpected end of JSON input"}}`), HasResponse: true},
	}

	err := bridgeToMCPEndpoint(stdin, stdout, client)
	c.Assert(err, IsNil)
	c.Check(stdout.String(), Matches, `(?s).*"message":"cannot decode request: .*".*`)
}

func (s *mcpSuite) TestBridgeToMCPEndpointReportsSnapdRequestError(c *C) {
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	stdout := &bytes.Buffer{}
	client := &fakeMCPBridgeClient{err: errors.New("boom")}

	err := bridgeToMCPEndpoint(stdin, stdout, client)
	c.Assert(err, IsNil)
	c.Check(stdout.String(), Matches, `(?s).*"message":"cannot send request to snapd: boom".*`)
}

func (s *mcpSuite) TestBridgeToMCPEndpointTranslatesToSnapdHTTPCall(c *C) {
	var seenMethod, seenPath, seenBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)
		seenBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write([]byte(`{"type":"sync","result":{"jsonrpc":"2.0","id":1,"result":{"pong":true}}}`))
		c.Assert(err, IsNil)
	}))
	defer srv.Close()

	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	stdout := &bytes.Buffer{}
	cli := client.New(&client.Config{BaseURL: srv.URL, DisableAuth: true})

	err := bridgeToMCPEndpoint(stdin, stdout, cli)
	c.Assert(err, IsNil)
	c.Check(seenMethod, Equals, "POST")
	c.Check(seenPath, Equals, "/v2/mcp")
	c.Check(seenBody, Equals, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	c.Check(stdout.String(), Equals, `{"jsonrpc":"2.0","id":1,"result":{"pong":true}}`+"\n")
}

func (s *mcpSuite) TestReadFrameSkipsEmptyLines(c *C) {
	frame, err := readFrame(bufio.NewReader(strings.NewReader("\n\r\n{\"jsonrpc\":\"2.0\"}\n")))
	c.Assert(err, IsNil)
	c.Check(string(frame), Equals, `{"jsonrpc":"2.0"}`)
}

func (s *mcpSuite) TestReadFrameReturnsPartialLastLine(c *C) {
	frame, err := readFrame(bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0"}`)))
	c.Assert(err, IsNil)
	c.Check(string(frame), Equals, `{"jsonrpc":"2.0"}`)
}

func (s *mcpSuite) TestWriteFrameMarshalError(c *C) {
	err := writeFrame(&bytes.Buffer{}, map[string]any{"bad": func() {}})
	c.Assert(err, NotNil)
}

func (s *mcpSuite) TestBridgeToMCPEndpointReturnsWriteError(c *C) {
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	client := &fakeMCPBridgeClient{response: client.MCPResult{Payload: []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`), HasResponse: true}}

	err := bridgeToMCPEndpoint(stdin, errWriter{}, client)
	c.Assert(err, ErrorMatches, `write failed`)
}

func (s *mcpSuite) TestBridgeRejectsEmptyResponse(c *C) {
	stdin := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	stdout := &bytes.Buffer{}
	client := &fakeMCPBridgeClient{response: client.MCPResult{HasResponse: true}}

	err := bridgeToMCPEndpoint(stdin, stdout, client)
	c.Assert(err, ErrorMatches, `cannot forward empty response from snapd`)
	c.Check(stdout.String(), Equals, "")
}

func (s *mcpSuite) TestReadFrameEOF(c *C) {
	frame, err := readFrame(bufio.NewReader(strings.NewReader("")))
	c.Check(frame, IsNil)
	c.Assert(err, Equals, io.EOF)
}

func (s *mcpSuite) TestWriteFrameSuccess(c *C) {
	buf := &bytes.Buffer{}
	err := writeFrame(buf, map[string]any{"jsonrpc": "2.0"})
	c.Assert(err, IsNil)
	c.Check(buf.String(), Equals, "{\"jsonrpc\":\"2.0\"}\n")
}

func (s *mcpSuite) TestCmdMCPExecute(c *C) {
	oldStdin, oldStdout := Stdin, Stdout
	defer func() {
		Stdin = oldStdin
		Stdout = oldStdout
	}()

	Stdin = strings.NewReader("")
	Stdout = &bytes.Buffer{}

	cmd := &cmdMCP{}
	c.Check(cmd.Execute([]string{"extra"}), Equals, ErrExtraArgs)
	c.Check(cmd.Execute(nil), IsNil)
}
