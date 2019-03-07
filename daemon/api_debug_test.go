// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package daemon

import (
	"bytes"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&postDebugSuite{})

type postDebugSuite struct {
	apiBaseSuite
}

func (s *postDebugSuite) TestPostDebugEnsureStateSoon(c *check.C) {
	s.daemonWithOverlordMock(c)

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}

	buf := bytes.NewBufferString(`{"action": "ensure-state-soon"}`)
	req, err := http.NewRequest("POST", "/v2/debug", buf)
	c.Assert(err, check.IsNil)

	rsp := postDebug(debugCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.Equals, true)
	c.Check(soon, check.Equals, 1)
}

func (s *postDebugSuite) TestPostDebugGetBaseDeclaration(c *check.C) {
	_ = s.daemon(c)

	buf := bytes.NewBufferString(`{"action": "get-base-declaration"}`)
	req, err := http.NewRequest("POST", "/v2/debug", buf)
	c.Assert(err, check.IsNil)

	rsp := postDebug(debugCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result.(map[string]interface{})["base-declaration"],
		testutil.Contains, "type: base-declaration")
}

func (s *postDebugSuite) testDebugConnectivityHappy(c *check.C, post bool) {
	_ = s.daemon(c)

	s.connectivityResult = map[string]bool{
		"good.host.com":         true,
		"another.good.host.com": true,
	}

	var rsp *resp
	if post {
		buf := bytes.NewBufferString(`{"action": "connectivity"}`)
		req, err := http.NewRequest("POST", "/v2/debug", buf)
		c.Assert(err, check.IsNil)

		rsp = postDebug(debugCmd, req, nil).(*resp)
	} else {
		req, err := http.NewRequest("POST", "/v2/debug?aspect=connectivity", nil)
		c.Assert(err, check.IsNil)
		rsp = getDebug(debugCmd, req, nil).(*resp)

	}

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, ConnectivityStatus{
		Connectivity: true,
		Unreachable:  []string(nil),
	})
}

func (s *postDebugSuite) TestPostDebugConnectivityHappy(c *check.C) {
	s.testDebugConnectivityHappy(c, true)
}

func (s *postDebugSuite) TestGetDebugConnectivityHappy(c *check.C) {
	s.testDebugConnectivityHappy(c, false)
}

func (s *postDebugSuite) testDebugConnectivityUnhappy(c *check.C, post bool) {
	_ = s.daemon(c)

	s.connectivityResult = map[string]bool{
		"good.host.com": true,
		"bad.host.com":  false,
	}

	var rsp *resp
	if post {
		buf := bytes.NewBufferString(`{"action": "connectivity"}`)
		req, err := http.NewRequest("POST", "/v2/debug", buf)
		c.Assert(err, check.IsNil)

		rsp = postDebug(debugCmd, req, nil).(*resp)
	} else {
		req, err := http.NewRequest("GET", "/v2/debug?aspect=connectivity", nil)
		c.Assert(err, check.IsNil)
		rsp = getDebug(debugCmd, req, nil).(*resp)
	}

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, ConnectivityStatus{
		Connectivity: false,
		Unreachable:  []string{"bad.host.com"},
	})
}

func (s *postDebugSuite) TestPostDebugConnectivityUnhappy(c *check.C) {
	s.testDebugConnectivityUnhappy(c, true)
}

func (s *postDebugSuite) TestGetDebugConnectivityUnhappy(c *check.C) {
	s.testDebugConnectivityUnhappy(c, false)
}

func (s *postDebugSuite) TestGetDebugBaseDeclaration(c *check.C) {
	_ = s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/debug?aspect=base-declaration", nil)
	c.Assert(err, check.IsNil)

	rsp := getDebug(debugCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result.(map[string]interface{})["base-declaration"],
		testutil.Contains, "type: base-declaration")
}
