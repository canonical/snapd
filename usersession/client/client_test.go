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
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/client"
)

var (
	timeout = testutil.HostScaledTimeout(80 * time.Millisecond)
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

func (s *clientSuite) TestAgentTimeout(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay one of the agents from responding, but don't
		// stick around if the client disconnects.
		if r.Host == "1000" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Second):
				c.Fatal("Request context was not cancelled")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
  "type": "sync",
  "result": {
    "version": "42"
  }
}`))
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	si, err := s.cli.SessionInfo(ctx)

	// An error is reported, and we receive information about the
	// agent that replied on time.
	c.Assert(err, ErrorMatches, `Get \"?http://1000/v1/session-info\"?: context deadline exceeded`)
	c.Check(si, DeepEquals, map[int]client.SessionInfo{
		42: {Version: "42"},
	})
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

func (s *clientSuite) TestPendingRefreshNotification(c *C) {
	var n int32
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		c.Assert(r.URL.Path, Equals, "/v1/notifications/pending-refresh")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"type": "sync"}`))
	})
	err := s.cli.PendingRefreshNotification(context.Background(), &client.PendingSnapRefreshInfo{})
	c.Assert(err, IsNil)
	c.Check(atomic.LoadInt32(&n), Equals, int32(2))
}

func (s *clientSuite) TestFinishRefreshNotification(c *C) {
	var n int32
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		c.Assert(r.URL.Path, Equals, "/v1/notifications/finish-refresh")
		body, err := io.ReadAll(r.Body)
		c.Check(err, IsNil)
		c.Check(string(body), DeepEquals, `{"instance-name":"some-snap"}`)
	})
	err := s.cli.FinishRefreshNotification(context.Background(), &client.FinishedSnapRefreshInfo{InstanceName: "some-snap"})
	c.Assert(err, IsNil)
	// two calls because clientSuite simulates two user sessions (two
	// snapd-session-agent.socket sockets).
	c.Check(atomic.LoadInt32(&n), Equals, int32(2))
}

func (s *clientSuite) TestPendingRefreshNotificationOneClient(c *C) {
	cli := client.NewForUids(1000)
	var n int32
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		c.Assert(r.URL.Path, Equals, "/v1/notifications/pending-refresh")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"type": "sync"}`))
	})
	err := cli.PendingRefreshNotification(context.Background(), &client.PendingSnapRefreshInfo{})
	c.Assert(err, IsNil)
	c.Check(atomic.LoadInt32(&n), Equals, int32(1))
}
