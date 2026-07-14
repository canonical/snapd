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

package snapstate

import (
	"sync"

	"github.com/snapcore/snapd/overlord/mcp"
)

var (
	mcpRegistrationOnce sync.Once
)

// delayedCrossMgrInit registers snap-specific MCP tools and resources.
// This is called once from the snap manager's Manager() function to handle
// cross-manager initialization similar to how ifacestate does it.
func delayedCrossMgrInit() {
	mcpRegistrationOnce.Do(func() {
		mcp.RegisterTool(listSnapsTool{})
		mcp.RegisterTool(getSnapTool{})
		mcp.RegisterTool(searchStoreSnapsTool{})
		mcp.RegisterTool(getStoreSnapTool{})
		mcp.RegisterTool(listChangesTool{})
		mcp.RegisterTool(listChangeTasksTool{})
		mcp.RegisterTool(listServicesTool{})
		mcp.RegisterTool(getServiceLogsTool{})
		mcp.RegisterResource(snapInfoResource{})
	})
}
