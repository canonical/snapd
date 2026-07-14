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

package mcpstate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/mcpstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdtool"
	. "gopkg.in/check.v1"
)

func TestMcpstate(t *testing.T) { TestingT(t) }

type serverSuite struct{}

var _ = Suite(&serverSuite{})

func (s *serverSuite) TestNotificationIsLogged(c *C) {
	buf, restore := logger.MockDebugLogger()
	defer restore()
	p := mcpstate.Manager(state.New(nil), nil, nil)

	payload := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)
	c.Check(respBytes, IsNil)

	logOutput := buf.String()
	c.Check(strings.Contains(logOutput, "received mcp notification"), Equals, true)
	c.Check(strings.Contains(logOutput, "method=notifications/initialized"), Equals, true)
}

type processorStateToolsSuite struct{}

var _ = Suite(&processorStateToolsSuite{})

func (s *processorStateToolsSuite) TestNewProcessorNilState(c *C) {
	c.Assert(func() {
		_ = mcpstate.Manager(nil, nil, nil)
	}, PanicMatches, "state cannot be nil")
}

func (s *processorStateToolsSuite) TestInitializeDefaults(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	result, rpcErr := p.Initialize(nil)
	c.Assert(rpcErr, IsNil)
	c.Assert(result.(map[string]any)["protocolVersion"], Equals, "2024-11-05")
}

func (s *processorStateToolsSuite) TestInitializeCustomVersionTrimmed(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	result, rpcErr := p.Initialize(json.RawMessage(`{"protocolVersion":"2025-01-01"}`))
	c.Assert(rpcErr, IsNil)
	res := result.(map[string]any)
	c.Check(res["protocolVersion"], Equals, "2025-01-01")
}

func (s *processorStateToolsSuite) TestInitializeInvalidParams(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	_, rpcErr := p.Initialize(json.RawMessage(`{"protocolVersion":{}}`))
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(rpcErr.Message, Equals, "invalid initialize params")
}

func (s *processorStateToolsSuite) TestInitializeServerInfoVersion(c *C) {
	restore := snapdtool.MockVersion("9.9.9-test")
	defer restore()

	p := mcpstate.Manager(state.New(nil), nil, nil)

	result, rpcErr := p.Initialize(nil)
	c.Assert(rpcErr, IsNil)

	res := result.(map[string]any)
	serverInfo := res["serverInfo"].(map[string]any)
	c.Check(serverInfo["name"], Equals, "snapd-mcp")
	c.Check(serverInfo["version"], Equals, "9.9.9-test")
}

func (s *processorStateToolsSuite) TestCallToolInvalidParamsJSON(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	result, rpcErr := p.CallTool(context.Background(), json.RawMessage(`{"name":`))
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(rpcErr.Message, Equals, "invalid tools/call params")
}

func (s *processorStateToolsSuite) TestCallToolRejectsUnknownTool(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"name":"snap_bad_tool","arguments":{}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(rpcErr.Message, Equals, "unknown tool")
}

func (s *processorStateToolsSuite) TestNewProcessorIgnoresDuplicateToolNames(c *C) {
	buf, restore := logger.MockLogger()
	defer restore()

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{TestTool{}, TestTool{}}, nil)
	c.Assert(p, NotNil)

	tools := p.ListTools()
	c.Check(len(tools), Equals, 1)
	c.Check(strings.Contains(buf.String(), `WARNING: duplicate mcp tool name "test_tool" ignored`), Equals, true)
}

func (s *processorStateToolsSuite) TestNewProcessorIgnoresDuplicateResourcePatterns(c *C) {
	buf, restore := logger.MockLogger()
	defer restore()

	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{TestResource{}, TestResource{}})
	c.Assert(p, NotNil)

	resources := p.ListResources()
	c.Check(len(resources), Equals, 1)
	c.Check(strings.Contains(buf.String(), `WARNING: duplicate mcp resource pattern "/test/" ignored`), Equals, true)
}

func (s *processorStateToolsSuite) TestProcessRequestInvalidJSON(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	payload := []byte(`{invalid json`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)

	var resp map[string]any
	err = json.Unmarshal(respBytes, &resp)
	c.Assert(err, IsNil)
	c.Check(resp["jsonrpc"], Equals, "2.0")
	c.Check(resp["error"], NotNil)
	errMap := resp["error"].(map[string]any)
	c.Check(errMap["code"], Equals, float64(mcpstate.RPCParseError))
}

func (s *processorStateToolsSuite) TestProcessRequestInvalidVersion(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	payload := []byte(`{"jsonrpc":"1.0","id":1,"method":"ping"}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)

	var resp map[string]any
	err = json.Unmarshal(respBytes, &resp)
	c.Assert(err, IsNil)
	c.Check(resp["error"], NotNil)
	errMap := resp["error"].(map[string]any)
	c.Check(errMap["code"], Equals, float64(mcpstate.RPCInvalidRequest))
}

func (s *processorStateToolsSuite) TestProcessRequestMissingMethod(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":""}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)

	var resp map[string]any
	err = json.Unmarshal(respBytes, &resp)
	c.Assert(err, IsNil)
	errMap := resp["error"].(map[string]any)
	c.Check(errMap["code"], Equals, float64(mcpstate.RPCInvalidRequest))
}

func (s *processorStateToolsSuite) TestProcessRequestPing(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	payload := []byte(`{"jsonrpc":"2.0","id":"123","method":"ping"}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)

	var resp map[string]any
	err = json.Unmarshal(respBytes, &resp)
	c.Assert(err, IsNil)
	c.Check(resp["jsonrpc"], Equals, "2.0")
	c.Check(resp["id"], NotNil)
	c.Check(resp["error"], IsNil)
	c.Check(resp["result"], NotNil)
}

func (s *processorStateToolsSuite) TestProcessRequestToolsList(c *C) {
	tool := TestTool{}
	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	payload := []byte(`{"jsonrpc":"2.0","id":"123","method":"tools/list"}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)

	var resp map[string]any
	err = json.Unmarshal(respBytes, &resp)
	c.Assert(err, IsNil)
	c.Check(resp["result"], NotNil)
	result := resp["result"].(map[string]any)
	c.Check(result["tools"], NotNil)
	tools := result["tools"].([]any)
	c.Check(len(tools), Equals, 1)
}

func (s *processorStateToolsSuite) TestProcessRequestResourcesList(c *C) {
	resource := TestResource{}
	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{resource})

	payload := []byte(`{"jsonrpc":"2.0","id":"123","method":"resources/list"}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)

	var resp map[string]any
	err = json.Unmarshal(respBytes, &resp)
	c.Assert(err, IsNil)
	c.Check(resp["result"], NotNil)
	result := resp["result"].(map[string]any)
	c.Check(result["resources"], NotNil)
	resources := result["resources"].([]any)
	c.Check(len(resources), Equals, 1)
}

func (s *processorStateToolsSuite) TestProcessRequestNotificationNoResponse(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	payload := []byte(`{"jsonrpc":"2.0","method":"ping"}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)
	c.Check(respBytes, IsNil)
}

func (s *processorStateToolsSuite) TestProcessRequestUnknownMethod(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	payload := []byte(`{"jsonrpc":"2.0","id":"123","method":"unknown/method"}`)
	respBytes, err := p.ProcessRequest(context.Background(), payload)
	c.Assert(err, IsNil)

	var resp map[string]any
	err = json.Unmarshal(respBytes, &resp)
	c.Assert(err, IsNil)
	errMap := resp["error"].(map[string]any)
	c.Check(errMap["code"], Equals, float64(mcpstate.RPCMethodNotFound))
}

func (s *processorStateToolsSuite) TestInitializeWithWhitespaceVersion(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	result, rpcErr := p.Initialize(json.RawMessage(`{"protocolVersion":"  2025-01-01  "}`))
	c.Assert(rpcErr, IsNil)
	res := result.(map[string]any)
	c.Check(res["protocolVersion"], Equals, "2025-01-01")
}

func (s *processorStateToolsSuite) TestInitializeWithEmptyVersion(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	result, rpcErr := p.Initialize(json.RawMessage(`{"protocolVersion":"   "}`))
	c.Assert(rpcErr, IsNil)
	res := result.(map[string]any)
	c.Check(res["protocolVersion"], Equals, "2024-11-05")
}

type TestTool struct{}

func (t TestTool) Descriptor() mcpstate.ToolDescriptor {
	return mcpstate.ToolDescriptor{Name: "test_tool"}
}

func (t TestTool) Validate(args map[string]any) error {
	return nil
}

func (t TestTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	return nil, nil
}

type TestResource struct{}

func (r TestResource) Descriptor() mcpstate.ResourceDescriptor {
	return mcpstate.ResourceDescriptor{URI: "snap://test/{id}"}
}

func (r TestResource) Pattern() string {
	return "/test/"
}

func (r TestResource) Read(ctx context.Context, st *state.State, req *http.Request) (any, error) {
	return nil, nil
}
