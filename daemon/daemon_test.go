// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type daemonSuite struct{}

var _ = check.Suite(&daemonSuite{})

// build a new daemon, with only a little of Init(), suitable for the tests
func newTestDaemon(c *check.C) *Daemon {
	d, err := New()
	c.Assert(err, check.IsNil)
	d.addRoutes()

	return d
}

// aResponse suitable for testing
type mockHandler struct {
	cmd        *Command
	lastMethod string
}

func (mck *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mck.lastMethod = r.Method
}
func (mck *mockHandler) Self(*Command, *http.Request) Response {
	return mck
}

func mkRF(c *check.C, cmd *Command, mck *mockHandler) ResponseFunc {
	return func(innerCmd *Command, req *http.Request) Response {
		c.Assert(cmd, check.Equals, innerCmd)
		return mck
	}
}

func (s *daemonSuite) TestCommandMethodDispatch(c *check.C) {
	cmd := &Command{}
	mck := &mockHandler{cmd: cmd}
	rf := mkRF(c, cmd, mck)
	cmd.GET = rf
	cmd.PUT = rf
	cmd.POST = rf
	cmd.DELETE = rf

	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		req, err := http.NewRequest(method, "", nil)
		c.Assert(err, check.IsNil)

		rec := httptest.NewRecorder()
		cmd.ServeHTTP(rec, req)
		c.Check(rec.Code, check.Equals, http.StatusForbidden, check.Commentf(method))

		rec = httptest.NewRecorder()
		req.RemoteAddr = "uid=0;" + req.RemoteAddr

		cmd.ServeHTTP(rec, req)
		c.Check(mck.lastMethod, check.Equals, method)
		c.Check(rec.Code, check.Equals, http.StatusOK)
	}

	req, err := http.NewRequest("POTATO", "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = "uid=0;" + req.RemoteAddr

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, http.StatusMethodNotAllowed)
}

func (s *daemonSuite) TestGuestAccess(c *check.C) {
	get := &http.Request{Method: "GET"}
	put := &http.Request{Method: "PUT"}
	pst := &http.Request{Method: "POST"}
	del := &http.Request{Method: "DELETE"}

	cmd := &Command{}
	c.Check(cmd.canAccess(get), check.Equals, false)
	c.Check(cmd.canAccess(put), check.Equals, false)
	c.Check(cmd.canAccess(pst), check.Equals, false)
	c.Check(cmd.canAccess(del), check.Equals, false)

	cmd = &Command{UserOK: true}
	c.Check(cmd.canAccess(get), check.Equals, false)
	c.Check(cmd.canAccess(put), check.Equals, false)
	c.Check(cmd.canAccess(pst), check.Equals, false)
	c.Check(cmd.canAccess(del), check.Equals, false)

	cmd = &Command{GuestOK: true}
	c.Check(cmd.canAccess(get), check.Equals, true)
	c.Check(cmd.canAccess(put), check.Equals, false)
	c.Check(cmd.canAccess(pst), check.Equals, false)
	c.Check(cmd.canAccess(del), check.Equals, false)
}

func (s *daemonSuite) TestUserAccess(c *check.C) {
	get := &http.Request{Method: "GET", RemoteAddr: "uid=42;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "uid=42;"}

	cmd := &Command{}
	c.Check(cmd.canAccess(get), check.Equals, false)
	c.Check(cmd.canAccess(put), check.Equals, false)

	cmd = &Command{UserOK: true}
	c.Check(cmd.canAccess(get), check.Equals, true)
	c.Check(cmd.canAccess(put), check.Equals, false)

	cmd = &Command{GuestOK: true}
	c.Check(cmd.canAccess(get), check.Equals, true)
	c.Check(cmd.canAccess(put), check.Equals, false)
}

func (s *daemonSuite) TestSuperAccess(c *check.C) {
	get := &http.Request{Method: "GET", RemoteAddr: "uid=0;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "uid=0;"}

	cmd := &Command{}
	c.Check(cmd.canAccess(get), check.Equals, true)
	c.Check(cmd.canAccess(put), check.Equals, true)

	cmd = &Command{UserOK: true}
	c.Check(cmd.canAccess(get), check.Equals, true)
	c.Check(cmd.canAccess(put), check.Equals, true)

	cmd = &Command{GuestOK: true}
	c.Check(cmd.canAccess(get), check.Equals, true)
	c.Check(cmd.canAccess(put), check.Equals, true)
}

func (s *daemonSuite) TestAddRoutes(c *check.C) {
	d := newTestDaemon(c)

	expected := make([]string, len(api))
	for i, v := range api {
		expected[i] = v.Path
	}

	got := make([]string, 0, len(api))
	c.Assert(d.router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		got = append(got, route.GetName())
		return nil
	}), check.IsNil)

	c.Check(got, check.DeepEquals, expected) // this'll stop being true if routes are added that aren't commands (e.g. for the favicon)

	// XXX: still waiting to know how to check d.router.NotFoundHandler has been set to NotFound
	//      the old test relied on undefined behaviour:
	//      c.Check(fmt.Sprintf("%p", d.router.NotFoundHandler), check.Equals, fmt.Sprintf("%p", NotFound))
}
