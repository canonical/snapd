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

package mcp

import "github.com/snapcore/snapd/osutil"

var (
	registeredTools            []Tool
	registeredToolNames        = make(map[string]struct{})
	registeredResources        []Resource
	registeredResourcePatterns = make(map[string]struct{})
)

// RegisterTool registers an MCP tool contribution.
// It panics if the tool name was already registered.
func RegisterTool(tool Tool) {
	name := tool.Descriptor().Name
	if _, exists := registeredToolNames[name]; exists {
		panic("duplicate registered tool name: " + name)
	}
	registeredToolNames[name] = struct{}{}
	registeredTools = append(registeredTools, tool)
}

// RegisterResource registers an MCP resource contribution.
// It panics if the resource pattern was already registered.
func RegisterResource(resource Resource) {
	pattern := resource.Pattern()
	if _, exists := registeredResourcePatterns[pattern]; exists {
		panic("duplicate registered resource pattern: " + pattern)
	}
	registeredResourcePatterns[pattern] = struct{}{}
	registeredResources = append(registeredResources, resource)
}

// AllTools returns a copy of all registered tools.
func AllTools() []Tool {
	return append([]Tool{}, registeredTools...)
}

// AllResources returns a copy of all registered resources.
func AllResources() []Resource {
	return append([]Resource{}, registeredResources...)
}

// ResetRegistryForTesting resets the global MCP registry.
func ResetRegistryForTesting() {
	if !osutil.IsTestBinary() {
		panic("internal error: ResetRegistryForTesting can only be used in tests")
	}

	registeredTools = nil
	registeredToolNames = make(map[string]struct{})
	registeredResources = nil
	registeredResourcePatterns = make(map[string]struct{})
}
