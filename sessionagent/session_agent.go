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

package sessionagent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/netutil"
)

type SessionAgent struct {
	Version  string
	listener net.Listener
	serve    *http.Server
	tomb     tomb.Tomb
	router   *mux.Router
}

func (s *SessionAgent) Init() error {
	listenerMap, err := netutil.ActivationListeners()
	if err != nil {
		return err
	}
	agentSocket := fmt.Sprintf("%s/%d/snap-session.socket", dirs.XdgRuntimeDirBase, os.Getuid())
	if s.listener, err = netutil.GetListener(agentSocket, listenerMap); err != nil {
		return fmt.Errorf("cannot listen on socket %s: %v", agentSocket, err)
	}
	s.router = mux.NewRouter()
	s.router.NotFoundHandler = daemon.NotFound("not found")
	s.serve = &http.Server{Handler: s.router}
	return nil
}

func (s *SessionAgent) AddRoute(path string, rspf func(s *SessionAgent, r *http.Request) daemon.Response) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rsp := rspf(s, r)
		rsp.ServeHTTP(w, r)
	})
	s.router.Handle(path, handler).Name(path)
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
}

var (
	shutdownTimeout = 5 * time.Second
)

func (s *SessionAgent) Stop() error {
	ctx, cancel := context.WithTimeout(s.tomb.Context(nil), shutdownTimeout)
	defer cancel()
	s.tomb.Kill(s.serve.Shutdown(ctx))
	return s.tomb.Wait()
}

func NewSessionAgent() (*SessionAgent, error) {
	agent := &SessionAgent{}
	if err := agent.Init(); err != nil {
		return nil, err
	}
	agent.AddRoute("/v1/agent-info", agentInfo)
	return agent, nil
}
