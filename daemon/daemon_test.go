// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/iotest"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/patch"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/standby"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type daemonSuite struct {
	testutil.BaseTest

	authorized bool
	err        error
	notified   []string
}

var _ = check.Suite(&daemonSuite{})

func (s *daemonSuite) SetUpTest(c *check.C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(osutil.MockMountInfo(""))

	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	c.Assert(err, check.IsNil)
	systemdSdNotify = func(notif string) error {
		s.notified = append(s.notified, notif)
		return nil
	}
	s.notified = nil
	s.AddCleanup(ifacestate.MockSecurityBackends(nil))
	s.AddCleanup(MockRebootNoticeWait(0))

	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapdSocket), 0755), check.IsNil)
}

func (s *daemonSuite) TearDownTest(c *check.C) {
	systemdSdNotify = systemd.SdNotify
	dirs.SetRootDir("")
	s.authorized = false
	s.err = nil

	s.BaseTest.TearDownTest(c)
}

// build a new daemon, with only a little of Init(), suitable for the tests
func (s *daemonSuite) newTestDaemon(c *check.C) *Daemon {
	d, err := New()
	c.Assert(err, check.IsNil)
	d.addRoutes()

	// don't actually try to talk to the store on snapstate.Ensure
	// needs doing after the call to devicestate.Manager (which
	// happens in daemon.New via overlord.New)
	snapstate.CanAutoRefresh = nil

	if d.Overlord() != nil {
		s.AddCleanup(snapstate.MockEnsuredMountsUpdated(d.Overlord().SnapManager(), true))
	}

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

func (s *daemonSuite) TestCommandMethodDispatch(c *check.C) {
	d := s.newTestDaemon(c)
	st := d.Overlord().State()
	st.Lock()
	authUser, err := auth.NewUser(st, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	st.Unlock()
	c.Assert(err, check.IsNil)

	fakeUserAgent := "some-agent-talking-to-snapd/1.0"

	cmd := &Command{d: d}
	mck := &mockHandler{cmd: cmd}
	rf := func(innerCmd *Command, req *http.Request, user *auth.UserState) Response {
		c.Assert(cmd, check.Equals, innerCmd)
		c.Check(store.ClientUserAgent(req.Context()), check.Equals, fakeUserAgent)
		c.Check(user, check.DeepEquals, authUser)
		return mck
	}
	cmd.GET = rf
	cmd.PUT = rf
	cmd.POST = rf
	cmd.ReadAccess = authenticatedAccess{}
	cmd.WriteAccess = authenticatedAccess{}

	for _, method := range []string{"GET", "POST", "PUT"} {
		req := httptest.NewRequest(method, "/", nil)
		req.Header.Add("User-Agent", fakeUserAgent)

		rec := httptest.NewRecorder()
		req.RemoteAddr = fmt.Sprintf("pid=100;uid=1001;socket=%s;", dirs.SnapdSocket)
		cmd.ServeHTTP(rec, req)
		c.Check(rec.Code, check.Equals, 401, check.Commentf(method))

		rec = httptest.NewRecorder()
		req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, authUser.Macaroon))

		cmd.ServeHTTP(rec, req)
		c.Check(mck.lastMethod, check.Equals, method)
		c.Check(rec.Code, check.Equals, 200)
	}

	req := httptest.NewRequest("POTATO", "/", nil)
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=1001;socket=%s;", dirs.SnapdSocket)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, authUser.Macaroon))
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 405)
}

func (s *daemonSuite) TestCommandMethodDispatchRoot(c *check.C) {
	fakeUserAgent := "some-agent-talking-to-snapd/1.0"

	cmd := &Command{d: s.newTestDaemon(c)}
	mck := &mockHandler{cmd: cmd}
	rf := func(innerCmd *Command, req *http.Request, user *auth.UserState) Response {
		c.Assert(cmd, check.Equals, innerCmd)
		c.Check(store.ClientUserAgent(req.Context()), check.Equals, fakeUserAgent)
		return mck
	}
	cmd.GET = rf
	cmd.PUT = rf
	cmd.POST = rf
	cmd.ReadAccess = authenticatedAccess{}
	cmd.WriteAccess = authenticatedAccess{}

	for _, method := range []string{"GET", "POST", "PUT"} {
		req := httptest.NewRequest(method, "/", nil)
		req.Header.Add("User-Agent", fakeUserAgent)

		rec := httptest.NewRecorder()
		// no ucred => forbidden
		req.RemoteAddr = ""
		cmd.ServeHTTP(rec, req)
		c.Check(rec.Code, check.Equals, 403, check.Commentf(method))

		rec = httptest.NewRecorder()
		req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)

		cmd.ServeHTTP(rec, req)
		c.Check(mck.lastMethod, check.Equals, method)
		c.Check(rec.Code, check.Equals, 200)
	}

	req := httptest.NewRequest("POTATO", "/", nil)
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 405)
}

func (s *daemonSuite) TestCommandRestartingState(c *check.C) {
	d := s.newTestDaemon(c)

	cmd := &Command{d: d}
	cmd.GET = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.ReadAccess = openAccess{}
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=42;socket=%s;", dirs.SnapdSocket)

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var rst struct {
		Maintenance *errorResult `json:"maintenance"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.Maintenance, check.IsNil)

	tests := []struct {
		rst  restart.RestartType
		kind client.ErrorKind
		msg  string
		op   string
	}{
		{
			rst:  restart.RestartSystem,
			kind: client.ErrorKindSystemRestart,
			msg:  "system is restarting",
			op:   "reboot",
		}, {
			rst:  restart.RestartSystemNow,
			kind: client.ErrorKindSystemRestart,
			msg:  "system is restarting",
			op:   "reboot",
		}, {
			rst:  restart.RestartDaemon,
			kind: client.ErrorKindDaemonRestart,
			msg:  "daemon is restarting",
		}, {
			rst:  restart.RestartSystemHaltNow,
			kind: client.ErrorKindSystemRestart,
			msg:  "system is halting",
			op:   "halt",
		}, {
			rst:  restart.RestartSystemPoweroffNow,
			kind: client.ErrorKindSystemRestart,
			msg:  "system is powering off",
			op:   "poweroff",
		}, {
			rst:  restart.RestartSocket,
			kind: client.ErrorKindDaemonRestart,
			msg:  "daemon is stopping to wait for socket activation",
		},
	}

	for _, t := range tests {
		d.overlord.RestartManager().MockPending(t.rst)
		rec = httptest.NewRecorder()
		cmd.ServeHTTP(rec, req)
		c.Check(rec.Code, check.Equals, 200)
		var rst struct {
			Maintenance *errorResult `json:"maintenance"`
		}
		err = json.Unmarshal(rec.Body.Bytes(), &rst)
		c.Assert(err, check.IsNil)
		var val errorValue
		if t.op != "" {
			val = map[string]any{
				"op": t.op,
			}
		}
		c.Check(rst.Maintenance, check.DeepEquals, &errorResult{
			Kind:    t.kind,
			Message: t.msg,
			Value:   val,
		})
	}
}

func (s *daemonSuite) TestMaintenanceJsonDeletedOnStart(c *check.C) {
	// write a maintenance.json file that has that the system is restarting
	maintErr := &errorResult{
		Kind:    client.ErrorKindDaemonRestart,
		Message: systemRestartMsg,
	}

	b, err := json.Marshal(maintErr)
	c.Assert(err, check.IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(dirs.SnapdMaintenanceFile), 0755), check.IsNil)
	c.Assert(os.WriteFile(dirs.SnapdMaintenanceFile, b, 0644), check.IsNil)

	d := s.newTestDaemon(c)
	makeDaemonListeners(c, d)

	s.markSeeded(d)

	// after starting, maintenance.json should be removed
	c.Assert(d.Start(context.Background()), check.IsNil)
	c.Assert(dirs.SnapdMaintenanceFile, testutil.FileAbsent)
	d.Stop(nil)
}

func (s *daemonSuite) TestFillsWarnings(c *check.C) {
	d := s.newTestDaemon(c)

	cmd := &Command{d: d}
	cmd.GET = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.ReadAccess = openAccess{}
	req, err := http.NewRequest("GET", "", nil)
	c.Assert(err, check.IsNil)
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=42;socket=%s;", dirs.SnapdSocket)

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	var rst struct {
		WarningTimestamp *time.Time `json:"warning-timestamp,omitempty"`
		WarningCount     int        `json:"warning-count,omitempty"`
	}
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.WarningCount, check.Equals, 0)
	c.Check(rst.WarningTimestamp, check.IsNil)

	st := d.overlord.State()
	st.Lock()
	st.Warnf("hello world")
	st.Unlock()

	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	err = json.Unmarshal(rec.Body.Bytes(), &rst)
	c.Assert(err, check.IsNil)
	c.Check(rst.WarningCount, check.Equals, 1)
	c.Check(rst.WarningTimestamp, check.NotNil)
}

type accessCheckFunc func(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError

func (f accessCheckFunc) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	return f(d, r, ucred, user)
}

func (s *daemonSuite) TestReadAccess(c *check.C) {
	cmd := &Command{d: s.newTestDaemon(c)}
	cmd.GET = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	var accessCalled bool
	cmd.ReadAccess = accessCheckFunc(func(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
		accessCalled = true
		c.Check(d, check.Equals, cmd.d)
		c.Check(r, check.NotNil)
		c.Assert(ucred, check.NotNil)
		c.Check(ucred.Uid, check.Equals, uint32(42))
		c.Check(ucred.Pid, check.Equals, int32(100))
		c.Check(ucred.Socket, check.Equals, "xyz")
		c.Check(user, check.IsNil)
		return nil
	})
	cmd.WriteAccess = accessCheckFunc(func(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
		c.Fail()
		return Forbidden("")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "pid=100;uid=42;socket=xyz;"
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(accessCalled, check.Equals, true)
}

func (s *daemonSuite) TestWriteAccess(c *check.C) {
	cmd := &Command{d: s.newTestDaemon(c)}
	cmd.PUT = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.POST = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.ReadAccess = accessCheckFunc(func(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
		c.Fail()
		return Forbidden("")
	})
	var accessCalled bool
	cmd.WriteAccess = accessCheckFunc(func(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
		accessCalled = true
		c.Check(d, check.Equals, cmd.d)
		c.Check(r, check.NotNil)
		c.Assert(ucred, check.NotNil)
		c.Check(ucred.Uid, check.Equals, uint32(42))
		c.Check(ucred.Pid, check.Equals, int32(100))
		c.Check(ucred.Socket, check.Equals, "xyz")
		c.Check(user, check.IsNil)
		return nil
	})

	req := httptest.NewRequest("PUT", "/", nil)
	req.RemoteAddr = "pid=100;uid=42;socket=xyz;"
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(accessCalled, check.Equals, true)

	accessCalled = false
	req = httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "pid=100;uid=42;socket=xyz;"
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(accessCalled, check.Equals, true)
}

func (s *daemonSuite) TestWriteAccessWithUser(c *check.C) {
	d := s.newTestDaemon(c)
	st := d.Overlord().State()
	st.Lock()
	authUser, err := auth.NewUser(st, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	st.Unlock()
	c.Assert(err, check.IsNil)

	cmd := &Command{d: d}
	cmd.PUT = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.POST = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.ReadAccess = accessCheckFunc(func(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
		c.Fail()
		return Forbidden("")
	})
	var accessCalled bool
	cmd.WriteAccess = accessCheckFunc(func(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
		accessCalled = true
		c.Check(d, check.Equals, cmd.d)
		c.Check(r, check.NotNil)
		c.Assert(ucred, check.NotNil)
		c.Check(ucred.Uid, check.Equals, uint32(1001))
		c.Check(ucred.Pid, check.Equals, int32(100))
		c.Check(ucred.Socket, check.Equals, "xyz")
		c.Check(user, check.DeepEquals, authUser)
		return nil
	})

	req := httptest.NewRequest("PUT", "/", nil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, authUser.Macaroon))
	req.RemoteAddr = "pid=100;uid=1001;socket=xyz;"
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(accessCalled, check.Equals, true)

	accessCalled = false
	req = httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", fmt.Sprintf(`Macaroon root="%s"`, authUser.Macaroon))
	req.RemoteAddr = "pid=100;uid=1001;socket=xyz;"
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(accessCalled, check.Equals, true)
}

func (s *daemonSuite) TestPolkitAccessPath(c *check.C) {
	cmd := &Command{d: s.newTestDaemon(c)}
	cmd.POST = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	access := false
	cmd.WriteAccess = authenticatedAccess{Polkit: "foo"}
	checkPolkitAction = func(r *http.Request, ucred *ucrednet, action string) *apiError {
		c.Check(action, check.Equals, "foo")
		c.Check(ucred.Uid, check.Equals, uint32(1001))
		if access {
			return nil
		}
		return AuthCancelled("")
	}

	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=1001;socket=%s;", dirs.SnapdSocket)
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 403)
	c.Check(rec.Body.String(), testutil.Contains, `"kind":"auth-cancelled"`)

	access = true
	rec = httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
}

func (s *daemonSuite) TestCommandAccessSane(c *check.C) {
	for _, cmd := range api {
		// If Command.GET is set, ReadAccess must be set
		c.Check(cmd.GET != nil, check.Equals, cmd.ReadAccess != nil, check.Commentf("%q ReadAccess", cmd.Path))
		// If Command.PUT or POST are set, WriteAccess must be set
		c.Check(cmd.PUT != nil || cmd.POST != nil, check.Equals, cmd.WriteAccess != nil, check.Commentf("%q WriteAccess", cmd.Path))
	}
}

func (s *daemonSuite) TestAddRoutes(c *check.C) {
	d := s.newTestDaemon(c)

	expected := make([]string, len(api))
	for i, v := range api {
		if v.PathPrefix != "" {
			expected[i] = v.PathPrefix
			continue
		}
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

	idempotClose sync.Once
	closeErr     error
	closed       chan struct{}
}

func (l *witnessAcceptListener) Accept() (net.Conn, error) {
	if !l.accept1 {
		l.accept1 = true
		close(l.accept)
	}
	return l.Listener.Accept()
}

func (l *witnessAcceptListener) Close() error {
	l.idempotClose.Do(func() {
		l.closeErr = l.Listener.Close()
		if l.closed != nil {
			close(l.closed)
		}
	})
	return l.closeErr
}

func (s *daemonSuite) markSeeded(d *Daemon) {
	st := d.overlord.State()
	st.Lock()
	devicestatetest.MarkInitialized(st)
	st.Unlock()
}

func (s *daemonSuite) TestStartStop(c *check.C) {
	d := s.newTestDaemon(c)
	// mark as already seeded
	s.markSeeded(d)
	// and pretend we have snaps
	st := d.overlord.State()
	st.Lock()
	si := &snap.SideInfo{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"}
	snapstate.Set(st, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
	})
	st.Unlock()
	snaptest.MockSnap(c, `name: core
version: 1`, si)
	// 1 snap => extended timeout 30s + 5s
	const extendedTimeoutUSec = "EXTEND_TIMEOUT_USEC=35000000"

	l1, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: l1, accept: snapdAccept}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: l2, accept: snapAccept}

	c.Assert(d.Start(context.Background()), check.IsNil)

	c.Check(s.notified, check.DeepEquals, []string{extendedTimeoutUSec, "READY=1"})

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

	err = d.Stop(nil)
	c.Check(err, check.IsNil)

	c.Check(s.notified, check.DeepEquals, []string{extendedTimeoutUSec, "READY=1", "STOPPING=1"})
}

func (s *daemonSuite) TestRestartWiring(c *check.C) {
	d := s.newTestDaemon(c)

	var systemctlArgs [][]string
	systemctlMock := systemd.MockSystemctl(func(args ...string) (buf []byte, err error) {
		systemctlArgs = append(systemctlArgs, args)
		return nil, nil
	})
	defer systemctlMock()

	// mark as already seeded
	s.markSeeded(d)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: l, accept: snapdAccept}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: l, accept: snapAccept}

	c.Assert(d.Start(context.Background()), check.IsNil)
	stoppedYet := false
	defer func() {
		if !stoppedYet {
			d.Stop(nil)
		}
	}()

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

	// Ensure sockets are now ready to accept connections
	<-snapdDone
	<-snapDone

	st := d.overlord.State()
	st.Lock()
	restart.Request(st, restart.RestartDaemon, nil)
	st.Unlock()

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("restart.Request -> daemon -> Kill chain didn't work")
	}

	c.Assert(d.Stop(nil), check.IsNil)
	stoppedYet = true

	c.Assert(s.notified, check.DeepEquals, []string{"EXTEND_TIMEOUT_USEC=30000000", "READY=1", "STOPPING=1"})
	c.Assert(systemctlArgs, check.DeepEquals, [][]string{
		{"start", "--no-block", "snapd.apparmor.service"},
		{"start", "--no-block", "snapd.service"},
		{"start", "--no-block", "snapd.seeded.service"},
		{"start", "--no-block", "snapd.autoimport.service"}})
}

func (s *daemonSuite) TestGracefulStop(c *check.C) {
	d := s.newTestDaemon(c)

	responding := make(chan struct{})
	doRespond := make(chan bool, 1)

	d.router.HandleFunc("/endp", func(w http.ResponseWriter, r *http.Request) {
		close(responding)
		if <-doRespond {
			w.Write([]byte("OKOK"))
		} else {
			w.Write([]byte("Gone"))
		}
	})

	// mark as already seeded
	s.markSeeded(d)
	// and pretend we have snaps
	st := d.overlord.State()
	st.Lock()
	si := &snap.SideInfo{RealName: "core", Revision: snap.R(1), SnapID: "core-snap-id"}
	snapstate.Set(st, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
	})
	st.Unlock()
	snaptest.MockSnap(c, `name: core
version: 1`, si)

	snapdL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	snapdClosed := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: snapdL, accept: snapdAccept, closed: snapdClosed}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: snapL, accept: snapAccept}

	c.Assert(d.Start(context.Background()), check.IsNil)

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
		body, err := io.ReadAll(res.Body)
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
	err = d.Stop(nil)
	doRespond <- false
	c.Check(err, check.IsNil)

	select {
	case <-alright:
	case <-time.After(2 * time.Second):
		c.Fatal("never got proper response")
	}
}

func (s *daemonSuite) TestGracefulStopHasLimits(c *check.C) {
	d := s.newTestDaemon(c)

	// mark as already seeded
	s.markSeeded(d)

	restore := MockShutdownTimeout(time.Second)
	defer restore()

	responding := make(chan struct{})
	doRespond := make(chan bool, 1)

	d.router.HandleFunc("/endp", func(w http.ResponseWriter, r *http.Request) {
		close(responding)
		if <-doRespond {
			for {
				// write in a loop to keep the handler running
				if _, err := w.Write([]byte("OKOK")); err != nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		} else {
			w.Write([]byte("Gone"))
		}
	})

	snapdL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	snapdClosed := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: snapdL, accept: snapdAccept, closed: snapdClosed}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: snapL, accept: snapAccept}

	c.Assert(d.Start(context.Background()), check.IsNil)

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

	clientErr := make(chan error)

	go func() {
		_, err := http.Get(fmt.Sprintf("http://%s/endp", snapdL.Addr()))
		c.Assert(err, check.NotNil)
		clientErr <- err
		close(clientErr)
	}()
	go func() {
		<-snapdClosed
		time.Sleep(200 * time.Millisecond)
		doRespond <- true
	}()

	<-responding
	err = d.Stop(nil)
	doRespond <- false
	c.Check(err, check.IsNil)

	select {
	case cErr := <-clientErr:
		c.Check(cErr, check.ErrorMatches, ".*: EOF")
	case <-time.After(5 * time.Second):
		c.Fatal("never got proper response")
	}
}

func (s *daemonSuite) testRestartSystemWiring(c *check.C, prep func(d *Daemon), doRestart func(*state.State, restart.RestartType, *boot.RebootInfo), restartKind restart.RestartType, wait time.Duration) {
	d := s.newTestDaemon(c)
	// mark as already seeded
	s.markSeeded(d)

	if prep != nil {
		prep(d)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: l, accept: snapdAccept}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: l, accept: snapAccept}

	oldRebootNoticeWait := rebootNoticeWait
	oldRebootWaitTimeout := rebootWaitTimeout
	defer func() {
		reboot = boot.Reboot
		rebootNoticeWait = oldRebootNoticeWait
		rebootWaitTimeout = oldRebootWaitTimeout
	}()
	rebootWaitTimeout = 100 * time.Millisecond
	rebootNoticeWait = 150 * time.Millisecond

	expectedAction := boot.RebootReboot
	expectedOp := "reboot"
	if restartKind == restart.RestartSystemHaltNow {
		expectedAction = boot.RebootHalt
		expectedOp = "halt"
	} else if restartKind == restart.RestartSystemPoweroffNow {
		expectedAction = boot.RebootPoweroff
		expectedOp = "poweroff"
	}
	var delays []time.Duration
	reboot = func(a boot.RebootAction, d time.Duration, ri *boot.RebootInfo) error {
		c.Check(a, check.Equals, expectedAction)
		delays = append(delays, d)
		return nil
	}

	c.Assert(d.Start(context.Background()), check.IsNil)
	defer d.Stop(nil)

	st := d.overlord.State()

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

	st.Lock()
	doRestart(st, restartKind, nil)
	st.Unlock()

	defer func() {
		d.mu.Lock()
		d.requestedRestart = restart.RestartUnset
		d.mu.Unlock()
	}()

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("restart.Request -> daemon -> Kill chain didn't work")
	}

	d.mu.Lock()
	rs := d.requestedRestart
	d.mu.Unlock()

	c.Check(rs, check.Equals, restartKind)

	c.Check(delays, check.HasLen, 1)
	c.Check(delays[0], check.DeepEquals, rebootWaitTimeout)

	now := time.Now()

	err = d.Stop(nil)

	// ensure Stop waited for at least rebootWaitTimeout
	timeToStop := time.Since(now)
	c.Check(timeToStop > rebootWaitTimeout+rebootNoticeWait, check.Equals, true)
	c.Check(err, check.ErrorMatches, fmt.Sprintf("expected %s did not happen", expectedAction))

	c.Check(delays, check.HasLen, 2)
	c.Check(delays[1], check.DeepEquals, wait)

	// we are not stopping, we wait for the reboot instead
	c.Check(s.notified, check.DeepEquals, []string{"EXTEND_TIMEOUT_USEC=30000000", "READY=1"})

	st.Lock()
	defer st.Unlock()
	var rebootAt time.Time
	err = st.Get("daemon-system-restart-at", &rebootAt)
	c.Assert(err, check.IsNil)
	if wait > 0 {
		approxAt := now.Add(wait)
		c.Check(rebootAt.After(approxAt) || rebootAt.Equal(approxAt), check.Equals, true)
	} else {
		// should be good enough
		c.Check(rebootAt.Before(now.Add(10*time.Second)), check.Equals, true)
	}

	// finally check that maintenance.json was written appropriate for this
	// restart reason
	b, err := os.ReadFile(dirs.SnapdMaintenanceFile)
	c.Assert(err, check.IsNil)

	maintErr := &errorResult{}
	c.Assert(json.Unmarshal(b, maintErr), check.IsNil)
	c.Check(maintErr.Kind, check.Equals, client.ErrorKindSystemRestart)
	c.Check(maintErr.Value, check.DeepEquals, map[string]any{
		"op": expectedOp,
	})

	exp := maintenanceForRestartType(restartKind)
	c.Assert(maintErr, check.DeepEquals, exp)
}

func (s *daemonSuite) TestRestartSystemGracefulWiring(c *check.C) {
	s.testRestartSystemWiring(c, nil, restart.Request, restart.RestartSystem, 1*time.Minute)
}

func (s *daemonSuite) TestRestartSystemImmediateWiring(c *check.C) {
	s.testRestartSystemWiring(c, nil, restart.Request, restart.RestartSystemNow, 0)
}

func (s *daemonSuite) TestRestartSystemHaltImmediateWiring(c *check.C) {
	s.testRestartSystemWiring(c, nil, restart.Request, restart.RestartSystemHaltNow, 0)
}

func (s *daemonSuite) TestRestartSystemPoweroffImmediateWiring(c *check.C) {
	s.testRestartSystemWiring(c, nil, restart.Request, restart.RestartSystemPoweroffNow, 0)
}

type rstManager struct {
	st *state.State
}

func (m *rstManager) Ensure() error {
	m.st.Lock()
	defer m.st.Unlock()
	restart.Request(m.st, restart.RestartSystemNow, nil)
	return nil
}

type witnessManager struct {
	ensureCalled int
}

func (m *witnessManager) Ensure() error {
	m.ensureCalled++
	return nil
}

func (s *daemonSuite) TestRestartSystemFromEnsure(c *check.C) {
	// Test that calling restart.Request from inside the first
	// Ensure loop works.
	wm := &witnessManager{}

	prep := func(d *Daemon) {
		st := d.overlord.State()
		hm := d.overlord.HookManager()
		o := overlord.MockWithState(st)
		d.overlord = o
		o.AddManager(hm)
		rm := &rstManager{st: st}
		o.AddManager(rm)
		o.AddManager(wm)
	}

	nop := func(*state.State, restart.RestartType, *boot.RebootInfo) {}

	s.testRestartSystemWiring(c, prep, nop, restart.RestartSystemNow, 0)

	c.Check(wm.ensureCalled, check.Equals, 1)
}

func makeDaemonListeners(c *check.C, d *Daemon) {
	snapdL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapL, err := net.Listen("tcp", "127.0.0.1:0")
	c.Assert(err, check.IsNil)

	snapdAccept := make(chan struct{})
	snapdClosed := make(chan struct{})
	d.snapdListener = &witnessAcceptListener{Listener: snapdL, accept: snapdAccept, closed: snapdClosed}

	snapAccept := make(chan struct{})
	d.snapListener = &witnessAcceptListener{Listener: snapL, accept: snapAccept}
}

// This test tests that when the snapd calls a restart of the system
// a sigterm (from e.g. systemd) is handled when it arrives before
// stop is fully done.
func (s *daemonSuite) TestRestartShutdownWithSigtermInBetween(c *check.C) {
	oldRebootNoticeWait := rebootNoticeWait
	defer func() {
		rebootNoticeWait = oldRebootNoticeWait
	}()
	rebootNoticeWait = 150 * time.Millisecond

	nRebootCall := 0
	rebootCheck := func(ra boot.RebootAction, d time.Duration, ri *boot.RebootInfo) error {
		// Check arguments passed to reboot call
		nRebootCall++
		c.Check(ra, check.Equals, boot.RebootReboot)
		switch nRebootCall {
		case 1:
			c.Check(d, check.Equals, 10*time.Minute)
			c.Check(ri, check.IsNil)
		case 2:
			c.Check(d, check.Equals, 1*time.Minute)
			c.Check(ri, check.IsNil)
		default:
			c.Error("reboot called more times than expected")
		}
		return nil
	}
	r := MockReboot(rebootCheck)
	defer r()

	d := s.newTestDaemon(c)
	makeDaemonListeners(c, d)
	s.markSeeded(d)

	c.Assert(d.Start(context.Background()), check.IsNil)
	st := d.overlord.State()

	st.Lock()
	restart.Request(st, restart.RestartSystem, nil)
	st.Unlock()

	ch := make(chan os.Signal, 2)
	ch <- syscall.SIGTERM
	// stop will check if we got a sigterm in between (which we did)
	err := d.Stop(ch)
	c.Assert(err, check.IsNil)

	// we must have called reboot twice
	c.Check(nRebootCall, check.Equals, 2)
}

// This test tests that when there is a shutdown we close the sigterm
// handler so that systemd can kill snapd.
func (s *daemonSuite) TestRestartShutdown(c *check.C) {
	oldRebootNoticeWait := rebootNoticeWait
	oldRebootWaitTimeout := rebootWaitTimeout
	defer func() {
		rebootNoticeWait = oldRebootNoticeWait
		rebootWaitTimeout = oldRebootWaitTimeout
	}()
	rebootWaitTimeout = 100 * time.Millisecond
	rebootNoticeWait = 150 * time.Millisecond

	nRebootCall := 0
	rebootCheck := func(ra boot.RebootAction, d time.Duration, ri *boot.RebootInfo) error {
		// Check arguments passed to reboot call
		nRebootCall++
		c.Check(ra, check.Equals, boot.RebootReboot)
		switch nRebootCall {
		case 1:
			c.Check(d, check.Equals, 100*time.Millisecond)
			c.Check(ri, check.IsNil)
		case 2:
			c.Check(d, check.Equals, 1*time.Minute)
			c.Check(ri, check.IsNil)
		default:
			c.Error("reboot called more times than expected")
		}
		return nil
	}
	r := MockReboot(rebootCheck)
	defer r()

	d := s.newTestDaemon(c)
	makeDaemonListeners(c, d)
	s.markSeeded(d)

	c.Assert(d.Start(context.Background()), check.IsNil)
	st := d.overlord.State()

	st.Lock()
	restart.Request(st, restart.RestartSystem, nil)
	st.Unlock()

	sigCh := make(chan os.Signal, 2)
	// stop (this will timeout but that's not relevant for this test)
	d.Stop(sigCh)

	// ensure that the sigCh got closed as part of the stop
	_, chOpen := <-sigCh
	c.Assert(chOpen, check.Equals, false)

	// we must have called reboot twice
	c.Check(nRebootCall, check.Equals, 2)
}

func (s *daemonSuite) TestRestartExpectedRebootDidNotHappen(c *check.C) {
	curBootID, err := osutil.BootID()
	c.Assert(err, check.IsNil)

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","refresh-privacy-key":"0123456789ABCDEF","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, curBootID, time.Now().UTC().Format(time.RFC3339)))
	err = os.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, check.IsNil)

	oldRebootNoticeWait := rebootNoticeWait
	oldRebootRetryWaitTimeout := rebootRetryWaitTimeout
	defer func() {
		rebootNoticeWait = oldRebootNoticeWait
		rebootRetryWaitTimeout = oldRebootRetryWaitTimeout
	}()
	rebootRetryWaitTimeout = 100 * time.Millisecond
	rebootNoticeWait = 150 * time.Millisecond

	nRebootCall := 0
	rebootCheck := func(ra boot.RebootAction, d time.Duration, ri *boot.RebootInfo) error {
		nRebootCall++
		// an immediate shutdown was scheduled again
		c.Check(ra, check.Equals, boot.RebootReboot)
		c.Check(d <= 0, check.Equals, true)
		c.Check(ri, check.IsNil)
		return nil
	}
	r := MockReboot(rebootCheck)
	defer r()

	d := s.newTestDaemon(c)
	c.Check(d.overlord, check.IsNil)
	c.Check(d.expectedRebootDidNotHappen, check.Equals, true)

	var n int
	d.state.Lock()
	err = d.state.Get("daemon-system-restart-tentative", &n)
	d.state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(n, check.Equals, 1)

	c.Assert(d.Start(context.Background()), check.IsNil)

	c.Check(s.notified, check.DeepEquals, []string{"READY=1"})

	select {
	case <-d.Dying():
	case <-time.After(2 * time.Second):
		c.Fatal("expected reboot not happening should proceed to try to shutdown again")
	}

	sigCh := make(chan os.Signal, 2)
	// stop (this will timeout but thats not relevant for this test)
	d.Stop(sigCh)

	// we must have called reboot once
	c.Check(nRebootCall, check.Equals, 1)
}

func (s *daemonSuite) TestRestartExpectedRebootOK(c *check.C) {
	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","refresh-privacy-key":"0123456789ABCDEF","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s"},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, "boot-id-0", time.Now().UTC().Format(time.RFC3339)))
	err := os.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, check.IsNil)

	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newTestDaemon(c)
	c.Assert(d.overlord, check.NotNil)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	var v any
	// these were cleared
	c.Check(st.Get("daemon-system-restart-at", &v), testutil.ErrorIs, state.ErrNoState)
	c.Check(st.Get("system-restart-from-boot-id", &v), testutil.ErrorIs, state.ErrNoState)
}

func (s *daemonSuite) TestRestartExpectedRebootGiveUp(c *check.C) {
	// we give up trying to restart the system after 3 retry tentatives
	curBootID, err := osutil.BootID()
	c.Assert(err, check.IsNil)

	fakeState := []byte(fmt.Sprintf(`{"data":{"patch-level":%d,"patch-sublevel":%d,"some":"data","refresh-privacy-key":"0123456789ABCDEF","system-restart-from-boot-id":%q,"daemon-system-restart-at":"%s","daemon-system-restart-tentative":3},"changes":null,"tasks":null,"last-change-id":0,"last-task-id":0,"last-lane-id":0}`, patch.Level, patch.Sublevel, curBootID, time.Now().UTC().Format(time.RFC3339)))
	err = os.WriteFile(dirs.SnapStateFile, fakeState, 0600)
	c.Assert(err, check.IsNil)

	cmd := testutil.MockCommand(c, "shutdown", "")
	defer cmd.Restore()

	d := s.newTestDaemon(c)
	c.Assert(d.overlord, check.NotNil)

	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	var v any
	// these were cleared
	c.Check(st.Get("daemon-system-restart-at", &v), testutil.ErrorIs, state.ErrNoState)
	c.Check(st.Get("system-restart-from-boot-id", &v), testutil.ErrorIs, state.ErrNoState)
	c.Check(st.Get("daemon-system-restart-tentative", &v), testutil.ErrorIs, state.ErrNoState)
}

func (s *daemonSuite) TestRestartIntoSocketModeNoNewChanges(c *check.C) {
	restore := standby.MockStandbyWait(5 * time.Millisecond)
	defer restore()

	d := s.newTestDaemon(c)
	makeDaemonListeners(c, d)

	// mark as already seeded, we also have no snaps so this will
	// go into socket activation mode
	s.markSeeded(d)

	c.Assert(d.Start(context.Background()), check.IsNil)
	// pretend some ensure happened
	for i := 0; i < 5; i++ {
		c.Check(d.overlord.StateEngine().Ensure(), check.IsNil)
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-d.Dying():
		// exit the loop
	case <-time.After(15 * time.Second):
		c.Errorf("daemon did not stop after 15s")
	}
	err := d.Stop(nil)
	c.Check(err, check.Equals, ErrRestartSocket)
	c.Check(d.restartSocket, check.Equals, true)
}

func (s *daemonSuite) TestRestartIntoSocketModePendingChanges(c *check.C) {
	restore := standby.MockStandbyWait(5 * time.Millisecond)
	defer restore()

	d := s.newTestDaemon(c)
	makeDaemonListeners(c, d)

	// mark as already seeded, we also have no snaps so this will
	// go into socket activation mode
	s.markSeeded(d)
	st := d.overlord.State()

	c.Assert(d.Start(context.Background()), check.IsNil)
	// pretend some ensure happened
	for i := 0; i < 5; i++ {
		c.Check(d.overlord.StateEngine().Ensure(), check.IsNil)
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-d.Dying():
		// Pretend we got change while shutting down, this can
		// happen when e.g. the user requested a `snap install
		// foo` at the same time as the code in the overlord
		// checked that it can go into socket activated
		// mode. I.e. the daemon was processing the request
		// but no change was generated at the time yet.
		st.Lock()
		chg := st.NewChange("fake-install", "fake install some snap")
		chg.AddTask(st.NewTask("fake-install-task", "fake install task"))
		chgStatus := chg.Status()
		st.Unlock()
		// ensure our change is valid and ready
		c.Check(chgStatus, check.Equals, state.DoStatus)
	case <-time.After(5 * time.Second):
		c.Errorf("daemon did not stop after 5s")
	}
	// when the daemon got a pending change it just restarts
	err := d.Stop(nil)
	c.Check(err, check.IsNil)
	c.Check(d.restartSocket, check.Equals, false)
}

func (s *daemonSuite) TestConnTrackerCanShutdown(c *check.C) {
	ct := &connTracker{conns: make(map[net.Conn]struct{})}
	c.Check(ct.CanStandby(), check.Equals, true)

	con := &net.IPConn{}
	ct.trackConn(con, http.StateActive)
	c.Check(ct.CanStandby(), check.Equals, false)

	ct.trackConn(con, http.StateIdle)
	c.Check(ct.CanStandby(), check.Equals, true)
}

func doTestReq(c *check.C, cmd *Command, mth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(mth, "/", nil)
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	return rec
}

func (s *daemonSuite) TestDegradedModeReply(c *check.C) {
	d := s.newTestDaemon(c)
	cmd := &Command{d: d}
	cmd.GET = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.POST = func(*Command, *http.Request, *auth.UserState) Response {
		return SyncResponse(nil)
	}
	cmd.ReadAccess = authenticatedAccess{}
	cmd.WriteAccess = authenticatedAccess{}

	// pretend we are in degraded mode
	d.SetDegradedMode(fmt.Errorf("foo error"))

	// GET is ok even in degraded mode
	rec := doTestReq(c, cmd, "GET")
	c.Check(rec.Code, check.Equals, 200)
	// POST is not allowed
	rec = doTestReq(c, cmd, "POST")
	c.Check(rec.Code, check.Equals, 500)
	// verify we get the error
	var v struct{ Result errorResult }
	c.Assert(json.NewDecoder(rec.Body).Decode(&v), check.IsNil)
	c.Check(v.Result.Message, check.Equals, "foo error")

	// clean degraded mode
	d.SetDegradedMode(nil)
	rec = doTestReq(c, cmd, "POST")
	c.Check(rec.Code, check.Equals, 200)
}

func (s *daemonSuite) TestHandleUnexpectedRestart(c *check.C) {
	os.Setenv("SNAPD_REVERT_TO_REV", "999")
	defer os.Unsetenv("SNAPD_REVERT_TO_REV")

	d := s.newTestDaemon(c)

	// mark as already seeded
	s.markSeeded(d)

	c.Assert(d.Start(context.Background()), check.Equals, ErrNoFailureRecoveryNeeded)
}

func clientForSnapdSocket() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				return net.Dial("unix", dirs.SnapdSocket)
			},
		},
	}
}

func (s *daemonSuite) TestNoticesRequestCanceledOnStop(c *check.C) {
	d, err := New()
	c.Assert(err, check.IsNil)
	// don't talk to the store, needs to be called after daemon.New()
	snapstate.CanAutoRefresh = nil

	// mark as already seeded
	s.markSeeded(d)

	c.Assert(d.Init(), check.IsNil)

	client := clientForSnapdSocket()

	req, err := http.NewRequest("GET", "http://localhost/v2/notices?timeout=10s", nil)
	c.Assert(err, check.IsNil)

	gotReqC := make(chan struct{})
	d.router.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.Assert(r.URL.String(), check.Equals, "/v2/notices?timeout=10s")
			close(gotReqC)
			h.ServeHTTP(w, r)
		})
	})

	c.Assert(d.Start(context.Background()), check.IsNil)

	var resp map[string]any
	clientC := make(chan struct{})
	go func() {
		// this will block until we call stop
		r, err := client.Do(req)
		if r != nil {
			defer r.Body.Close()
		}
		c.Check(err, check.IsNil)
		err = json.NewDecoder(r.Body).Decode(&resp)
		c.Assert(err, check.IsNil)
		close(clientC)
	}()

	<-gotReqC
	d.Stop(nil)
	<-clientC
	c.Check(resp, check.DeepEquals, map[string]any{
		"status-code": 500.,
		"status":      "Internal Server Error",
		"result": map[string]any{
			"message": "request canceled",
		},
		"type": "error"})
}

func (s *daemonSuite) TestRequestBodyPolicy(c *check.C) {
	for _, tc := range []struct {
		method, path, ct     string
		wantRead, wantAction bool
	}{
		{ct: "", wantRead: true, wantAction: true},
		{ct: "application/json", wantRead: true, wantAction: true},
		{ct: "APPLICATION/JSON", wantRead: true, wantAction: true},
		{ct: "Application/Json", wantRead: true, wantAction: true},
		{ct: "application/json; charset=utf-8", wantRead: true, wantAction: true},
		{ct: "application/json; charset=windows-1252", wantRead: true, wantAction: true},
		{ct: "application/json; boundary=foo", wantRead: true, wantAction: true},
		{ct: "text/plain; charset", wantRead: true},
		// Unparseable types are read (and rejected if oversize), not streamed.
		{ct: "application/json; charset=utf-8; charset=utf-16", wantRead: true},
		{ct: "application/json extra", wantRead: true},
		{ct: "application/", wantRead: true},
		{ct: "application", wantRead: true},
		{ct: "; charset=utf-8", wantRead: true},
		{ct: "text/plain", wantRead: true},
		{ct: "application/json-patch+json", wantRead: true},
		{ct: "notvalid", wantRead: true},
		{ct: "multipart/form-data"},
		{ct: "multipart/mixed"},
		{ct: client.SnapshotExportMediaType},
		{ct: asserts.MediaType},
		{method: "PUT", ct: "application/json", wantRead: true},
		{method: "PUT", ct: "", wantRead: true},
		{method: "PUT", path: "/v2/assertions"},
		{method: "GET", ct: "application/json"},
		{method: "DELETE", ct: "application/json"},
		{method: "PATCH", ct: "application/json"},
		{path: "/v2/assertions"},
		{path: "/v2/assertions/"},
		// The path exception is only for an absent Content-Type.
		{path: "/v2/assertions", ct: "application/json", wantRead: true, wantAction: true},
	} {
		method := tc.method
		if method == "" {
			method = "POST"
		}
		path := tc.path
		if path == "" {
			path = "/"
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		if tc.ct != "" {
			req.Header.Set("Content-Type", tc.ct)
		}
		read, wantAction := requestBodyPolicy(req)
		cmt := check.Commentf("method=%s path=%s ct=%q", method, path, tc.ct)
		c.Check(read, check.Equals, tc.wantRead, cmt)
		c.Check(wantAction, check.Equals, tc.wantAction, cmt)
	}
}

func (s *daemonSuite) TestReadRequestBody(c *check.C) {
	type testCase struct {
		name          string
		method        string
		path          string
		body          string
		contentType   string // "" sets application/json; "none" omits the header
		contentLength int64
		wantAction    string
		wantErr       string
		wantSentinel  error
	}

	head := `{"action":"ok","pad":"`
	tail := `"}`
	atLimit := head + strings.Repeat("x", maxBodySize-len(head)-len(tail)) + tail
	c.Assert(len(atLimit), check.Equals, maxBodySize)

	for _, tc := range []testCase{
		{
			name:       "extracts action",
			body:       `{"snaps":["x"],"action":"refresh"}`,
			wantAction: "refresh",
		},
		{
			name:       "action absent",
			body:       `{"snaps":["x"]}`,
			wantAction: "",
		},
		{
			name:       "empty action",
			body:       `{"action":""}`,
			wantAction: "",
		},
		{
			name:          "content length ignored",
			body:          `{"action":"install","snaps":["x"]}`,
			contentLength: 5,
			wantAction:    "install",
		},
		{
			name:          "content length unknown",
			body:          `{"action":"install","snaps":["x"]}`,
			contentLength: -1,
			wantAction:    "install",
		},
		{
			name:    "non-object top-level",
			body:    `[{"action":"install"}]`,
			wantErr: "json: cannot unmarshal array",
		},
		{
			name:    "malformed json",
			body:    `{not json`,
			wantErr: "invalid character",
		},
		{
			name:         "data after json",
			body:         `{"action":"install"} trailing`,
			wantErr:      "unexpected data after request body",
			wantSentinel: errUnexpectedDataAfterBody,
		},
		{
			name:         "data after json starting with brace",
			body:         `{"action":"install"} }morestuff`,
			wantErr:      "unexpected data after request body",
			wantSentinel: errUnexpectedDataAfterBody,
		},
		{
			name:         "data after json starting with bracket",
			body:         `{"action":"install"} ]morestuff`,
			wantErr:      "unexpected data after request body",
			wantSentinel: errUnexpectedDataAfterBody,
		},
		{
			name:          "declared content-length oversize",
			body:          `{"action":"install"}`,
			contentLength: maxBodySize + 1,
			wantErr:       "body size limit exceeded",
			wantSentinel:  errBodyTooLarge,
		},
		{
			name:         "body size limit exceeded",
			body:         strings.Repeat("x", maxBodySize+1),
			wantErr:      "body size limit exceeded",
			wantSentinel: errBodyTooLarge,
		},
		{
			name:          "oversize with unknown content-length",
			body:          strings.Repeat("x", maxBodySize+1),
			contentLength: -1,
			wantErr:       "body size limit exceeded",
			wantSentinel:  errBodyTooLarge,
		},
		{
			name:       "body at size limit still parsed",
			body:       atLimit,
			wantAction: "ok",
		},
		{
			name:         "empty body",
			body:         "",
			wantErr:      "empty request body",
			wantSentinel: errEmptyBody,
		},
		{
			name:         "whitespace-only body",
			body:         " \n\t",
			wantErr:      "EOF",
			wantSentinel: io.EOF,
		},
		{
			name:        "multipart skipped",
			body:        `{"action":"install"}`,
			contentType: "multipart/form-data",
		},
		{
			name:         "malformed parameter still size limited",
			body:         strings.Repeat("x", maxBodySize+1),
			contentType:  "application/json; charset",
			wantErr:      "body size limit exceeded",
			wantSentinel: errBodyTooLarge,
		},
		{
			name:        "text body read but not parsed",
			body:        `{"action":"install"}`,
			contentType: "text/plain",
			wantAction:  "",
		},
		{
			name:         "text body size limited",
			body:         strings.Repeat("x", maxBodySize+1),
			contentType:  "text/plain",
			wantErr:      "body size limit exceeded",
			wantSentinel: errBodyTooLarge,
		},
		{
			name:         "unknown media type size limited",
			body:         strings.Repeat("x", maxBodySize+1),
			contentType:  "application/octet-stream",
			wantErr:      "body size limit exceeded",
			wantSentinel: errBodyTooLarge,
		},
		{
			// An unidentifiable Content-Type must not buy an exemption.
			name:         "unparseable media type size limited",
			body:         strings.Repeat("x", maxBodySize+1),
			contentType:  "application/",
			wantErr:      "body size limit exceeded",
			wantSentinel: errBodyTooLarge,
		},
		{
			name:        "labelled assertions skipped",
			path:        "/v2/assertions",
			body:        strings.Repeat("x", maxBodySize+1),
			contentType: asserts.MediaType,
		},
		{
			name:        "snapshot archive skipped",
			path:        "/v2/snapshots",
			body:        strings.Repeat("x", maxBodySize+1),
			contentType: client.SnapshotExportMediaType,
		},
		{
			name:       "put not parsed",
			method:     "PUT",
			body:       `{"action":"delegate"}`,
			wantAction: "",
		},
		{
			name:         "put still size limited",
			method:       "PUT",
			body:         strings.Repeat("x", maxBodySize+1),
			wantErr:      "body size limit exceeded",
			wantSentinel: errBodyTooLarge,
		},
		{
			name:       "put malformed json not parsed",
			method:     "PUT",
			body:       `{not json`,
			wantAction: "",
		},
		{
			name:        "empty content-type still json",
			path:        "/v2/warnings",
			body:        `{"action":"okay"}`,
			contentType: "none",
			wantAction:  "okay",
		},
		{
			name:        "assertions empty content-type skipped",
			path:        "/v2/assertions",
			body:        strings.Repeat("x", maxBodySize+1),
			contentType: "none",
		},
		{
			name:        "assertions trailing slash empty content-type skipped",
			path:        "/v2/assertions/",
			body:        "type: account\n",
			contentType: "none",
		},
	} {
		method := tc.method
		if method == "" {
			method = "POST"
		}
		path := tc.path
		if path == "" {
			path = "/"
		}
		req := httptest.NewRequest(method, path, strings.NewReader(tc.body))
		switch tc.contentType {
		case "none":
		case "":
			req.Header.Set("Content-Type", "application/json")
		default:
			req.Header.Set("Content-Type", tc.contentType)
		}
		if tc.contentLength != 0 {
			req.ContentLength = tc.contentLength
		}

		got, err := readRequestBody(req)
		cmt := check.Commentf("case: %s", tc.name)
		if tc.wantErr != "" {
			c.Assert(err, check.ErrorMatches, tc.wantErr+".*", cmt)
		} else {
			c.Assert(err, check.IsNil, cmt)
			c.Check(got, check.Equals, tc.wantAction, cmt)
		}

		if tc.wantSentinel != errBodyTooLarge || tc.contentLength > maxBodySize {
			gotBody, readErr := io.ReadAll(req.Body)
			c.Assert(readErr, check.IsNil, cmt)
			c.Check(string(gotBody), check.Equals, tc.body, cmt)
		}
		if tc.wantSentinel != nil {
			c.Check(err, check.Equals, tc.wantSentinel, cmt)
		}

	}

	for _, method := range []string{"GET", "DELETE", "PATCH"} {
		cmt := check.Commentf("method: %s", method)
		req := httptest.NewRequest(method, "/", strings.NewReader(`{"action":"install"}`))
		got, err := readRequestBody(req)
		c.Assert(err, check.IsNil, cmt)
		c.Check(got, check.Equals, "", cmt)
	}
}

func (s *daemonSuite) TestReadRequestBodyReadError(c *check.C) {
	prefix := []byte(`{"action":"install"`)
	simulatedErr := errors.New("simulated transient read error")
	// Prefix then a persistent read error, as when the client disconnects.
	req := httptest.NewRequest("POST", "/", http.NoBody)
	req.Body = io.NopCloser(io.MultiReader(
		bytes.NewReader(prefix),
		iotest.ErrReader(simulatedErr),
	))
	req.Header.Set("Content-Type", "application/json")

	action, err := readRequestBody(req)
	c.Check(action, check.Equals, "")
	c.Assert(err, check.NotNil)
	c.Check(err, check.ErrorMatches, "cannot read request body: "+simulatedErr.Error())
	c.Check(errors.Is(err, errBodyUnreadable), check.Equals, true)
	c.Check(isBodyUnusable(err), check.Equals, true)
}

func (s *daemonSuite) TestRequestActionContext(c *check.C) {
	for _, tc := range []struct {
		name   string
		action string
		err    error
	}{
		{name: "action", action: "install"},
		{name: "no action field", action: ""},
		{name: "empty body", err: errEmptyBody},
		{name: "oversize", err: errBodyTooLarge},
		{name: "decode failure", err: errUnexpectedDataAfterBody},
	} {
		cmt := check.Commentf("case: %s", tc.name)
		ctx := withRequestAction(context.Background(), tc.action, tc.err)
		action, err := requestActionFromContext(ctx)
		c.Check(action, check.Equals, tc.action, cmt)
		c.Check(err, check.Equals, tc.err, cmt)
	}
}

func (s *daemonSuite) TestRequestActionFromContextMissing(c *check.C) {
	action, err := requestActionFromContext(context.Background())
	c.Check(action, check.Equals, "")
	c.Check(err, check.Equals, errNoRequestAction)
	c.Check(err, check.ErrorMatches, "internal error: request action not in context")
}

// recordingTraceLogger records logger.Trace calls. Log.Trace is a no-op
// unless structured JSON logging is enabled, so tests that care about
// SNAPD_TRACE output install this instead of logger.MockLogger.
type recordingTraceLogger struct {
	logger.Logger
	calls []traceCall
}

type traceCall struct {
	msg   string
	attrs []any
}

func (l *recordingTraceLogger) Trace(msg string, attrs ...any) {
	l.calls = append(l.calls, traceCall{msg: msg, attrs: append([]any(nil), attrs...)})
}

// withLogger installs l as the global logger. MockLogger is used only to
// restore the previous logger when the returned function is called.
func withLogger(l logger.Logger) (restore func()) {
	_, restore = logger.MockLogger()
	logger.SetLogger(l)
	return restore
}

func (s *daemonSuite) TestTraceSnapdAPI(c *check.C) {
	rec := &recordingTraceLogger{Logger: logger.NullLogger}
	defer withLogger(rec)()

	cmd := &Command{Path: "/v2/test"}
	simulatedErr := errors.New("simulated transient read error")

	// ServeHTTP cases share one daemon: a second times out on the state file lock.
	d := s.newTestDaemon(c)

	for _, tc := range []struct {
		name         string
		trace        bool
		method       string
		path         string
		contentType  string // "" sets application/json; "none" omits the header
		body         string
		customBody   io.ReadCloser
		serveHTTP    bool
		wantMsgs     []string
		wantAction   string
		wantBodyRead string
		wantStatus   int
	}{
		{
			name:     "trace disabled",
			method:   "POST",
			body:     `{"action":"install"}`,
			wantMsgs: nil,
		},
		{
			name:     "get skipped",
			trace:    true,
			method:   "GET",
			body:     `{"action":"install"}`,
			wantMsgs: []string{"endpoint"},
		},
		{
			name:     "delete skipped",
			trace:    true,
			method:   "DELETE",
			body:     `{"action":"install"}`,
			wantMsgs: []string{"endpoint"},
		},
		{
			name:     "patch skipped",
			trace:    true,
			method:   "PATCH",
			body:     `{"action":"install"}`,
			wantMsgs: []string{"endpoint"},
		},
		{
			name:        "multipart skipped",
			trace:       true,
			method:      "POST",
			contentType: "multipart/form-data",
			body:        `{"action":"install"}`,
			wantMsgs:    []string{"endpoint"},
		},
		{
			name:        "snapshot skipped",
			trace:       true,
			method:      "POST",
			contentType: client.SnapshotExportMediaType,
			body:        `{"action":"install"}`,
			wantMsgs:    []string{"endpoint"},
		},
		{
			name:        "assertions unlabeled skipped",
			trace:       true,
			method:      "POST",
			path:        "/v2/assertions",
			contentType: "none",
			body:        "type: account\n",
			wantMsgs:    []string{"endpoint"},
		},
		{
			name:       "post extracts action",
			trace:      true,
			method:     "POST",
			body:       `{"action":"install"}`,
			wantMsgs:   []string{"endpoint"},
			wantAction: "install",
		},
		{
			name:     "put action not traced",
			trace:    true,
			method:   "PUT",
			body:     `{"action":"delegate"}`,
			wantMsgs: []string{"endpoint"},
		},
		{
			name:        "charset json extracts action",
			trace:       true,
			method:      "POST",
			contentType: "application/json; charset=utf-8",
			body:        `{"action":"okay"}`,
			wantMsgs:    []string{"endpoint"},
			wantAction:  "okay",
		},
		{
			name:        "empty content-type extracts action",
			trace:       true,
			method:      "POST",
			path:        "/v2/warnings",
			contentType: "none",
			body:        `{"action":"okay"}`,
			wantMsgs:    []string{"endpoint"},
			wantAction:  "okay",
		},
		{
			name:     "action absent",
			trace:    true,
			method:   "POST",
			body:     `{"snaps":["x"]}`,
			wantMsgs: []string{"endpoint"},
		},
		{
			name:     "empty action",
			trace:    true,
			method:   "POST",
			body:     `{"action":""}`,
			wantMsgs: []string{"endpoint"},
		},
		{
			name:     "empty body",
			trace:    true,
			method:   "POST",
			body:     "",
			wantMsgs: []string{"endpoint"},
		},
		{
			name:         "whitespace-only",
			trace:        true,
			method:       "POST",
			body:         " \n\t",
			wantMsgs:     []string{"endpoint-error", "endpoint"},
			wantBodyRead: "EOF",
		},
		{
			name:         "malformed json",
			trace:        true,
			method:       "POST",
			body:         `{not json`,
			wantMsgs:     []string{"endpoint-error", "endpoint"},
			wantBodyRead: "invalid character",
		},
		{
			name:         "non-object json",
			trace:        true,
			method:       "POST",
			body:         `[{"action":"install"}]`,
			wantMsgs:     []string{"endpoint-error", "endpoint"},
			wantBodyRead: "cannot unmarshal",
		},
		{
			name:         "trailing data",
			trace:        true,
			method:       "POST",
			body:         `{"action":"install"} trailing`,
			wantMsgs:     []string{"endpoint-error", "endpoint"},
			wantBodyRead: "unexpected data after request body",
		},
		{
			name:   "read error rejected before trace",
			trace:  true,
			method: "POST",
			customBody: io.NopCloser(io.MultiReader(
				bytes.NewReader([]byte(`{"action":"install"`)),
				iotest.ErrReader(simulatedErr),
			)),
			serveHTTP:  true,
			wantMsgs:   nil,
			wantStatus: 400,
		},
		{
			name:       "oversize rejected before trace",
			trace:      true,
			method:     "POST",
			body:       strings.Repeat("x", maxBodySize+1),
			serveHTTP:  true,
			wantMsgs:   nil,
			wantStatus: 400,
		},
	} {
		rec.calls = nil
		if tc.trace {
			os.Setenv("SNAPD_TRACE", "1")
		} else {
			os.Unsetenv("SNAPD_TRACE")
		}

		method := tc.method
		if method == "" {
			method = "POST"
		}
		path := tc.path
		if path == "" {
			path = "/"
		}
		var req *http.Request
		if tc.customBody != nil {
			req = httptest.NewRequest(method, path, http.NoBody)
			req.Body = tc.customBody
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(tc.body))
		}
		switch tc.contentType {
		case "none":
		case "":
			req.Header.Set("Content-Type", "application/json")
		default:
			req.Header.Set("Content-Type", tc.contentType)
		}

		cmt := check.Commentf("case: %s", tc.name)
		if tc.serveHTTP {
			cmdHTTP := &Command{d: d, Path: path}
			cmdHTTP.POST = func(*Command, *http.Request, *auth.UserState) Response {
				c.Fatalf("handler must not run for %s", tc.name)
				return SyncResponse(nil)
			}
			cmdHTTP.WriteAccess = openAccess{}
			req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
			httprec := httptest.NewRecorder()
			cmdHTTP.ServeHTTP(httprec, req)
			c.Check(httprec.Code, check.Equals, tc.wantStatus, cmt)
		} else {
			// traceSnapdAPI reads the cached action, so skip ServeHTTP
			// only after the body has been read and cached.
			action, err := readRequestBody(req)
			c.Assert(isBodyUnusable(err), check.Equals, false, cmt)
			req = req.WithContext(withRequestAction(req.Context(), action, err))
			traceSnapdAPI(cmd, req)
		}

		var gotMsgs []string
		var gotAction string
		var gotBodyRead string
		for _, call := range rec.calls {
			gotMsgs = append(gotMsgs, call.msg)
			for i := 0; i+1 < len(call.attrs); i += 2 {
				key, _ := call.attrs[i].(string)
				switch key {
				case "action":
					gotAction, _ = call.attrs[i+1].(string)
				case "body-read":
					gotBodyRead = fmt.Sprint(call.attrs[i+1])
				}
			}
		}
		c.Check(gotMsgs, check.DeepEquals, tc.wantMsgs, cmt)
		c.Check(gotAction, check.Equals, tc.wantAction, cmt)
		if tc.wantBodyRead != "" {
			c.Check(gotBodyRead, testutil.Contains, tc.wantBodyRead, cmt)
		} else {
			c.Check(gotBodyRead, check.Equals, "", cmt)
		}
	}
	os.Unsetenv("SNAPD_TRACE")
}

func (s *daemonSuite) TestServeHTTPTraceExtractsActionPreservesBody(c *check.C) {
	os.Setenv("SNAPD_TRACE", "1")
	defer os.Unsetenv("SNAPD_TRACE")

	d := s.newTestDaemon(c)
	body := `{"action":"install","snaps":["x"]}`

	var gotBody string
	cmd := &Command{d: d, Path: "/v2/test"}
	cmd.POST = func(_ *Command, r *http.Request, _ *auth.UserState) Response {
		b, err := io.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		gotBody = string(b)
		return SyncResponse(nil)
	}
	cmd.WriteAccess = openAccess{}

	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)

	c.Check(rec.Code, check.Equals, 200)
	c.Check(gotBody, check.Equals, body)
}

func (s *daemonSuite) TestServeHTTPOversizeBodyRejected(c *check.C) {
	d := s.newTestDaemon(c)
	defer os.Unsetenv("SNAPD_TRACE")
	for _, trace := range []bool{false, true} {
		if trace {
			os.Setenv("SNAPD_TRACE", "1")
		} else {
			os.Unsetenv("SNAPD_TRACE")
		}

		body := strings.Repeat("x", maxBodySize+128)
		handlerCalled := false
		cmd := &Command{d: d, Path: "/v2/test"}
		cmd.POST = func(_ *Command, r *http.Request, _ *auth.UserState) Response {
			handlerCalled = true
			return SyncResponse(nil)
		}
		cmd.WriteAccess = openAccess{}

		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
		rec := httptest.NewRecorder()
		cmd.ServeHTTP(rec, req)

		c.Check(rec.Code, check.Equals, 400, check.Commentf("trace=%v", trace))
		c.Check(handlerCalled, check.Equals, false, check.Commentf("trace=%v", trace))
		c.Check(rec.Body.String(), check.Matches, `(?s).*body size limit exceeded.*`, check.Commentf("trace=%v", trace))
	}
}

func (s *daemonSuite) TestServeHTTPUnreadableBodyRejected(c *check.C) {
	d := s.newTestDaemon(c)
	simulatedErr := errors.New("simulated transient read error")

	handlerCalled := false
	cmd := &Command{d: d, Path: "/v2/test"}
	cmd.POST = func(_ *Command, r *http.Request, _ *auth.UserState) Response {
		handlerCalled = true
		return SyncResponse(nil)
	}
	cmd.WriteAccess = openAccess{}

	req := httptest.NewRequest("POST", "/", http.NoBody)
	req.Body = io.NopCloser(io.MultiReader(
		strings.NewReader(`{"action":"install"`),
		iotest.ErrReader(simulatedErr),
	))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)

	c.Check(rec.Code, check.Equals, 400)
	c.Check(handlerCalled, check.Equals, false)
	c.Check(rec.Body.String(), check.Matches, `(?s).*cannot read request body: `+simulatedErr.Error()+`.*`)
}

func (s *daemonSuite) TestServeHTTPSkippedOversizeBodyStillServed(c *check.C) {
	d := s.newTestDaemon(c)
	body := strings.Repeat("x", maxBodySize+128)
	for _, tc := range []struct {
		path        string
		contentType string
	}{
		{"/v2/assertions", ""},
		{"/v2/snaps", "multipart/form-data; boundary=x"},
		{"/v2/snapshots", client.SnapshotExportMediaType},
	} {
		var gotLen int
		cmd := &Command{d: d, Path: tc.path}
		cmd.POST = func(_ *Command, r *http.Request, _ *auth.UserState) Response {
			b, err := io.ReadAll(r.Body)
			c.Assert(err, check.IsNil)
			gotLen = len(b)
			return SyncResponse(nil)
		}
		cmd.WriteAccess = openAccess{}

		req := httptest.NewRequest("POST", tc.path, strings.NewReader(body))
		if tc.contentType != "" {
			req.Header.Set("Content-Type", tc.contentType)
		}
		req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
		rec := httptest.NewRecorder()
		cmd.ServeHTTP(rec, req)

		c.Check(rec.Code, check.Equals, 200, check.Commentf("%s %s", tc.path, tc.contentType))
		c.Check(gotLen, check.Equals, len(body), check.Commentf("%s %s", tc.path, tc.contentType))
	}
}

func (s *daemonSuite) TestServeHTTPTraceInvalidJSONStillServed(c *check.C) {
	os.Setenv("SNAPD_TRACE", "1")
	defer os.Unsetenv("SNAPD_TRACE")

	d := s.newTestDaemon(c)
	body := `{not json`

	var gotBody string
	cmd := &Command{d: d, Path: "/v2/test"}
	cmd.POST = func(_ *Command, r *http.Request, _ *auth.UserState) Response {
		b, err := io.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		gotBody = string(b)
		return SyncResponse(nil)
	}
	cmd.WriteAccess = openAccess{}

	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = fmt.Sprintf("pid=100;uid=0;socket=%s;", dirs.SnapdSocket)
	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)

	c.Check(rec.Code, check.Equals, 200)
	c.Check(gotBody, check.Equals, body)
}
