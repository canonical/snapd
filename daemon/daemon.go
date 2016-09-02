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
	"os/exec"
	"strings"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/notifications"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
)

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	Version       string
	overlord      *overlord.Overlord
	snapdListener net.Listener
	snapListener  net.Listener
	tomb          tomb.Tomb
	router        *mux.Router
	hub           *notifications.Hub
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
	// is this path accessible on the snapd-snap socket?
	SnapOK bool

	d *Daemon
}

func (c *Command) canAccess(r *http.Request, user *auth.UserState) bool {
	if user != nil {
		// Authenticated users do anything for now.
		return true
	}

	isUser := false
	uid, err := ucrednetGetUID(r.RemoteAddr)
	if err == nil {
		if uid == 0 {
			// Superuser does anything.
			return true
		}

		isUser = true
	} else if err != errNoUID {
		logger.Noticef("unexpected error when attempting to get UID: %s", err)
		return false
	} else if c.SnapOK {
		return true
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

	listenerMap := make(map[string]net.Listener)

	for _, listener := range listeners {
		listenerMap[listener.Addr().String()] = listener
	}

	// The SnapdSocket is required-- without it, die.
	if listener, ok := listenerMap[dirs.SnapdSocket]; ok {
		d.snapdListener = &ucrednetListener{listener}
	} else {
		return fmt.Errorf("daemon is missing the listener for %s", dirs.SnapdSocket)
	}

	// Note that the SnapSocket listener does not use ucrednet. We use the lack
	// of remote information as an indication that the request originated with
	// this socket. This listener may also be nil if that socket wasn't among
	// the listeners, so check it before using it.
	d.snapListener = listenerMap[dirs.SnapSocket]

	d.addRoutes()

	logger.Debugf("init done in %s", time.Now().Sub(t0))

	return nil
}

func (d *Daemon) addRoutes() {
	d.router = mux.NewRouter()

	for _, c := range api {
		c.d = d
		logger.Debugf("adding %s", c.Path)
		d.router.Handle(c.Path, c).Name(c.Path)
	}

	// also maybe add a /favicon.ico handler...

	d.router.NotFoundHandler = NotFound("not found")
}

var shutdownMsg = i18n.G("reboot scheduled to update the system - temporarily cancel with 'sudo shutdown -c'")

// Start the Daemon
func (d *Daemon) Start() {
	// die when asked to restart (systemd should get us back up!)
	d.overlord.SetRestartHandler(func(t state.RestartType) {
		switch t {
		case state.RestartDaemon:
			d.tomb.Kill(nil)
		case state.RestartSystem:
			cmd := exec.Command("shutdown", "+10", "-r", shutdownMsg)
			if out, err := cmd.CombinedOutput(); err != nil {
				logger.Noticef("%s", osutil.OutputErr(out, err))
			}
		default:
			logger.Noticef("internal error: restart handler called with unknown restart type: %v", t)
			d.tomb.Kill(nil)
		}
	})

	// the loop runs in its own goroutine
	d.overlord.Loop()

	d.tomb.Go(func() error {
		if d.snapListener != nil {
			d.tomb.Go(func() error {
				if err := http.Serve(d.snapListener, logit(d.router)); err != nil && d.tomb.Err() == tomb.ErrStillAlive {
					return err
				}

				return nil
			})
		}

		if err := http.Serve(d.snapdListener, logit(d.router)); err != nil && d.tomb.Err() == tomb.ErrStillAlive {
			return err
		}

		return nil
	})
}

// Stop shuts down the Daemon
func (d *Daemon) Stop() error {
	d.tomb.Kill(nil)
	d.snapdListener.Close()
	if d.snapListener != nil {
		d.snapListener.Close()
	}
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
