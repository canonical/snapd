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
	"net"
	"net/http"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/usersession/agent"
)

func Test(t *testing.T) { TestingT(t) }

type restSuite struct {
	socketPath string
	client     *http.Client
}

var _ = Suite(&restSuite{})

func (s *restSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)
	s.socketPath = fmt.Sprintf("%s/snapd.session-agent.socket", xdgRuntimeDir)

	transport := &http.Transport{
		Dial: func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", s.socketPath)
		},
		DisableKeepAlives: true,
	}
	s.client = &http.Client{Transport: transport}
}

func (s *restSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *restSuite) TestAgentInfo(c *C) {
	agent, err := agent.NewSessionAgent()
	c.Assert(err, IsNil)
	agent.Version = "42"
	agent.Start()
	defer func() { c.Check(agent.Stop(), IsNil) }()

	response, err := s.client.Get("http://localhost/v1/session-info")
	c.Assert(err, IsNil)
	c.Check(response.StatusCode, Equals, 200)

	var rst struct {
		Result struct {
			Version string `json:"version"`
		} `json:"result"`
	}
	c.Assert(json.NewDecoder(response.Body).Decode(&rst), IsNil)
	c.Check(rst.Result.Version, Equals, "42")
}
