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
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/agent"
)

type restSuite struct {
	testutil.BaseTest
	sysdLog [][]string
}

var _ = Suite(&restSuite{})

func (s *restSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)

	s.sysdLog = nil
	restore := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		s.sysdLog = append(s.sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	s.AddCleanup(restore)
	restore = systemd.MockStopDelays(time.Millisecond, 25*time.Second)
	s.AddCleanup(restore)
}

func (s *restSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.BaseTest.TearDownTest(c)
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

func (s *restSuite) TestServices(c *C) {
	// the agent.Services end point only supports POST requests
	c.Assert(agent.ServicesCmd.GET, IsNil)
	c.Check(agent.ServicesCmd.PUT, IsNil)
	c.Check(agent.ServicesCmd.POST, NotNil)
	c.Check(agent.ServicesCmd.DELETE, IsNil)

	c.Check(agent.ServicesCmd.Path, Equals, "/v1/services")
}

func (s *restSuite) TestServicesDaemonReload(c *C) {
	_, err := agent.New()
	c.Assert(err, IsNil)

	req, err := http.NewRequest("POST", "/v1/services", bytes.NewBufferString(`{"action":"daemon-reload"}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServicesCmd.POST(agent.ServicesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "daemon-reload"},
	})
}

func (s *restSuite) TestServicesStart(c *C) {
	_, err := agent.New()
	c.Assert(err, IsNil)

	req, err := http.NewRequest("POST", "/v1/services", bytes.NewBufferString(`{"action":"start","services":["snap.foo.service", "snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServicesCmd.POST(agent.ServicesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "start", "snap.foo.service"},
		{"--user", "start", "snap.bar.service"},
	})
}

func (s *restSuite) TestServicesStop(c *C) {
	_, err := agent.New()
	c.Assert(err, IsNil)

	req, err := http.NewRequest("POST", "/v1/services", bytes.NewBufferString(`{"action":"stop","services":["snap.foo.service", "snap.bar.service"]}`))
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)
	rec := httptest.NewRecorder()
	agent.ServicesCmd.POST(agent.ServicesCmd, req).ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), Equals, "application/json")

	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, agent.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)

	c.Check(s.sysdLog, DeepEquals, [][]string{
		{"--user", "stop", "snap.foo.service"},
		{"--user", "show", "--property=ActiveState", "snap.foo.service"},
		{"--user", "stop", "snap.bar.service"},
		{"--user", "show", "--property=ActiveState", "snap.bar.service"},
	})
}
