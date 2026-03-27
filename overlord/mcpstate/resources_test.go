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
	"net/http"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/mcpstate"
	"github.com/snapcore/snapd/overlord/state"
	. "gopkg.in/check.v1"
)

type resourcesSuite struct{}

var _ = Suite(&resourcesSuite{})

func (s *resourcesSuite) SetUpTest(c *C) {
	mcp.ResetRegistryForTesting()
}

func (s *resourcesSuite) TestListResourcesEmpty(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	result := p.ListResources()
	c.Assert(result, NotNil)
	resources := result["resources"].([]mcpstate.ResourceDescriptor)
	c.Check(len(resources), Equals, 0)
}

func (s *resourcesSuite) TestListResourcesWithResources(c *C) {
	r1 := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://info/{id}",
			Name:        "info",
			Description: "Snap info",
		},
		pattern: "/info/",
	}
	r2 := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://services/{id}",
			Name:        "services",
			Description: "Snap services",
		},
		pattern: "/services/",
	}

	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{r1, r2})
	result := p.ListResources()
	c.Assert(result, NotNil)
	resources := result["resources"].([]mcpstate.ResourceDescriptor)
	c.Check(len(resources), Equals, 2)
	c.Check(resources[0].Name, Equals, "info")
	c.Check(resources[1].Name, Equals, "services")
}

func (s *resourcesSuite) TestReadResourceInvalidJSON(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"uri":`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
}

func (s *resourcesSuite) TestReadResourceEmptyURI(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"uri":""}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(rpcErr.Message, Matches, "invalid arguments.*")
}

func (s *resourcesSuite) TestReadResourceInvalidScheme(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"uri":"file://info/core"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
}

func (s *resourcesSuite) TestReadResourceMissingEndpoint(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"uri":"snap:///core"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
}

func (s *resourcesSuite) TestReadResourceMissingID(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"uri":"snap://info/"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
}

func (s *resourcesSuite) TestReadResourceUnsupportedPattern(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{"uri":"snap://unknown/core"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
	c.Check(rpcErr.Message, Matches, "invalid arguments.*")
}

func (s *resourcesSuite) TestReadResourceSuccess(c *C) {
	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://info/{id}",
			Name:        "info",
			Description: "Snap info",
		},
		pattern:    "/info/",
		readResult: map[string]any{"name": "core", "status": "active"},
	}

	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{resource})

	params := json.RawMessage(`{"uri":"snap://info/core"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	resultMap := result.(map[string]any)
	c.Check(resultMap["name"], Equals, "core")
	c.Check(resultMap["status"], Equals, "active")
}

func (s *resourcesSuite) TestReadResourceError(c *C) {
	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://info/{id}",
			Name:        "info",
			Description: "Snap info",
		},
		pattern: "/info/",
		readErr: fmt.Errorf("not found"),
	}

	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{resource})

	params := json.RawMessage(`{"uri":"snap://info/core"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInternalError)
	c.Check(rpcErr.Message, Matches, "not found")
}

func (s *resourcesSuite) TestReadResourceEncodedURI(c *C) {
	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://info/{id}",
			Name:        "info",
			Description: "Snap info",
		},
		pattern:    "/info/",
		readResult: map[string]any{"name": "encoded-name"},
	}

	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{resource})

	params := json.RawMessage(`{"uri":"snap://info/encoded%2Dname"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)
}

func (s *resourcesSuite) TestReadResourcePassesContextToResource(c *C) {
	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://info/{id}",
			Name:        "info",
			Description: "Snap info",
		},
		pattern:    "/info/",
		readResult: map[string]any{"name": "core"},
	}

	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{resource})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	params := json.RawMessage(`{"uri":"snap://info/core"}`)
	result, rpcErr := p.ReadResource(ctx, params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)
	c.Assert(resource.readCtx, NotNil)
	c.Check(resource.readCtx.Err(), Equals, context.Canceled)
}

func (s *resourcesSuite) TestReadResourceQueryAndFragmentAreIgnored(c *C) {
	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://info/{id}",
			Name:        "info",
			Description: "Snap info",
		},
		pattern:    "/info/",
		readResult: map[string]any{"name": "core"},
	}

	p := mcpstate.Manager(state.New(nil), nil, []mcpstate.Resource{resource})

	params := json.RawMessage(`{"uri":"snap://info/core?channel=stable#details"}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Assert(rpcErr, IsNil)
	c.Assert(result, NotNil)

	c.Assert(resource.readReq, NotNil)
	c.Check(resource.readReq.URL.Path, Equals, "/info/core")
	c.Check(resource.readReq.URL.RawQuery, Equals, "")
	c.Check(resource.readReq.URL.Fragment, Equals, "")
}

func (s *resourcesSuite) TestReadResourceMissingURI(c *C) {
	p := mcpstate.Manager(state.New(nil), nil, nil)

	params := json.RawMessage(`{}`)
	result, rpcErr := p.ReadResource(context.Background(), params)
	c.Check(result, IsNil)
	c.Assert(rpcErr, NotNil)
	c.Check(rpcErr.Code, Equals, mcpstate.RPCInvalidParams)
}

type mockResource struct {
	descriptor mcpstate.ResourceDescriptor
	pattern    string
	readResult any
	readErr    error
	readReq    *http.Request
	readCtx    context.Context
}

func (r *mockResource) Descriptor() mcpstate.ResourceDescriptor {
	return r.descriptor
}

func (r *mockResource) Pattern() string {
	return r.pattern
}

func (r *mockResource) Read(ctx context.Context, st *state.State, req *http.Request) (any, error) {
	r.readCtx = ctx
	r.readReq = req
	if r.readErr != nil {
		return nil, r.readErr
	}
	return r.readResult, nil
}
