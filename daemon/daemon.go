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
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	unix "syscall"
	"time"

	"github.com/coreos/go-systemd/activation"
	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/polkit"
	"github.com/snapcore/snapd/systemd"
)

var systemdSdNotify = systemd.SdNotify

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	Version       string
	overlord      *overlord.Overlord
	snapdListener net.Listener
	snapdServe    *shutdownServer
	snapListener  net.Listener
	snapServe     *shutdownServer
	tomb          tomb.Tomb
	router        *mux.Router
	// enableInternalInterfaceActions controls if adding and removing slots and plugs is allowed.
	enableInternalInterfaceActions bool
	// set to remember we need to restart the system
	restartSystem bool
	mu            sync.Mutex
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

	// can polkit grant access? set to polkit action ID if so
	PolkitOK string

	d *Daemon
}

type accessResult int

const (
	accessOK accessResult = iota
	accessUnauthorized
	accessForbidden
)

var polkitCheckAuthorization = polkit.CheckAuthorization

// canAccess checks the following properties:
//
// - if a user is logged in (via `snap login`) everything is allowed
// - if the user is `root` everything is allowed
// - POST/PUT/DELETE all require `snap login` or `root`
//
// Otherwise for GET requests the following parameters are honored:
// - GuestOK: anyone can access GET
// - UserOK: any uid on the local system can access GET
// - SnapOK: a snap can access this via `snapctl`
func (c *Command) canAccess(r *http.Request, user *auth.UserState) accessResult {
	if user != nil {
		// Authenticated users do anything for now.
		return accessOK
	}

	// isUser means we have a UID for the request
	isUser := false
	pid, uid, socket, err := ucrednetGet(r.RemoteAddr)
	if err == nil {
		isUser = true
	} else if err != errNoID {
		logger.Noticef("unexpected error when attempting to get UID: %s", err)
		return accessForbidden
	}
	isSnap := (socket == dirs.SnapSocket)

	// ensure that snaps can only access SnapOK things
	if isSnap {
		if c.SnapOK {
			return accessOK
		}
		return accessUnauthorized
	}

	if r.Method == "GET" {
		// Guest and user access restricted to GET requests
		if c.GuestOK {
			return accessOK
		}

		if isUser && c.UserOK {
			return accessOK
		}
	}

	// Remaining admin checks rely on identifying peer uid
	if !isUser {
		return accessUnauthorized
	}

	if uid == 0 {
		// Superuser does anything.
		return accessOK
	}

	if c.PolkitOK != "" {
		var flags polkit.CheckFlags
		allowHeader := r.Header.Get(client.AllowInteractionHeader)
		if allowHeader != "" {
			if allow, err := strconv.ParseBool(allowHeader); err != nil {
				logger.Noticef("error parsing %s header: %s", client.AllowInteractionHeader, err)
			} else if allow {
				flags |= polkit.CheckAllowInteraction
			}
		}
		// Pass both pid and uid from the peer ucred to avoid pid race
		if authorized, err := polkitCheckAuthorization(pid, uid, c.PolkitOK, nil, flags); err == nil {
			if authorized {
				// polkit says user is authorised
				return accessOK
			}
		} else if err == polkit.ErrDismissed {
			return accessForbidden
		} else {
			logger.Noticef("polkit error: %s", err)
		}
	}

	return accessUnauthorized
}

type maintenanceTransmitter interface {
	transmitMaintenance(kind errorKind, message string)
}

func (c *Command) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	st := c.d.overlord.State()
	st.Lock()
	// TODO Look at the error and fail if there's an attempt to authenticate with invalid data.
	user, _ := UserFromRequest(st, r)
	st.Unlock()

	switch c.canAccess(r, user) {
	case accessOK:
		// nothing
	case accessUnauthorized:
		Unauthorized("access denied").ServeHTTP(w, r)
		return
	case accessForbidden:
		Forbidden("forbidden").ServeHTTP(w, r)
		return
	}

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
		rsp = rspf(c, r, user)
	}

	if maintTransmitter, ok := rsp.(maintenanceTransmitter); ok {
		_, rst := st.Restarting()
		switch rst {
		case state.RestartSystem:
			maintTransmitter.transmitMaintenance(errorKindSystemRestart, "system is restarting")
		case state.RestartDaemon:
			maintTransmitter.transmitMaintenance(errorKindDaemonRestart, "daemon is restarting")
		}
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

func (w *wrappedWriter) Flush() {
	if f, ok := w.w.(http.Flusher); ok {
		f.Flush()
	}
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

// getListener tries to get a listener for the given socket path from
// the listener map, and if it fails it tries to set it up directly.
func getListener(socketPath string, listenerMap map[string]net.Listener) (net.Listener, error) {
	if listener, ok := listenerMap[socketPath]; ok {
		return listener, nil
	}

	if c, err := net.Dial("unix", socketPath); err == nil {
		c.Close()
		return nil, fmt.Errorf("socket %q already in use", socketPath)
	}

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	address, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	oldmask := unix.Umask(0111)
	listener, err := net.ListenUnix("unix", address)
	unix.Umask(oldmask)
	runtime.UnlockOSThread()
	if err != nil {
		return nil, err
	}

	logger.Debugf("socket %q was not activated; listening", socketPath)

	return listener, nil
}

// Init sets up the Daemon's internal workings.
// Don't call more than once.
func (d *Daemon) Init() error {
	listeners, err := activation.Listeners(false)
	if err != nil {
		return err
	}

	listenerMap := make(map[string]net.Listener, len(listeners))

	for _, listener := range listeners {
		listenerMap[listener.Addr().String()] = listener
	}

	// The SnapdSocket is required-- without it, die.
	if listener, err := getListener(dirs.SnapdSocket, listenerMap); err == nil {
		d.snapdListener = &ucrednetListener{listener}
	} else {
		return fmt.Errorf("when trying to listen on %s: %v", dirs.SnapdSocket, err)
	}

	if listener, err := getListener(dirs.SnapSocket, listenerMap); err == nil {
		// This listener may also be nil if that socket wasn't among
		// the listeners, so check it before using it.
		d.snapListener = &ucrednetListener{listener}
	} else {
		logger.Debugf("cannot get listener for %q: %v", dirs.SnapSocket, err)
	}

	d.addRoutes()

	logger.Noticef("started %v.", httputil.UserAgent())

	return nil
}

func (d *Daemon) addRoutes() {
	d.router = mux.NewRouter()

	for _, c := range api {
		c.d = d
		d.router.Handle(c.Path, c).Name(c.Path)
	}

	// also maybe add a /favicon.ico handler...

	d.router.NotFoundHandler = NotFound("not found")
}

var (
	shutdownTimeout = 5 * time.Second
)

// shutdownServer supplements a http.Server with graceful shutdown.
// TODO: with go1.8 http.Server itself grows a graceful Shutdown method
type shutdownServer struct {
	l       net.Listener
	httpSrv *http.Server

	mu           sync.Mutex
	conns        map[net.Conn]http.ConnState
	shuttingDown bool
}

func newShutdownServer(l net.Listener, h http.Handler) *shutdownServer {
	srv := &http.Server{
		Handler: h,
	}
	ssrv := &shutdownServer{
		l:       l,
		httpSrv: srv,
		conns:   make(map[net.Conn]http.ConnState),
	}
	srv.ConnState = ssrv.trackConn
	return ssrv
}

func (srv *shutdownServer) Serve() error {
	return srv.httpSrv.Serve(srv.l)
}

func (srv *shutdownServer) trackConn(conn net.Conn, state http.ConnState) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	// we ignore hijacked connections, if we do things with websockets
	// we'll need custom shutdown handling for them
	if state == http.StateClosed || state == http.StateHijacked {
		delete(srv.conns, conn)
		return
	}
	if srv.shuttingDown && state == http.StateIdle {
		conn.Close()
		delete(srv.conns, conn)
		return
	}
	srv.conns[conn] = state
}

func (srv *shutdownServer) finishShutdown() error {
	toutC := time.After(shutdownTimeout)

	srv.mu.Lock()
	defer srv.mu.Unlock()

	srv.shuttingDown = true
	for conn, state := range srv.conns {
		if state == http.StateIdle {
			conn.Close()
			delete(srv.conns, conn)
		}
	}

	doWait := true
	for doWait {
		if len(srv.conns) == 0 {
			return nil
		}
		srv.mu.Unlock()
		select {
		case <-time.After(200 * time.Millisecond):
		case <-toutC:
			doWait = false
		}
		srv.mu.Lock()
	}
	return fmt.Errorf("cannot gracefully finish, still active connections on %v after %v", srv.l.Addr(), shutdownTimeout)
}

// Start the Daemon
func (d *Daemon) Start() {
	// die when asked to restart (systemd should get us back up!)
	d.overlord.SetRestartHandler(func(t state.RestartType) {
		switch t {
		case state.RestartDaemon:
			d.tomb.Kill(nil)
		case state.RestartSystem:
			// try to schedule a fallback slow reboot already here
			// in case we get stuck shutting down
			if err := reboot(rebootWaitTimeout); err != nil {
				logger.Noticef("%s", err)
			}

			d.mu.Lock()
			defer d.mu.Unlock()
			// remember we need to restart the system
			d.restartSystem = true
			d.tomb.Kill(nil)
		default:
			logger.Noticef("internal error: restart handler called with unknown restart type: %v", t)
			d.tomb.Kill(nil)
		}
	})

	if d.snapListener != nil {
		d.snapServe = newShutdownServer(d.snapListener, logit(d.router))
	}
	d.snapdServe = newShutdownServer(d.snapdListener, logit(d.router))

	// the loop runs in its own goroutine
	d.overlord.Loop()

	d.tomb.Go(func() error {
		if d.snapListener != nil {
			d.tomb.Go(func() error {
				if err := d.snapServe.Serve(); err != nil && d.tomb.Err() == tomb.ErrStillAlive {
					return err
				}

				return nil
			})
		}

		if err := d.snapdServe.Serve(); err != nil && d.tomb.Err() == tomb.ErrStillAlive {
			return err
		}

		return nil
	})

	// notify systemd that we are ready
	systemdSdNotify("READY=1")
}

var shutdownMsg = i18n.G("reboot scheduled to update the system")

func rebootImpl(rebootDelay time.Duration) error {
	if rebootDelay < 0 {
		rebootDelay = 0
	}
	mins := int64((rebootDelay + time.Minute - 1) / time.Minute)
	cmd := exec.Command("shutdown", "-r", fmt.Sprintf("+%d", mins), shutdownMsg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(out, err)
	}
	return nil
}

var reboot = rebootImpl

var (
	rebootNoticeWait  = 3 * time.Second
	rebootWaitTimeout = 10 * time.Minute
)

// Stop shuts down the Daemon
func (d *Daemon) Stop() error {
	d.tomb.Kill(nil)

	d.mu.Lock()
	restartSystem := d.restartSystem
	d.mu.Unlock()

	d.snapdListener.Close()

	if d.snapListener != nil {
		// stop running hooks first
		// and do it more gracefully if we are restarting
		hookMgr := d.overlord.HookManager()
		if ok, _ := d.overlord.State().Restarting(); ok {
			logger.Noticef("gracefully waiting for running hooks")
			hookMgr.GracefullyWaitRunningHooks()
			logger.Noticef("done waiting for running hooks")
		}
		hookMgr.StopHooks()
		d.snapListener.Close()
	}

	if restartSystem {
		// give time to polling clients to notice restart
		time.Sleep(rebootNoticeWait)
	}

	d.tomb.Kill(d.snapdServe.finishShutdown())
	if d.snapListener != nil {
		d.tomb.Kill(d.snapServe.finishShutdown())
	}

	if !restartSystem {
		// tell systemd that we are stopping
		systemdSdNotify("STOPPING=1")

	}

	d.overlord.Stop()

	err := d.tomb.Wait()
	if err != nil {
		return err
	}

	if restartSystem {
		// ask for shutdown and wait for it to happen.
		// if we exit snapd will be restared by systemd
		rebootDelay := 1 * time.Minute
		ovr := os.Getenv("SNAPD_REBOOT_DELAY") // for tests
		if ovr != "" {
			d, err := time.ParseDuration(ovr)
			if err == nil {
				rebootDelay = d
			}
		}
		if err := reboot(rebootDelay); err != nil {
			return err
		}
		// wait for reboot to happen
		logger.Noticef("Waiting for system reboot")
		time.Sleep(rebootWaitTimeout)
		return fmt.Errorf("expected reboot did not happen")
	}

	return nil
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
		// TODO: Decide when this should be disabled by default.
		enableInternalInterfaceActions: true,
	}, nil
}
