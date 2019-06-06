// -*- Mode: Go; indent-tabs-mode: t -*-
// +build linux

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

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type sessionAgentSuite struct {
	BaseSnapSuite

	restoreLogger func()
	socketPath    string
	client        *http.Client
}

var _ = Suite(&sessionAgentSuite{})

func (s *sessionAgentSuite) SetUpTest(c *C) {
	s.BaseSnapSuite.SetUpTest(c)

	_, s.restoreLogger = logger.MockLogger()

	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	c.Assert(os.MkdirAll(xdgRuntimeDir, 0700), IsNil)
	s.socketPath = fmt.Sprintf("%s/snap-session.socket", xdgRuntimeDir)
	transport := &http.Transport{
		Dial: func(_, _ string) (net.Conn, error) {
			return net.Dial("unix", s.socketPath)
		},
		DisableKeepAlives: true,
	}
	s.client = &http.Client{Transport: transport}
}

func (s *sessionAgentSuite) TearDownTest(c *C) {
	s.BaseSnapSuite.TearDownTest(c)

	s.restoreLogger()
}

func (s *sessionAgentSuite) TestSessionAgentBadCommandline(c *C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"session-agent", "extra-arg"})
	c.Assert(err, ErrorMatches, "too many arguments for command")
}

func (s *sessionAgentSuite) TestSessionAgentSocket(c *C) {
	go func() {
		myPid := os.Getpid()
		defer func() {
			me, err := os.FindProcess(myPid)
			c.Assert(err, IsNil)
			me.Signal(syscall.SIGUSR1)
		}()

		// Wait for socket file to be created
		for i := 0; i < 1000; i++ {
			if osutil.FileExists(s.socketPath) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Check that agent functions
		response, err := s.client.Get("http://localhost/v1/agent-info")
		c.Assert(err, IsNil)
		c.Check(response.StatusCode, Equals, 200)
	}()

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"session-agent"})
	c.Assert(err, IsNil)
	c.Check(rest, DeepEquals, []string{})
	c.Check(strings.ToLower(s.Stdout()), Equals, "exiting on user defined signal 1.\n")
}
