// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package agent_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/usersession/agent"
)

type restSuite struct{}

var _ = Suite(&restSuite{})

func (s *restSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)
}

func (s *restSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

type resp struct {
	Type   agent.ResponseType `json:"type"`
	Result interface{}        `json:"result"`
}

func (s *restSuite) TestSessionInfo(c *C) {
	// the agent.SessionInfo end point only supports GET requests
	c.Check(agent.SessionInfoCmd.PUT, IsNil)
	c.Check(agent.SessionInfoCmd.POST, IsNil)
	c.Check(agent.SessionInfoCmd.DELETE, IsNil)
	c.Assert(agent.SessionInfoCmd.GET, NotNil)

	c.Check(agent.SessionInfoCmd.Path, Equals, "/v1/session-info")

	a, err := agent.New()
	c.Assert(err, IsNil)
	a.Version = "42b1"
	rec := httptest.NewRecorder()
	agent.SessionInfoCmd.GET(agent.SessionInfoCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"version": "42b1",
	})
}
