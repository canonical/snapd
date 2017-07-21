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
	"fmt"

	"errors"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/polkit"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type daemonSuite struct {
	authResult polkit.AuthorizationResult
	err        error
}

var _ = check.Suite(&daemonSuite{})

func (s *daemonSuite) checkAuthorization(subject polkit.Subject, actionId string, details map[string]string, flags polkit.CheckAuthorizationFlags) (polkit.AuthorizationResult, error) {
	return s.authResult, s.err
}

func (s *daemonSuite) SetUpSuite(c *check.C) {
	snapstate.CanAutoRefresh = nil
	polkitCheckAuthorization = s.checkAuthorization
}

func (s *daemonSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, check.IsNil)
}

func (s *daemonSuite) TearDownTest(c *check.C) {
	dirs.SetRootDir("")
	s.authResult = polkit.AuthorizationResult{}
	s.err = nil
}

func (s *daemonSuite) TearDownSuite(c *check.C) {
	polkitCheckAuthorization = polkit.CheckAuthorization
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
		c.Check(rec.Code, check.Equals, 401, check.Commentf(method))

		rec = httptest.NewRecorder()
		req.RemoteAddr = "pid=100;uid=0;" + req.RemoteAddr

		cmd.ServeHTTP(rec, req)
		c.Check(mck.lastMethod, check.Equals, method)
		c.Check(rec.Code, check.Equals, 200)
	}

	req, err := http.NewRequest("POTATO", "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = "pid=100;uid=0;" + req.RemoteAddr

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 405)
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
	get := &http.Request{Method: "GET", RemoteAddr: "pid=100;uid=42;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "pid=100;uid=42;"}

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
	get := &http.Request{Method: "GET", RemoteAddr: "pid=100;uid=0;"}
	put := &http.Request{Method: "PUT", RemoteAddr: "pid=100;uid=0;"}

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

func (s *daemonSuite) TestPolkitAccess(c *check.C) {
	put := &http.Request{Method: "PUT", RemoteAddr: "pid=100;uid=42;"}
	cmd := &Command{d: newTestDaemon(c), ActionID: "polkit.action"}

	// polkit says user is not authorised
	s.authResult.IsAuthorized = false
	c.Check(cmd.canAccess(put, nil), check.Equals, false)

	// polkit grants authorisation
	s.authResult.IsAuthorized = true
	c.Check(cmd.canAccess(put, nil), check.Equals, true)

	// an error occurs communicating with polkit
	s.err = errors.New("error")
	c.Check(cmd.canAccess(put, nil), check.Equals, false)
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

	accept  chan struct{}
	accept1 bool

	closed    chan struct{}
	closed1   bool
	closedLck sync.Mutex
}

func (l *witnessAcceptListener) Accept() (net.Conn, error) {
	if !l.accept1 {
		l.accept1 = true
		close(l.accept)
	}
	return l.Listener.Accept()
}

func (l *witnessAcceptListener) Close() error {
	err := l.Listener.Close()
	if l.closed != nil {
		l.closedLck.Lock()
		defer l.closedLck.Unlock()
		if !l.closed1 {
			l.closed1 = true
			close(l.closed)
		}
	}
	return err
}

func (s *daemonSuite) TestStartStop(c *check.C) {
	d := newTestDaemon(c)
	st := d.overlord.State()
	// mark as already seeded
	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: l, accept: snapdAccept}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: l, accept: snapAccept}

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
	// mark as already seeded
	st := d.overlord.State()
	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: l, accept: snapdAccept}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: l, accept: snapAccept}

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

func (s *daemonSuite) TestGracefulStop(c *check.C) {
	d := newTestDaemon(c)

	responding := make(chan struct{})
	doRespond := make(chan bool, 1)

	d.router.HandleFunc("/endp", func(w http.ResponseWriter, r *http.Request) {
		close(responding)
		if <-doRespond {
			w.Write([]byte("OKOK"))
		} else {
			w.Write([]byte("Gone"))
		}
		return
	})

	st := d.overlord.State()
	// mark as already seeded
	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	snapdL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	snapdClosed := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: snapdL, accept: snapdAccept, closed: snapdClosed}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: snapL, accept: snapAccept}

	d.Start()

	snapdAccepting := make(chan struct{})
	go func() {
		select {
		case <-snapdAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("snapd accept was not called")
		}
		close(snapdAccepting)
	}()

	snapAccepting := make(chan struct{})
	go func() {
		select {
		case <-snapAccept:
		case <-time.After(2 * time.Second):
			c.Fatal("snapd accept was not called")
		}
		close(snapAccepting)
	}()

	<-snapdAccepting
	<-snapAccepting

	alright := make(chan struct{})

	go func() {
		res, err := http.Get(fmt.Sprintf("http://%s/endp", snapdL.Addr()))
		c.Assert(err, check.IsNil)
		c.Check(res.StatusCode, check.Equals, 200)
		body, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		c.Assert(err, check.IsNil)
		c.Check(string(body), check.Equals, "OKOK")
		close(alright)
	}()
	go func() {
		<-snapdClosed
		time.Sleep(200 * time.Millisecond)
		doRespond <- true
	}()

	<-responding
	err = d.Stop()
	doRespond <- false
	c.Check(err, check.IsNil)

	select {
	case <-alright:
	case <-time.After(2 * time.Second):
		c.Fatal("never got proper response")
	}
}
