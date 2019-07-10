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

package agent

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
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

func (s *restSuite) TestSessionInfo(c *C) {
	// the sessionInfo end point only supports GET requests
	c.Check(sessionInfoCmd.PUT, IsNil)
	c.Check(sessionInfoCmd.POST, IsNil)
	c.Check(sessionInfoCmd.DELETE, IsNil)
	c.Assert(sessionInfoCmd.GET, NotNil)

	c.Check(sessionInfoCmd.Path, Equals, "/v1/session-info")

	agent, err := New()
	c.Assert(err, IsNil)
	agent.Version = "42b1"
	rec := httptest.NewRecorder()
	sessionInfoCmd.GET(sessionInfoCmd, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{
		"version": "42b1",
	})
}
