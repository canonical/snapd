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

package mcp_test

import (
	"context"
	"net/http"
	"os"

	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	. "gopkg.in/check.v1"
)

type registrySuite struct{}

var _ = Suite(&registrySuite{})

type mockTool struct {
	name string
}

func (t *mockTool) Descriptor() mcp.ToolDescriptor {
	return mcp.ToolDescriptor{Name: t.name, Title: t.name, Description: t.name}
}

func (t *mockTool) Validate(args map[string]any) error {
	return nil
}

func (t *mockTool) Call(ctx context.Context, st *state.State, args map[string]any) (any, error) {
	return map[string]any{"ok": true}, nil
}

type mockResource struct {
	name    string
	pattern string
}

func (r *mockResource) Descriptor() mcp.ResourceDescriptor {
	return mcp.ResourceDescriptor{URI: "snap://" + r.name + "/{id}", Name: r.name, Description: r.name}
}

func (r *mockResource) Pattern() string {
	return r.pattern
}

func (r *mockResource) Read(ctx context.Context, st *state.State, req *http.Request) (any, error) {
	return map[string]any{"name": r.name}, nil
}

func (s *registrySuite) SetUpTest(c *C) {
	mcp.ResetRegistryForTesting()
}

func (s *registrySuite) TestRegisterToolAndAllToolsCopy(c *C) {
	mcp.RegisterTool(&mockTool{name: "tool-a"})
	mcp.RegisterTool(&mockTool{name: "tool-b"})

	allTools := mcp.AllTools()
	c.Assert(allTools, HasLen, 2)
	c.Check(allTools[0].Descriptor().Name, Equals, "tool-a")
	c.Check(allTools[1].Descriptor().Name, Equals, "tool-b")

	allTools[0] = nil
	c.Check(mcp.AllTools()[0].Descriptor().Name, Equals, "tool-a")
}

func (s *registrySuite) TestRegisterToolDuplicatePanics(c *C) {
	mcp.RegisterTool(&mockTool{name: "dup-tool"})

	c.Assert(func() {
		mcp.RegisterTool(&mockTool{name: "dup-tool"})
	}, PanicMatches, "duplicate registered tool name: dup-tool")
}

func (s *registrySuite) TestRegisterResourceAndAllResourcesCopy(c *C) {
	mcp.RegisterResource(&mockResource{name: "resource-a", pattern: "/resource-a/"})
	mcp.RegisterResource(&mockResource{name: "resource-b", pattern: "/resource-b/"})

	allResources := mcp.AllResources()
	c.Assert(allResources, HasLen, 2)
	c.Check(allResources[0].Descriptor().Name, Equals, "resource-a")
	c.Check(allResources[1].Descriptor().Name, Equals, "resource-b")

	allResources[0] = nil
	c.Check(mcp.AllResources()[0].Descriptor().Name, Equals, "resource-a")
}

func (s *registrySuite) TestRegisterResourceDuplicatePanics(c *C) {
	mcp.RegisterResource(&mockResource{name: "dup-resource", pattern: "/dup-resource/"})

	c.Assert(func() {
		mcp.RegisterResource(&mockResource{name: "other", pattern: "/dup-resource/"})
	}, PanicMatches, "duplicate registered resource pattern: /dup-resource/")
}

func (s *registrySuite) TestResetRegistryForTestingClearsAll(c *C) {
	mcp.RegisterTool(&mockTool{name: "tool-a"})
	mcp.RegisterResource(&mockResource{name: "resource-a", pattern: "/resource-a/"})

	mcp.ResetRegistryForTesting()

	c.Check(mcp.AllTools(), HasLen, 0)
	c.Check(mcp.AllResources(), HasLen, 0)

	mcp.RegisterTool(&mockTool{name: "tool-a"})
	mcp.RegisterResource(&mockResource{name: "resource-a", pattern: "/resource-a/"})
	c.Check(mcp.AllTools(), HasLen, 1)
	c.Check(mcp.AllResources(), HasLen, 1)
}

func (s *registrySuite) TestResetRegistryForTestingPanicsOutsideTestBinary(c *C) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{"/tmp/not-a-test-binary"}

	c.Assert(func() {
		mcp.ResetRegistryForTesting()
	}, PanicMatches, "internal error: ResetRegistryForTesting can only be used in tests")
}
