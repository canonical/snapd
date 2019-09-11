// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	s.handler.ServeHTTP(w, r)
}

func (s *clientSuite) TestSessionInfo(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
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
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "message": "something bad happened"
  }
}`))
	})
	si, err := s.cli.SessionInfo(context.Background())
	c.Check(si, DeepEquals, map[int]client.SessionInfo{})
	c.Check(err, DeepEquals, &client.Error{
		Kind:    "",
		Message: "something bad happened",
		Value:   nil,
	})
}

func (s *clientSuite) TestServicesDaemonReload(c *C) {
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
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
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{
  "type": "error",
  "result": {
    "message": "something bad happened"
  }
}`))
	})
	err := s.cli.ServicesDaemonReload(context.Background())
	c.Check(err, DeepEquals, &client.Error{
		Kind:    "",
		Message: "something bad happened",
		Value:   nil,
	})
}
