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
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type daemonSuite struct{}

var _ = check.Suite(&daemonSuite{})

func (s *daemonSuite) SetUpTest(c *check.C) {
	os.Setenv("SNAPPY_SKIP_CHATTR_FOR_TESTS", "1")
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, check.IsNil)
}

func (s *daemonSuite) TearDownTest(c *check.C) {
	dirs.SetRootDir("")
	os.Unsetenv("SNAPPY_SKIP_CHATTR_FOR_TESTS")
}

// build a new daemon, with only a little of Init(), suitable for the tests
func newTestDaemon(c *check.C) *Daemon {
	d, err := New()
	c.Assert(err, check.IsNil)
	d.addRoutes()

	return d
}

// a Response suitable for testing
type mockHandler struct {
	cmd        *Command
	lastMethod string
}

func (mck *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mck.lastMethod = r.Method
}

func mkRF(c *check.C, cmd *Command, mck *mockHandler) ResponseFunc {
	return func(innerCmd *Command, req *http.Request, user *auth.UserState) Response {
		c.Assert(cmd, check.Equals, innerCmd)
		return mck
	}
}

func (s *daemonSuite) TestCommandMethodDispatch(c *check.C) {
	cmd := &Command{d: newTestDaemon(c)}
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
		c.Check(rec.Code, check.Equals, http.StatusUnauthorized, check.Commentf(method))

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

	cmd := &Command{d: newTestDaemon(c)}
	c.Check(cmd.canAccess(get, nil), check.Equals, false)
	c.Check(cmd.canAccess(put, nil), check.Equals, false)
	c.Check(cmd.canAccess(pst, nil), check.Equals, false)
	c.Check(cmd.canAccess(del, nil), check.Equals, false)

	cmd = &Command{d: newTestDaemon(c), UserOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, false)
	c.Check(cmd.canAccess(put, nil), check.Equals, false)
	c.Check(cmd.canAccess(pst, nil), check.Equals, false)
	c.Check(cmd.canAccess(del, nil), check.Equals, false)

	cmd = &Command{d: newTestDaemon(c), GuestOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, false)
	c.Check(cmd.canAccess(pst, nil), check.Equals, false)
	c.Check(cmd.canAccess(del, nil), check.Equals, false)

	// Since this request has no RemoteAddr, it must be coming from the snap
	// socket instead of the snapd one. In that case, if SnapOK is true, this
	// command should be wide open for all HTTP methods.
	cmd = &Command{d: newTestDaemon(c), SnapOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, true)
	c.Check(cmd.canAccess(pst, nil), check.Equals, true)
	c.Check(cmd.canAccess(del, nil), check.Equals, true)
}

func (s *daemonSuite) TestUserAccess(c *check.C) {
	get := &http.Request{Method: "GET", RemoteAddr: "uid=42;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "uid=42;"}

	cmd := &Command{d: newTestDaemon(c)}
	c.Check(cmd.canAccess(get, nil), check.Equals, false)
	c.Check(cmd.canAccess(put, nil), check.Equals, false)

	cmd = &Command{d: newTestDaemon(c), UserOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, false)

	cmd = &Command{d: newTestDaemon(c), GuestOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, false)

	// Since this request has a RemoteAddr, it must be coming from the snapd
	// socket instead of the snap one. In that case, SnapOK should have no
	// bearing on the default behavior, which is to deny access.
	cmd = &Command{d: newTestDaemon(c), SnapOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, false)
	c.Check(cmd.canAccess(put, nil), check.Equals, false)
}

func (s *daemonSuite) TestSuperAccess(c *check.C) {
	get := &http.Request{Method: "GET", RemoteAddr: "uid=0;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "uid=0;"}

	cmd := &Command{d: newTestDaemon(c)}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, true)

	cmd = &Command{d: newTestDaemon(c), UserOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, true)

	cmd = &Command{d: newTestDaemon(c), GuestOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, true)

	cmd = &Command{d: newTestDaemon(c), SnapOK: true}
	c.Check(cmd.canAccess(get, nil), check.Equals, true)
	c.Check(cmd.canAccess(put, nil), check.Equals, true)
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

type witnessAcceptListener struct {
	net.Listener
	accept chan struct{}
}

func (l *witnessAcceptListener) Accept() (net.Conn, error) {
	close(l.accept)
	return l.Listener.Accept()
}

func (s *daemonSuite) TestStartStop(c *check.C) {
	d := newTestDaemon(c)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{l, snapdAccept}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{l, snapAccept}

	d.Start()

	snapdDone := make(chan struct{})
	go func() {
		select {
		case <-snapdAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("snapd accept was not called")
		}
		close(snapdDone)
	}()

	snapDone := make(chan struct{})
	go func() {
		select {
		case <-snapAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("snapd accept was not called")
		}
		close(snapDone)
	}()

	<-snapdDone
	<-snapDone

	err = d.Stop()
	c.Check(err, check.IsNil)
}

func (s *daemonSuite) TestRestartWiring(c *check.C) {
	d := newTestDaemon(c)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{l, snapdAccept}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{l, snapAccept}

	d.Start()
	defer d.Stop()

	snapdDone := make(chan struct{})
	go func() {
		select {
		case <-snapdAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("snapd accept was not called")
		}
		close(snapdDone)
	}()

	snapDone := make(chan struct{})
	go func() {
		select {
		case <-snapAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("snap accept was not called")
		}
		close(snapDone)
	}()

	<-snapdDone
	<-snapDone

	d.overlord.State().RequestRestart(state.RestartDaemon)

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("RequestRestart -> overlord -> Kill chain didn't work")
	}
}
