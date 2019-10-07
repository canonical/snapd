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

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/systemd"
)

type SessionAgent struct {
	Version  string
	listener net.Listener
	serve    *http.Server
	tomb     tomb.Tomb
	router   *mux.Router

	idle        *idleTracker
	IdleTimeout time.Duration
}

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
	var rsp = MethodNotAllowed("method %q not allowed", r.Method)

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
		f, err := uconn.File()
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return sysGetsockoptUcred(int(f.Fd()), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}
	return nil, fmt.Errorf("expected a net.UnixConn, but got a %T", conn)
}

func (it *idleTracker) trackConn(conn net.Conn, state http.ConnState) {
	// Perform peer credentials check
	if state == http.StateNew {
		ucred, err := getUcred(conn)
		if err != nil {
			logger.Noticef("Failed to retrieve peer credentials: %v", err)
			conn.Close()
			return
		}
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
	return time.Now().Sub(it.lastActive)
}

const (
	defaultIdleTimeout = 30 * time.Second
	shutdownTimeout    = 5 * time.Second
)

func (s *SessionAgent) Init() error {
	listenerMap, err := netutil.ActivationListeners()
	if err != nil {
		return err
	}
	agentSocket := fmt.Sprintf("%s/%d/snapd-session-agent.socket", dirs.XdgRuntimeDirBase, os.Getuid())
	if s.listener, err = netutil.GetListener(agentSocket, listenerMap); err != nil {
		return fmt.Errorf("cannot listen on socket %s: %v", agentSocket, err)
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

func (s *SessionAgent) addRoutes() {
	s.router = mux.NewRouter()
	for _, c := range restApi {
		c.s = s
		s.router.Handle(c.Path, c).Name(c.Path)
	}
	s.router.NotFoundHandler = NotFound("not found")
}

func (s *SessionAgent) Start() {
	s.tomb.Go(func() error {
		err := s.serve.Serve(s.listener)
		if err == http.ErrServerClosed {
			return nil
		}
		if err != nil && s.tomb.Err() == tomb.ErrStillAlive {
			return err
		}
		return nil
	})
	s.tomb.Go(s.exitOnIdle)
	systemd.SdNotify("READY=1")
}

func (s *SessionAgent) exitOnIdle() error {
	timer := time.NewTimer(s.IdleTimeout)
Loop:
	for {
		select {
		case <-s.tomb.Dying():
			break Loop
		case <-timer.C:
			// Have we been idle
			idleDuration := s.idle.idleDuration()
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

// Stop performs a graceful shutdown of the session agent and waits up to 5
// seconds for it to complete.
func (s *SessionAgent) Stop() error {
	systemd.SdNotify("STOPPING=1")
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	s.tomb.Kill(s.serve.Shutdown(ctx))
	return s.tomb.Wait()
}

func (s *SessionAgent) Dying() <-chan struct{} {
	return s.tomb.Dying()
}

func New() (*SessionAgent, error) {
	agent := &SessionAgent{}
	if err := agent.Init(); err != nil {
		return nil, err
	}
	return agent, nil
}
