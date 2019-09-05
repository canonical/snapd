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
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/systemd"
)

type SessionAgent struct {
	Version  string
	listener net.Listener
	serve    *http.Server
	tomb     tomb.Tomb
	router   *mux.Router
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

func (s *SessionAgent) Init() error {
	listenerMap, err := netutil.ActivationListeners()
	if err != nil {
		return err
	}
	agentSocket := fmt.Sprintf("%s/%d/snapd-session-agent.socket", dirs.XdgRuntimeDirBase, os.Getuid())
	if s.listener, err = netutil.GetListener(agentSocket, listenerMap); err != nil {
		return fmt.Errorf("cannot listen on socket %s: %v", agentSocket, err)
	}
	s.addRoutes()
	s.serve = &http.Server{Handler: s.router}
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
	systemd.SdNotify("READY=1")
}

var (
	shutdownTimeout = 5 * time.Second
)

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
