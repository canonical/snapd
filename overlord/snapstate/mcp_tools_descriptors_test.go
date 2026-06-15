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

package snapstate_test

import (
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/snapstate"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestDescriptorsIncludeOutputSchemaAndExecutionMetadata(c *C) {
	tools := []interface {
		Descriptor() mcp.ToolDescriptor
	}{
		snapstate.ListSnapsTool{},
		snapstate.GetSnapTool{},
		snapstate.SearchStoreSnapsTool{},
		snapstate.GetStoreSnapTool{},
		snapstate.ListChangesTool{},
		snapstate.ListChangeTasksTool{},
		snapstate.ListServicesTool{},
		snapstate.GetServiceLogsTool{},
	}

	for _, tool := range tools {
		d := tool.Descriptor()
		c.Check(d.Annotations.ReadOnlyHint, Equals, true, Commentf("tool=%s", d.Name))
		c.Check(d.Execution.TaskSupport, Equals, mcp.ToolTaskSupportForbidden, Commentf("tool=%s", d.Name))
		c.Assert(d.OutputSchema, NotNil, Commentf("tool=%s", d.Name))
		c.Check(d.OutputSchema["type"], Equals, "object", Commentf("tool=%s", d.Name))
	}
}
