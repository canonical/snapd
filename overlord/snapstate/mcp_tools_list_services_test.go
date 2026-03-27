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
	"context"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestCallListServicesNoServices(c *C) {
	result, callErr := (snapstate.ListServicesTool{}).Call(context.Background(), state.New(nil), map[string]any{})
	c.Assert(callErr, IsNil)
	services := resultToMap(c, result)["services"].([]any)
	c.Check(services, HasLen, 0)
}

func (s *snapMCPSuite) TestListServicesValidateInvalidType(c *C) {
	err := (snapstate.ListServicesTool{}).Validate(map[string]any{"snap_name": true})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `invalid arguments: .*snap_name.*`)
}
