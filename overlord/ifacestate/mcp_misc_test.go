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

package ifacestate_test

import (
	"context"
	"testing"

	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
)

func TestInterfaceMCPToolDescriptorsIncludeSchemaAndExecutionMetadata(t *testing.T) {
	tools := []interface {
		Descriptor() mcp.ToolDescriptor
	}{
		ifacestate.ListConnectionsTool{},
		ifacestate.ListPlugsTool{},
		ifacestate.ListSlotsTool{},
		ifacestate.ListInterfaceTypesTool{},
	}

	for _, tool := range tools {
		d := tool.Descriptor()
		if !d.Annotations.ReadOnlyHint {
			t.Fatalf("tool %q must be marked read-only", d.Name)
		}
		if d.Execution.TaskSupport != mcp.ToolTaskSupportForbidden {
			t.Fatalf("tool %q must set execution.taskSupport to %q, got %q", d.Name, mcp.ToolTaskSupportForbidden, d.Execution.TaskSupport)
		}
		if d.OutputSchema == nil {
			t.Fatalf("tool %q must provide output schema", d.Name)
		}
		if d.OutputSchema["type"] != "object" {
			t.Fatalf("tool %q output schema must have object root type, got %#v", d.Name, d.OutputSchema["type"])
		}
	}
}

func TestInterfaceRepositoryToolsDoNotRequireCallerStateLock(t *testing.T) {
	st := state.New(nil)
	_ = setupInterfaceRepo(t, st)

	tools := []struct {
		name string
		call func() (any, error)
	}{
		{
			name: "plugs",
			call: func() (any, error) {
				return (ifacestate.ListPlugsTool{}).Call(context.Background(), st, map[string]any{"snap_name": "consumer"})
			},
		},
		{
			name: "slots",
			call: func() (any, error) {
				return (ifacestate.ListSlotsTool{}).Call(context.Background(), st, map[string]any{"snap_name": "provider"})
			},
		},
		{
			name: "interface types",
			call: func() (any, error) {
				return (ifacestate.ListInterfaceTypesTool{}).Call(context.Background(), st, map[string]any{"name": "net"})
			},
		},
	}

	for _, tc := range tools {
		result, err := tc.call()
		if err != nil {
			t.Fatalf("tool %s unexpectedly failed without caller lock: %v", tc.name, err)
		}
		if result == nil {
			t.Fatalf("tool %s returned nil result", tc.name)
		}
	}
}
