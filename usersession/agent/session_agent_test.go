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
	sys "syscall"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/usersession/agent"
)

func Test(t *testing.T) { TestingT(t) }

type sessionAgentSuite struct {
	socketPath string
	client     *http.Client
}

var _ = Suite(&sessionAgentSuite{})

func (s *sessionAgentSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)
	s.socketPath = fmt.Sprintf("%s/snapd-session-agent.socket", xdgRuntimeDir)

	transport := &http.Transport{
		Dial: func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", s.socketPath)
		},
		DisableKeepAlives: true,
	}
	s.client = &http.Client{Transport: transport}
}

func (s *sessionAgentSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *sessionAgentSuite) TestStartStop(c *C) {
	agent, err := agent.New()
	c.Assert(err, IsNil)
	agent.Version = "42"
	agent.Start()
	defer func() { c.Check(agent.Stop(), IsNil) }()

	response, err := s.client.Get("http://localhost/v1/session-info")
	c.Assert(err, IsNil)
	defer response.Body.Close()
	c.Check(response.StatusCode, Equals, 200)

	var rst struct {
		Result struct {
			Version string `json:"version"`
		} `json:"result"`
	}
	c.Assert(json.NewDecoder(response.Body).Decode(&rst), IsNil)
	c.Check(rst.Result.Version, Equals, "42")
	response.Body.Close()

	c.Check(agent.Stop(), IsNil)
}

func (s *sessionAgentSuite) TestDying(c *C) {
	agent, err := agent.New()
	c.Assert(err, IsNil)
	agent.Start()
	select {
	case <-agent.Dying():
		c.Error("agent.Dying() channel closed prematurely")
	default:
	}
	go func() {
		time.Sleep(5 * time.Millisecond)
		c.Check(agent.Stop(), IsNil)
	}()
	select {
	case <-agent.Dying():
	case <-time.After(2 * time.Second):
		c.Error("agent.Dying() channel was not closed when agent stopped")
	}
}

func (s *sessionAgentSuite) TestExitOnIdle(c *C) {
	agent, err := agent.New()
	c.Assert(err, IsNil)
	agent.IdleTimeout = 100 * time.Millisecond
	startTime := time.Now()
	agent.Start()
	defer agent.Stop()

	makeRequest := func() {
		response, err := s.client.Get("http://localhost/v1/session-info")
		c.Assert(err, IsNil)
		defer response.Body.Close()
		c.Check(response.StatusCode, Equals, 200)
	}
	makeRequest()
	time.Sleep(25 * time.Millisecond)
	makeRequest()

	select {
	case <-agent.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("agent did not exit after idle timeout expired")
	}
	elapsed := time.Now().Sub(startTime)
	if elapsed < 125*time.Millisecond {
		// The idle timeout should have been extended when we
		// issued a second request after 25ms.
		c.Errorf("Expected ellaped time to be greater than 125 ms, but got %v", elapsed)
	}
}

func (s *sessionAgentSuite) TestPeerCredentialsCheck(c *C) {
	sa, err := agent.New()
	c.Assert(err, IsNil)
	sa.Start()
	defer sa.Stop()

	// Connections from other users are dropped.
	uid := uint32(sys.Geteuid())
	restore := agent.MockUcred(&sys.Ucred{Uid: uid + 1}, nil)
	defer restore()
	_, err = s.client.Get("http://localhost/v1/session-info")
	// This could be an EOF error or a failed read, depending on timing
	c.Assert(err, ErrorMatches, "Get http://localhost/v1/session-info: .*")

	// However, connections from root are accepted.
	restore = agent.MockUcred(&sys.Ucred{Uid: 0}, nil)
	defer restore()
	response, err := s.client.Get("http://localhost/v1/session-info")
	c.Assert(err, IsNil)
	defer response.Body.Close()
	c.Check(response.StatusCode, Equals, 200)

	// Connections are dropped if peer credential lookup fails.
	restore = agent.MockUcred(nil, fmt.Errorf("SO_PEERCRED failed"))
	defer restore()
	_, err = s.client.Get("http://localhost/v1/session-info")
	c.Assert(err, ErrorMatches, "Get http://localhost/v1/session-info: .*")
}
