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
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/mcpstate"
	"github.com/snapcore/snapd/overlord/state"
	. "gopkg.in/check.v1"
)

type registrySuite struct{}

var _ = Suite(&registrySuite{})

func (s *registrySuite) SetUpTest(c *C) {
	mcp.ResetRegistryForTesting()
}

func (s *registrySuite) TestRegisterToolSuccess(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{
			Name:        "registered_tool",
			Title:       "Registered Tool",
			Description: "A registered tool",
		},
	}

	mcp.RegisterTool(tool)

	// Verify it's registered
	tools := mcp.AllTools()
	found := false
	for _, t := range tools {
		if t.Descriptor().Name == "registered_tool" {
			found = true
			break
		}
	}
	c.Check(found, Equals, true)
}

func (s *registrySuite) TestRegisterToolDuplicatePanics(c *C) {
	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "dup_tool"},
	}

	mcp.RegisterTool(tool)

	c.Assert(func() {
		mcp.RegisterTool(tool)
	}, PanicMatches, "duplicate registered tool name.*")
}

func (s *registrySuite) TestRegisterResourceSuccess(c *C) {
	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:         "snap://custom/{id}",
			Name:        "custom",
			Description: "Custom resource",
		},
		pattern: "/custom/",
	}

	mcp.RegisterResource(resource)

	// Verify it's registered
	resources := mcp.AllResources()
	found := false
	for _, r := range resources {
		if r.Descriptor().Name == "custom" {
			found = true
			break
		}
	}
	c.Check(found, Equals, true)
}

func (s *registrySuite) TestRegisterResourceDuplicatePanics(c *C) {
	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:  "snap://dup/{id}",
			Name: "dup",
		},
		pattern: "/dup/",
	}

	mcp.RegisterResource(resource)

	c.Assert(func() {
		mcp.RegisterResource(resource)
	}, PanicMatches, "duplicate registered resource pattern.*")
}

func (s *registrySuite) TestAllToolsIntegration(c *C) {
	tool1 := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "tool1"},
	}
	tool2 := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "tool2"},
	}

	mcp.RegisterTool(tool1)
	mcp.RegisterTool(tool2)

	allTools := mcp.AllTools()
	c.Assert(len(allTools) >= 2, Equals, true)

	names := make(map[string]bool)
	for _, t := range allTools {
		names[t.Descriptor().Name] = true
	}
	c.Check(names["tool1"], Equals, true)
	c.Check(names["tool2"], Equals, true)
}

func (s *registrySuite) TestAllResourcesIntegration(c *C) {
	r1 := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:  "snap://res1/{id}",
			Name: "res1",
		},
		pattern: "/res1/",
	}
	r2 := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:  "snap://res2/{id}",
			Name: "res2",
		},
		pattern: "/res2/",
	}

	mcp.RegisterResource(r1)
	mcp.RegisterResource(r2)

	allResources := mcp.AllResources()
	c.Assert(len(allResources) >= 2, Equals, true)

	names := make(map[string]bool)
	for _, r := range allResources {
		names[r.Descriptor().Name] = true
	}
	c.Check(names["res1"], Equals, true)
	c.Check(names["res2"], Equals, true)
}

func (s *registrySuite) TestManagerWithRegisteredTools(c *C) {
	mcp.ResetRegistryForTesting()

	tool := &mockTool{
		descriptor: mcpstate.ToolDescriptor{Name: "mgr_tool"},
	}
	mcp.RegisterTool(tool)

	// Create manager with nil tools - should pick up registered ones
	p := mcpstate.Manager(state.New(nil), nil, nil)
	tools := p.ListTools()

	found := false
	for _, t := range tools {
		if t.Name == "mgr_tool" {
			found = true
			break
		}
	}
	c.Check(found, Equals, true)
}

func (s *registrySuite) TestManagerWithRegisteredResources(c *C) {
	mcp.ResetRegistryForTesting()

	resource := &mockResource{
		descriptor: mcpstate.ResourceDescriptor{
			URI:  "snap://mgr_res/{id}",
			Name: "mgr_res",
		},
		pattern: "/mgr_res/",
	}
	mcp.RegisterResource(resource)

	// Create manager with nil resources - should pick up registered ones
	p := mcpstate.Manager(state.New(nil), nil, nil)
	result := p.ListResources()

	resources := result["resources"].([]mcpstate.ResourceDescriptor)
	found := false
	for _, r := range resources {
		if r.Name == "mgr_res" {
			found = true
			break
		}
	}
	c.Check(found, Equals, true)
}
