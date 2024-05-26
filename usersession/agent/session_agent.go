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

package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/godbus/dbus"
	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/desktop/notification"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/systemd"
)

type SessionAgent struct {
	Version         string
	bus             *dbus.Conn
	listener        net.Listener
	serve           *http.Server
	tomb            tomb.Tomb
	router          *mux.Router
	notificationMgr notification.NotificationManager

	idle        *idleTracker
	IdleTimeout time.Duration
}

const sessionAgentBusName = "io.snapcraft.SessionAgent"

// A ResponseFunc handles one of the individual verbs for a method
type ResponseFunc func(*Command, *http.Request) Response

// A Command routes a request to an individual per-verb ResponseFunc
type Command struct {
	Path string

	GET    ResponseFunc
	PUT    ResponseFunc
	POST   ResponseFunc
	DELETE ResponseFunc

	s *SessionAgent
}

func (c *Command) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var rspf ResponseFunc
	rsp := MethodNotAllowed("method %q not allowed", r.Method)

	switch r.Method {
	case "GET":
		rspf = c.GET
	case "PUT":
		rspf = c.PUT
	case "POST":
		rspf = c.POST
	case "DELETE":
		rspf = c.DELETE
	}

	if rspf != nil {
		rsp = rspf(c, r)
	}
	rsp.ServeHTTP(w, r)
}

type idleTracker struct {
	mu         sync.Mutex
	active     map[net.Conn]struct{}
	lastActive time.Time
}

var sysGetsockoptUcred = syscall.GetsockoptUcred

func getUcred(conn net.Conn) (*syscall.Ucred, error) {
	if uconn, ok := conn.(*net.UnixConn); ok {
		f := mylog.Check2(uconn.File())

		defer f.Close()
		return sysGetsockoptUcred(int(f.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}
	return nil, fmt.Errorf("expected a net.UnixConn, but got a %T", conn)
}

func (it *idleTracker) trackConn(conn net.Conn, state http.ConnState) {
	// Perform peer credentials check
	if state == http.StateNew {
		ucred := mylog.Check2(getUcred(conn))

		if ucred.Uid != 0 && ucred.Uid != uint32(sys.Geteuid()) {
			logger.Noticef("Blocking request from user ID %v", ucred.Uid)
			conn.Close()
			return
		}
	}

	it.mu.Lock()
	defer it.mu.Unlock()
	oldActive := len(it.active)
	if state == http.StateNew || state == http.StateActive {
		it.active[conn] = struct{}{}
	} else {
		delete(it.active, conn)
	}
	if len(it.active) == 0 && oldActive != 0 {
		it.lastActive = time.Now()
	}
}

// idleDuration returns the duration of time the server has been idle
func (it *idleTracker) idleDuration() time.Duration {
	it.mu.Lock()
	defer it.mu.Unlock()
	if len(it.active) != 0 {
		return 0
	}
	return time.Since(it.lastActive)
}

const (
	defaultIdleTimeout = 30 * time.Second
	shutdownTimeout    = 5 * time.Second
)

type closeOnceListener struct {
	net.Listener

	idempotClose sync.Once
	closeErr     error
}

func (l *closeOnceListener) Close() error {
	l.idempotClose.Do(func() {
		l.closeErr = l.Listener.Close()
	})
	return l.closeErr
}

func (s *SessionAgent) Init() error {
	mylog.Check(
		// Set up D-Bus connection
		s.tryConnectSessionBus())

	// Set up REST API server
	listenerMap := mylog.Check2(netutil.ActivationListeners())

	// Set up notification manager
	// Note that session bus may be nil, see the comment in tryConnectSessionBus.
	if s.bus != nil {
		s.notificationMgr = notification.NewNotificationManager(s.bus, "io.snapcraft.SessionAgent")
	}

	agentSocket := fmt.Sprintf("%s/%d/snapd-session-agent.socket", dirs.XdgRuntimeDirBase, os.Getuid())
	if l := mylog.Check2(netutil.GetListener(agentSocket, listenerMap)); err != nil {
		return fmt.Errorf("cannot listen on socket %s: %v", agentSocket, err)
	} else {
		s.listener = &closeOnceListener{Listener: l}
	}
	s.idle = &idleTracker{
		active:     make(map[net.Conn]struct{}),
		lastActive: time.Now(),
	}
	s.IdleTimeout = defaultIdleTimeout
	s.addRoutes()
	s.serve = &http.Server{
		Handler:   s.router,
		ConnState: s.idle.trackConn,
	}
	return nil
}

func (s *SessionAgent) tryConnectSessionBus() (err error) {
	s.bus = mylog.Check2(dbusutil.SessionBusPrivate())

	// ssh sessions on Ubuntu 16.04 may have a user
	// instance of systemd but no D-Bus session bus.  So
	// don't treat this as an error.

	defer func() {
	}()

	reply := mylog.Check2(s.bus.RequestName(sessionAgentBusName, dbus.NameFlagDoNotQueue))

	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("cannot obtain bus name %q: %v", sessionAgentBusName, reply)
	}
	return nil
}

func (s *SessionAgent) addRoutes() {
	s.router = mux.NewRouter()
	for _, c := range restApi {
		c.s = s
		s.router.Handle(c.Path, c).Name(c.Path)
	}
	s.router.NotFoundHandler = NotFound("not found")
}

func (s *SessionAgent) Start() {
	s.tomb.Go(s.runServer)
	s.tomb.Go(s.shutdownServerOnKill)
	s.tomb.Go(s.exitOnIdle)
	if s.notificationMgr != nil {
		s.tomb.Go(s.handleNotifications)
	}
	systemd.SdNotify("READY=1")
}

func (s *SessionAgent) runServer() error {
	mylog.Check(s.serve.Serve(s.listener))
	if err == http.ErrServerClosed {
		err = nil
	}
	if s.tomb.Err() == tomb.ErrStillAlive {
		return err
	}
	return nil
}

func (s *SessionAgent) shutdownServerOnKill() error {
	<-s.tomb.Dying()
	systemd.SdNotify("STOPPING=1")
	// closing the listener (but then it needs wrapping in
	// closeOnceListener) before actually calling Shutdown, to
	// workaround https://github.com/golang/go/issues/20239, we
	// can in some cases (e.g. tests) end up calling Shutdown
	// before runServer calls Serve and in go <1.11 this can be
	// racy and the shutdown blocks.
	// Historically We do something similar in the main daemon
	// logic as well.
	s.listener.Close()
	// Note that session bus may be nil, see the comment in tryConnectSessionBus.
	if s.bus != nil {
		s.bus.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.serve.Shutdown(ctx)
}

func (s *SessionAgent) exitOnIdle() error {
	timer := time.NewTimer(s.IdleTimeout)
Loop:
	for {
		select {
		case <-s.tomb.Dying():
			break Loop
		case <-timer.C:
			// Have we been idle? Consult idle duration from connection tracker
			// and from notification manager, pick the lower one.
			idleDuration := s.idle.idleDuration()
			// notificationMgr may be nil if session bus is not available
			if s.notificationMgr != nil {
				if dur := s.notificationMgr.IdleDuration(); dur < idleDuration {
					idleDuration = dur
				}
			}
			if idleDuration >= s.IdleTimeout {
				s.tomb.Kill(nil)
				break Loop
			} else {
				timer.Reset(s.IdleTimeout - idleDuration)
			}
		}
	}
	return nil
}

// handleNotifications handles notifications in a blocking manner.
// This should only be called when notificationMgr is available (i.e. s.bus is set).
func (s *SessionAgent) handleNotifications() error {
	mylog.Check(s.notificationMgr.HandleNotifications(s.tomb.Context(context.Background())))

	return nil
}

// Stop performs a graceful shutdown of the session agent and waits up to 5
// seconds for it to complete.
func (s *SessionAgent) Stop() error {
	if s.bus != nil {
		_ := mylog.Check2(s.bus.ReleaseName(sessionAgentBusName))
	}
	s.tomb.Kill(nil)
	return s.tomb.Wait()
}

func (s *SessionAgent) Dying() <-chan struct{} {
	return s.tomb.Dying()
}

func New() (*SessionAgent, error) {
	agent := &SessionAgent{}
	mylog.Check(agent.Init())

	return agent, nil
}
