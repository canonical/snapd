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
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/mcpstate"
	"github.com/snapcore/snapd/overlord/state"
	. "gopkg.in/check.v1"
)

type toolsSuite struct{}

var _ = Suite(&toolsSuite{})

func (s *toolsSuite) SetUpTest(c *C) {
	mcp.ResetRegistryForTesting()
}

func (s *toolsSuite) TestListToolsEmpty(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	tools := p.ListTools()
	c.Assert(tools, HasLen, 0)
}

func (s *toolsSuite) TestListToolsWithTools(c *C) {
	tool1 := &mockTool{
		descriptor: mcpstate.ToolDescriptor{
			Name:        "tool1",
			Title:       "Tool 1",
			Description: "First tool",
		},
	}
	tool2 := &mockTool{
		descriptor: mcpstate.ToolDescriptor{
			Name:        "tool2",
			Title:       "Tool 2",
			Description: "Second tool",
		},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool1, tool2}, nil)
	tools := p.ListTools()
	c.Assert(tools, HasLen, 2)
	c.Check(tools[0].Name, Equals, "tool1")
	c.Check(tools[1].Name, Equals, "tool2")
}

func (s *toolsSuite) TestCallToolValidationFailure(c *C) {
	tool := &mockTool{
		descriptor:  mcpstate.ToolDescriptor{Name: "my_tool"},
		validateErr: fmt.Errorf("missing required field"),
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"my_tool","arguments":{"key":"value"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(rpcErr.Message, Matches, "invalid arguments.*")
}

func (s *toolsSuite) TestCallToolExecutionSuccess(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
		callResult: map[string]any{"status": "success"},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"my_tool","arguments":{"key":"value"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	resultMap := result.(map[string]any)
	c.Check(resultMap["isError"], Equals, false)
	c.Check(resultMap["content"], NotNil)
	structured := resultMap["structuredContent"].(map[string]any)
	c.Check(structured["status"], Equals, "success")
}

func (s *toolsSuite) TestCallToolExecutionError(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
		callErr:    fmt.Errorf("execution failed"),
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"my_tool","arguments":{"key":"value"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	resultMap := result.(map[string]any)
	c.Check(resultMap["isError"], Equals, true)
	content := resultMap["content"].([]map[string]any)
	c.Check(len(content) > 0, Equals, true)
}

func (s *toolsSuite) TestCallToolMissingName(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"arguments":{"key":"value"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
}

func (s *toolsSuite) TestCallToolMissingArguments(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"my_tool"}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)
	c.Check(tool.validateArgs, DeepEquals, map[string]any(nil))
	c.Check(tool.callArgs, DeepEquals, map[string]any(nil))
}

func (s *toolsSuite) TestCallToolPassesArgumentsToValidateAndCall(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
		callResult: map[string]any{"ok": true},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"my_tool","arguments":{"count":3,"name":"core"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	expectedArgs := map[string]any{"count": float64(3), "name": "core"}
	c.Check(tool.validateArgs, DeepEquals, expectedArgs)
	c.Check(tool.callArgs, DeepEquals, expectedArgs)
}

func (s *toolsSuite) TestCallToolPassesContextToCall(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
		callResult: map[string]any{"ok": true},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	params := json.RawMessage(`{"name":"my_tool","arguments":{}}`)
	result, rpcErr := p.CallTool(ctx, params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)
	c.Assert(tool.callCtx, NotNil)
	c.Check(tool.callCtx.Err(), Equals, context.Canceled)
}

func (s *toolsSuite) TestCallToolResultMarshalFallback(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
		callResult: map[string]any{"bad": make(chan int)},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"my_tool","arguments":{}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	resultMap := result.(map[string]any)
	content := resultMap["content"].([]map[string]any)
	text := content[0]["text"].(string)
	c.Check(strings.Contains(text, "unsupported type"), Equals, true)
	structured := resultMap["structuredContent"].(map[string]any)
	c.Check(strings.Contains(structured["marshal_error"].(string), "unsupported type"), Equals, true)
}

func (s *toolsSuite) TestCallToolSuccessWrapsNonObjectStructuredContent(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
		callResult: []string{"a", "b"},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"my_tool","arguments":{}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	resultMap := result.(map[string]any)
	structured := resultMap["structuredContent"].(map[string]any)
	wrapped := structured["result"].([]any)
	c.Check(wrapped, DeepEquals, []any{"a", "b"})
}

func (s *toolsSuite) TestCallToolNameIsCaseSensitive(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "my_tool"},
	}

	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"MY_TOOL","arguments":{}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(rpcErr.Message, Equals, "unknown tool")
}

func (s *toolsSuite) TestCallToolTypedToolDecodesTypedArgs(c *C) {
	tool := &typedTool{}
	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"typed_tool","arguments":{"value":"hello"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	resultMap := result.(map[string]any)
	structured := resultMap["structuredContent"].(map[string]any)
	c.Check(structured["hello"], Equals, "world")
}

func (s *toolsSuite) TestCallToolTypedToolRejectsUnknownField(c *C) {
	tool := &typedTool{}
	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"typed_tool","arguments":{"unknown":"x"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(strings.Contains(rpcErr.Message, "invalid arguments"), Equals, true)
}

func (s *toolsSuite) TestCallToolTypedToolValidateArgsFail(c *C) {
	tool := &typedTool{validateFail: true}
	p := mcpstate.Manager(state.New(nil), []mcpstate.Tool{tool}, nil)

	params := json.RawMessage(`{"name":"typed_tool","arguments":{"value":"hello"}}`)
	result, rpcErr := p.CallTool(context.Background(), params)
	c.Assert(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(strings.Contains(rpcErr.Message, "invalid arguments"), Equals, true)
}

type typedTool struct {
	validateFail bool
}

func (t *typedTool) Descriptor() mcpstate.ToolDescriptor {
	return mcpstate.ToolDescriptor{Name: "typed_tool"}
}

func (t *typedTool) ArgsType() any {
	return &struct {
		Value string `json:"value"`
	}{}
}

func (t *typedTool) ValidateArgs(args any) error {
	if t.validateFail {
		return fmt.Errorf("bad value")
	}
	return nil
}

func (t *typedTool) ResultType() any {
	return &struct {
		Hello string `json:"hello"`
	}{}
}

func (t *typedTool) CallWithArgs(ctx context.Context, st *state.State, args any) (any, error) {
	return map[string]any{"hello": "world"}, nil
}

func (t *typedTool) Validate(args map[string]any) error {
	return nil
}

func (t *typedTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	return nil, nil
}

type mockTool struct {
	descriptor   mcpstate.ToolDescriptor
	validateErr  error
	callResult   any
	callErr      error
	validateArgs map[string]any
	callArgs     map[string]any
	callCtx      context.Context
}

func (t *mockTool) Descriptor() mcpstate.ToolDescriptor {
	return t.descriptor
}

func (t *mockTool) Validate(args map[string]any) error {
	t.validateArgs = args
	return t.validateErr
}

func (t *mockTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	t.callCtx = ctx
	t.callArgs = args
	if t.callErr != nil {
		return nil, t.callErr
	}
	return t.callResult, nil
}
