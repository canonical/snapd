// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package daemon_test

import (
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate"
)

var _ = Suite(&featuresDebugSuite{})

type featuresDebugSuite struct {
	apiBaseSuite
}

func (s *featuresDebugSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)
	s.daemonWithOverlordMock()
	ifacemgr, err := ifacestate.Manager(s.d.Overlord().State(), s.d.Overlord().HookManager(), s.d.Overlord().TaskRunner(), []interfaces.Interface{}, []interfaces.SecurityBackend{})
	c.Assert(err, IsNil)
	s.d.Overlord().AddManager(ifacemgr)
}

func (s *featuresDebugSuite) getFeaturesDebug(c *C) any {
	req, err := http.NewRequest("GET", "/v2/debug?aspect=features", nil)
	c.Assert(err, IsNil)

	rsp := s.syncReq(c, req, nil, actionIsExpected)
	c.Assert(rsp.Type, Equals, daemon.ResponseTypeSync)
	return rsp.Result
}

func (s *featuresDebugSuite) TestNoData(c *C) {
	data := s.getFeaturesDebug(c)
	c.Check(data, NotNil)
	resp, ok := data.(daemon.FeatureResponse)
	c.Assert(ok, Equals, true)
	c.Assert(len(resp.Tasks) > 0, Equals, true)
	c.Assert(len(resp.Changes) > 0, Equals, true)
	c.Assert(len(resp.Endpoints) > 0, Equals, true)
	c.Assert(len(resp.Ensures) > 0, Equals, true)
}
