// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

package client_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/client"
)

func Test(t *testing.T) { TestingT(t) }

type clientSuite struct {
	testutil.BaseTest

	cli *client.Client

	server  *http.Server
	handler http.Handler
}

var _ = Suite(&clientSuite{})

func (s *clientSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())

	s.handler = nil

	s.server = &http.Server{Handler: s}
	for _, uid := range []int{1000, 42} {
		sock := fmt.Sprintf("%s/%d/snapd-session-agent.socket", dirs.XdgRuntimeDirBase, uid)
		err := os.MkdirAll(filepath.Dir(sock), 0755)
		c.Assert(err, IsNil)
		l, err := net.Listen("unix", sock)
		c.Assert(err, IsNil)
		go func(l net.Listener) {
			err := s.server.Serve(l)
			c.Check(err, Equals, http.ErrServerClosed)
		}(l)
	}

	s.cli = client.New()
}

func (s *clientSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")

	err := s.server.Shutdown(context.Background())
	c.Check(err, IsNil)
}

func (s *clientSuite) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.handler == nil {
		w.WriteHeader(500)
		return
	}
	s.handler.ServeHTTP(w, r)
}

func (s *clientSuite) TestBadJsonResponse(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"type":`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Check(si, DeepEquals, map[int]client.SessionInfo{})
	c.Check(err, ErrorMatches, `cannot decode "{\\"type\\":": unexpected EOF`)
}

func (s *clientSuite) TestSessionInfo(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": {
    "version": "42"
  }
}`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Assert(err, IsNil)
	c.Check(si, DeepEquals, map[int]client.SessionInfo{
		42:   {Version: "42"},
		1000: {Version: "42"},
	})
}

func (s *clientSuite) TestSessionInfoError(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "message": "something bad happened"
  }
}`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Check(si, DeepEquals, map[int]client.SessionInfo{})
	c.Check(err, ErrorMatches, "something bad happened")
	c.Check(err, DeepEquals, &client.Error{
		Kind:    "",
		Message: "something bad happened",
		Value:   nil,
	})
}

func (s *clientSuite) TestSessionInfoWrongResultType(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": ["a", "list"]
}`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Check(si, DeepEquals, map[int]client.SessionInfo{})
	c.Check(err, ErrorMatches, `json: cannot unmarshal array into Go value of type client.SessionInfo`)
}

func (s *clientSuite) TestServicesDaemonReload(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	err := s.cli.ServicesDaemonReload(context.Background())
	c.Assert(err, IsNil)
}

func (s *clientSuite) TestServicesDaemonReloadError(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "message": "something bad happened"
  }
}`))
	})
	err := s.cli.ServicesDaemonReload(context.Background())
	c.Check(err, ErrorMatches, "something bad happened")
	c.Check(err, DeepEquals, &client.Error{
		Kind:    "",
		Message: "something bad happened",
		Value:   nil,
	})
}

func (s *clientSuite) TestServicesStart(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	startFailures, stopFailures, err := s.cli.ServicesStart(context.Background(), []string{"service1.service", "service2.service"})
	c.Assert(err, IsNil)
	c.Check(startFailures, HasLen, 0)
	c.Check(stopFailures, HasLen, 0)
}

func (s *clientSuite) TestServicesStartFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to start services",
    "value": {
      "start-errors": {
        "service2.service": "failed to start"
      }
    }
  }
}`))
	})
	startFailures, stopFailures, err := s.cli.ServicesStart(context.Background(), []string{"service1.service", "service2.service"})
	c.Assert(err, IsNil)
	c.Check(startFailures, HasLen, 2)
	c.Check(stopFailures, HasLen, 0)

	failure0 := startFailures[0]
	failure1 := startFailures[1]
	if failure0.Uid == 1000 {
		failure0, failure1 = failure1, failure0
	}
	c.Check(failure0.Uid, Equals, 42)
	c.Check(failure0.Service, Equals, "service2.service")
	c.Check(failure0.Error, Equals, "failed to start")

	c.Check(failure1.Uid, Equals, 1000)
	c.Check(failure1.Service, Equals, "service2.service")
	c.Check(failure1.Error, Equals, "failed to start")
}

func (s *clientSuite) TestServicesStop(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": null
}`))
	})
	failures, err := s.cli.ServicesStop(context.Background(), []string{"service1.service", "service2.service"})
	c.Assert(err, IsNil)
	c.Check(failures, HasLen, 0)
}

func (s *clientSuite) TestServicesStopFailure(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "kind": "service-control",
    "message": "failed to stop services",
    "value": {
      "stop-errors": {
        "service2.service": "failed to stop"
      }
    }
  }
}`))
	})
	failures, err := s.cli.ServicesStop(context.Background(), []string{"service1.service", "service2.service"})
	c.Assert(err, IsNil)
	c.Check(failures, HasLen, 2)
	failure0 := failures[0]
	failure1 := failures[1]
	if failure0.Uid == 1000 {
		failure0, failure1 = failure1, failure0
	}
	c.Check(failure0.Uid, Equals, 42)
	c.Check(failure0.Service, Equals, "service2.service")
	c.Check(failure0.Error, Equals, "failed to stop")

	c.Check(failure1.Uid, Equals, 1000)
	c.Check(failure1.Service, Equals, "service2.service")
	c.Check(failure1.Error, Equals, "failed to stop")
}
