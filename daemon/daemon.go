// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"net"
	"net/http"

	"github.com/coreos/go-systemd/activation"
	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"launchpad.net/snappy/logger"
)

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	listener net.Listener
	tomb     tomb.Tomb
	router   *mux.Router
}

// A ResponseFunc handles one of the individual verbs for a method
type ResponseFunc func(*Command, *http.Request) Response

// A Command routes a request to an individual per-verb ResponseFUnc
type Command struct {
	Path string
	//
	GET    ResponseFunc
	PUT    ResponseFunc
	POST   ResponseFunc
	DELETE ResponseFunc
	//
	d *Daemon
}

func (c *Command) handler(w http.ResponseWriter, r *http.Request) {
	var rspf ResponseFunc
	rsp := BadMethod

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

	rsp.Handler(w, r)
}

// Init sets up the Daemon's internal workings.
// Don't call more than once.
func (d *Daemon) Init() error {
	listeners, err := activation.Listeners(false)
	if err != nil {
		return err
	}

	if len(listeners) != 1 {
		return fmt.Errorf("daemon does not handle %d listeners right now, just one", len(listeners))
	}

	d.listener = listeners[0]

	d.router = mux.NewRouter()

	for _, c := range api {
		c.d = d
		logger.Debugf("adding %s", c.Path)
		d.router.HandleFunc(c.Path, c.handler).Name(c.Path)
	}

	d.router.NotFoundHandler = http.HandlerFunc(NotFound.Handler)

	return nil
}

// Start the Daemon
func (d *Daemon) Start() {
	d.tomb.Go(func() error {
		return http.Serve(d.listener, d.router)
	})
}

// Stop shuts down the Daemon
func (d *Daemon) Stop() error {
	d.tomb.Kill(nil)
	return d.tomb.Wait()
}

// Dying is a tomb-ish thing
func (d *Daemon) Dying() <-chan struct{} {
	return d.tomb.Dying()
}

// New Daemon
func New() *Daemon {
	return &Daemon{}
}
