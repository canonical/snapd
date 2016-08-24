// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"strings"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/notifications"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
)

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	Version         string
	overlord        *overlord.Overlord
	privateListener net.Listener
	publicListener  net.Listener
	tomb            tomb.Tomb
	privateRouter   *mux.Router
	publicRouter    *mux.Router
	hub             *notifications.Hub
	// enableInternalInterfaceActions controls if adding and removing slots and plugs is allowed.
	enableInternalInterfaceActions bool
}

// A ResponseFunc handles one of the individual verbs for a method
type ResponseFunc func(*Command, *http.Request, *auth.UserState) Response

// A Command routes a request to an individual per-verb ResponseFUnc
type Command struct {
	Path string
	//
	GET    ResponseFunc
	PUT    ResponseFunc
	POST   ResponseFunc
	DELETE ResponseFunc
	// can guest GET?
	GuestOK bool
	// can non-admin GET?
	UserOK bool
	//
	d *Daemon
}

func (c *Command) canAccess(r *http.Request, user *auth.UserState) bool {
	if user != nil {
		// Authenticated users do anything for now.
		return true
	}

	isUser := false
	if uid, err := ucrednetGetUID(r.RemoteAddr); err == nil {
		if uid == 0 {
			// Superuser does anything.
			return true
		}

		isUser = true
	}

	if r.Method != "GET" {
		return false
	}

	if isUser && c.UserOK {
		return true
	}

	if c.GuestOK {
		return true
	}

	return false
}

func (c *Command) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	state := c.d.overlord.State()
	state.Lock()
	// TODO Look at the error and fail if there's an attempt to authenticate with invalid data.
	user, _ := UserFromRequest(state, r)
	state.Unlock()

	if !c.canAccess(r, user) {
		Unauthorized("access denied").ServeHTTP(w, r)
		return
	}

	var rspf ResponseFunc
	var rsp = BadMethod("method %q not allowed", r.Method)

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
		rsp = rspf(c, r, user)
	}

	rsp.ServeHTTP(w, r)
}

type wrappedWriter struct {
	w http.ResponseWriter
	s int
}

func (w *wrappedWriter) Header() http.Header {
	return w.w.Header()
}

func (w *wrappedWriter) Write(bs []byte) (int, error) {
	return w.w.Write(bs)
}

func (w *wrappedWriter) WriteHeader(s int) {
	w.w.WriteHeader(s)
	w.s = s
}

func logit(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := &wrappedWriter{w: w}
		t0 := time.Now()
		handler.ServeHTTP(ww, r)
		t := time.Now().Sub(t0)
		url := r.URL.String()
		if !strings.Contains(url, "/changes/") {
			logger.Debugf("%s %s %s %s %d", r.RemoteAddr, r.Method, r.URL, t, ww.s)
		}
	})
}

// Init sets up the Daemon's internal workings.
// Don't call more than once.
func (d *Daemon) Init() error {
	t0 := time.Now()
	listeners, err := activation.Listeners(false)
	if err != nil {
		return err
	}

	if len(listeners) != 2 {
		return fmt.Errorf("daemon does not handle %d listeners right now, only two", len(listeners))
	}

	// systemd provides the sockets in the order they were specified in the
	// .socket file. This needs to be kept in sync with debian/snapd.socket.
	// Currently the first socket is snapd.socket, and the second is
	// snapd-public.socket.
	d.privateListener = &ucrednetListener{listeners[0]}
	d.publicListener = &ucrednetListener{listeners[1]}

	d.addRoutes()

	logger.Debugf("init done in %s", time.Now().Sub(t0))

	return nil
}

func (d *Daemon) addRoutes() {
	d.privateRouter = mux.NewRouter()

	for _, c := range privateAPI {
		c.d = d
		logger.Debugf("adding %s to private API", c.Path)
		d.privateRouter.Handle(c.Path, c).Name(c.Path)
	}

	d.publicRouter = mux.NewRouter()

	for _, c := range publicAPI {
		c.d = d
		logger.Debugf("adding %s to public API", c.Path)
		d.publicRouter.Handle(c.Path, c).Name(c.Path)
	}

	// also maybe add a /favicon.ico handler...

	d.privateRouter.NotFoundHandler = NotFound("not found")
	d.publicRouter.NotFoundHandler = NotFound("not found")
}

// Start the Daemon
func (d *Daemon) Start() {
	// die when asked to restart (systemd should get us back up!)
	d.overlord.SetRestartHandler(func() {
		d.tomb.Kill(nil)
	})

	// the loop runs in its own goroutine
	d.overlord.Loop()

	d.tomb.Go(func() error {
		d.tomb.Go(func() error {
			if err := http.Serve(d.publicListener, logit(d.publicRouter)); err != nil && d.tomb.Err() == tomb.ErrStillAlive {
				return err
			}

			return nil
		})

		if err := http.Serve(d.privateListener, logit(d.privateRouter)); err != nil && d.tomb.Err() == tomb.ErrStillAlive {
			return err
		}

		return nil
	})
}

// Stop shuts down the Daemon
func (d *Daemon) Stop() error {
	d.tomb.Kill(nil)
	d.privateListener.Close()
	d.publicListener.Close()
	d.overlord.Stop()

	return d.tomb.Wait()
}

// Dying is a tomb-ish thing
func (d *Daemon) Dying() <-chan struct{} {
	return d.tomb.Dying()
}

// New Daemon
func New() (*Daemon, error) {
	ovld, err := overlord.New()
	if err != nil {
		return nil, err
	}
	return &Daemon{
		overlord: ovld,
		hub:      notifications.NewHub(),
		// TODO: Decide when this should be disabled by default.
		enableInternalInterfaceActions: true,
	}, nil
}
