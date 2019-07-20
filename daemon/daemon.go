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
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/standby"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/polkit"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
)

var ErrRestartSocket = fmt.Errorf("daemon stop requested to wait for socket activation")

var systemdSdNotify = systemd.SdNotify

// A Daemon listens for requests and routes them to the right command
type Daemon struct {
	Version         string
	overlord        *overlord.Overlord
	state           *state.State
	snapdListener   net.Listener
	snapListener    net.Listener
	connTracker     *connTracker
	serve           *http.Server
	tomb            tomb.Tomb
	router          *mux.Router
	standbyOpinions *standby.StandbyOpinions

	// set to remember we need to restart the system
	restartSystem bool
	// set to remember that we need to exit the daemon in a way that
	// prevents systemd from restarting it
	restartSocket bool
	// degradedErr is set when the daemon is in degraded mode
	degradedErr error

	expectedRebootDidNotHappen bool

	mu sync.Mutex
}

// A ResponseFunc handles one of the individual verbs for a method
type ResponseFunc func(*Command, *http.Request, *auth.UserState) Response

// A Command routes a request to an individual per-verb ResponseFUnc
type Command struct {
	Path       string
	PathPrefix string
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
	// this path is only accessible to root
	RootOnly bool

	// can polkit grant access? set to polkit action ID if so
	PolkitOK string

	d *Daemon
}

type accessResult int

const (
	accessOK accessResult = iota
	accessUnauthorized
	accessForbidden
	accessCancelled
)

var polkitCheckAuthorization = polkit.CheckAuthorization

// canAccess checks the following properties:
//
// - if the user is `root` everything is allowed
// - if a user is logged in (via `snap login`) and the command doesn't have RootOnly, everything is allowed
// - POST/PUT/DELETE all require `root`, or just `snap login` if not RootOnly
//
// Otherwise for GET requests the following parameters are honored:
// - GuestOK: anyone can access GET
// - UserOK: any uid on the local system can access GET
// - RootOnly: only root can access this
// - SnapOK: a snap can access this via `snapctl`
func (c *Command) canAccess(r *http.Request, user *auth.UserState) accessResult {
	if c.RootOnly && (c.UserOK || c.GuestOK || c.SnapOK) {
		// programming error
		logger.Panicf("Command can't have RootOnly together with any *OK flag")
	}

	if user != nil && !c.RootOnly {
		// Authenticated users do anything not requiring explicit root.
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

	// the !RootOnly check is redundant, but belt-and-suspenders
	if r.Method == "GET" && !c.RootOnly {
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

	if c.RootOnly {
		return accessUnauthorized
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
			return accessCancelled
		} else {
			logger.Noticef("polkit error: %s", err)
		}
	}

	return accessUnauthorized
}

func (c *Command) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	st := c.d.state
	st.Lock()
	// TODO Look at the error and fail if there's an attempt to authenticate with invalid data.
	user, _ := UserFromRequest(st, r)
	st.Unlock()

	// check if we are in degradedMode
	if c.d.degradedErr != nil && r.Method != "GET" {
		InternalError(c.d.degradedErr.Error()).ServeHTTP(w, r)
		return
	}

	switch c.canAccess(r, user) {
	case accessOK:
		// nothing
	case accessUnauthorized:
		Unauthorized("access denied").ServeHTTP(w, r)
		return
	case accessForbidden:
		Forbidden("forbidden").ServeHTTP(w, r)
		return
	case accessCancelled:
		AuthCancelled("cancelled").ServeHTTP(w, r)
		return
	}

	ctx := store.WithClientUserAgent(r.Context(), r)
	r = r.WithContext(ctx)

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

	if rsp, ok := rsp.(*resp); ok {
		_, rst := st.Restarting()
		switch rst {
		case state.RestartSystem:
			rsp.transmitMaintenance(errorKindSystemRestart, "system is restarting")
		case state.RestartDaemon:
			rsp.transmitMaintenance(errorKindDaemonRestart, "daemon is restarting")
		case state.RestartSocket:
			rsp.transmitMaintenance(errorKindDaemonRestart, "daemon is stopping to wait for socket activation")
		}
		if rsp.Type != ResponseTypeError {
			st.Lock()
			count, stamp := st.WarningsSummary()
			st.Unlock()
			rsp.addWarningsToMeta(count, stamp)
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

// Init sets up the Daemon's internal workings.
// Don't call more than once.
func (d *Daemon) Init() error {
	listenerMap, err := netutil.ActivationListeners()
	if err != nil {
		return err
	}

	// The SnapdSocket is required-- without it, die.
	if listener, err := netutil.GetListener(dirs.SnapdSocket, listenerMap); err == nil {
		d.snapdListener = &ucrednetListener{Listener: listener}
	} else {
		return fmt.Errorf("when trying to listen on %s: %v", dirs.SnapdSocket, err)
	}

	if listener, err := netutil.GetListener(dirs.SnapSocket, listenerMap); err == nil {
		// This listener may also be nil if that socket wasn't among
		// the listeners, so check it before using it.
		d.snapListener = &ucrednetListener{Listener: listener}
	} else {
		logger.Debugf("cannot get listener for %q: %v", dirs.SnapSocket, err)
	}

	d.addRoutes()

	logger.Noticef("started %v.", httputil.UserAgent())

	return nil
}

// SetDegradedMode puts the daemon into an degraded mode which will the
// error given in the "err" argument for commands that are not marked
// as readonlyOK.
//
// This is useful to report errors to the client when the daemon
// cannot work because e.g. a sanity check failed or the system is out
// of diskspace.
//
// When the system is fine again calling "DegradedMode(nil)" is enough
// to put the daemon into full operation again.
func (d *Daemon) SetDegradedMode(err error) {
	d.degradedErr = err
}

func (d *Daemon) addRoutes() {
	d.router = mux.NewRouter()

	for _, c := range api {
		c.d = d
		if c.PathPrefix == "" {
			d.router.Handle(c.Path, c).Name(c.Path)
		} else {
			d.router.PathPrefix(c.PathPrefix).Handler(c).Name(c.PathPrefix)
		}
	}

	// also maybe add a /favicon.ico handler...

	d.router.NotFoundHandler = NotFound("not found")
}

var (
	shutdownTimeout = 25 * time.Second
)

type connTracker struct {
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func (ct *connTracker) CanStandby() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	return len(ct.conns) == 0
}

func (ct *connTracker) trackConn(conn net.Conn, state http.ConnState) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	// we ignore hijacked connections, if we do things with websockets
	// we'll need custom shutdown handling for them
	if state == http.StateNew || state == http.StateActive {
		ct.conns[conn] = struct{}{}
	} else {
		delete(ct.conns, conn)
	}
}

func (d *Daemon) initStandbyHandling() {
	d.standbyOpinions = standby.New(d.state)
	d.standbyOpinions.AddOpinion(d.connTracker)
	d.standbyOpinions.AddOpinion(d.overlord)
	d.standbyOpinions.AddOpinion(d.overlord.SnapManager())
	d.standbyOpinions.AddOpinion(d.overlord.DeviceManager())
	d.standbyOpinions.Start()
}

// Start the Daemon
func (d *Daemon) Start() error {
	if d.expectedRebootDidNotHappen {
		// we need to schedule and wait for a system restart
		d.tomb.Kill(nil)
		// avoid systemd killing us again while we wait
		systemdSdNotify("READY=1")
		return nil
	}
	if d.overlord == nil {
		panic("internal error: no Overlord")
	}

	to, reasoning, err := d.overlord.StartupTimeout()
	if err != nil {
		return err
	}
	if to > 0 {
		to = to.Round(time.Microsecond)
		us := to.Nanoseconds() / 1000
		logger.Noticef("adjusting startup timeout by %v (%s)", to, reasoning)
		systemdSdNotify(fmt.Sprintf("EXTEND_TIMEOUT_USEC=%d", us))
	}
	// now perform expensive overlord/manages initiliazation
	if err := d.overlord.StartUp(); err != nil {
		return err
	}

	d.connTracker = &connTracker{conns: make(map[net.Conn]struct{})}
	d.serve = &http.Server{
		Handler:   logit(d.router),
		ConnState: d.connTracker.trackConn,
	}

	// enable standby handling
	d.initStandbyHandling()

	// the loop runs in its own goroutine
	d.overlord.Loop()

	d.tomb.Go(func() error {
		if d.snapListener != nil {
			d.tomb.Go(func() error {
				if err := d.serve.Serve(d.snapListener); err != http.ErrServerClosed && d.tomb.Err() == tomb.ErrStillAlive {
					return err
				}

				return nil
			})
		}

		if err := d.serve.Serve(d.snapdListener); err != http.ErrServerClosed && d.tomb.Err() == tomb.ErrStillAlive {
			return err
		}

		return nil
	})

	// notify systemd that we are ready
	systemdSdNotify("READY=1")
	return nil
}

// HandleRestart implements overlord.RestartBehavior.
func (d *Daemon) HandleRestart(t state.RestartType) {
	// die when asked to restart (systemd should get us back up!) etc
	switch t {
	case state.RestartDaemon:
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
	case state.RestartSocket:
		d.mu.Lock()
		defer d.mu.Unlock()
		d.restartSocket = true
	default:
		logger.Noticef("internal error: restart handler called with unknown restart type: %v", t)
	}
	d.tomb.Kill(nil)
}

var (
	rebootNoticeWait       = 3 * time.Second
	rebootWaitTimeout      = 10 * time.Minute
	rebootRetryWaitTimeout = 5 * time.Minute
	rebootMaxTentatives    = 3
)

// Stop shuts down the Daemon
func (d *Daemon) Stop(sigCh chan<- os.Signal) error {
	// we need to schedule/wait for a system restart again
	if d.expectedRebootDidNotHappen {
		return d.doReboot(sigCh, rebootRetryWaitTimeout)
	}
	if d.overlord == nil {
		return fmt.Errorf("internal error: no Overlord")
	}

	d.tomb.Kill(nil)

	d.mu.Lock()
	restartSystem := d.restartSystem
	restartSocket := d.restartSocket
	d.mu.Unlock()

	d.snapdListener.Close()
	d.standbyOpinions.Stop()

	if d.snapListener != nil {
		// stop running hooks first
		// and do it more gracefully if we are restarting
		hookMgr := d.overlord.HookManager()
		if ok, _ := d.state.Restarting(); ok {
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

	// We're using the background context here because the tomb's
	// context will likely already have been cancelled when we are
	// called.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	d.tomb.Kill(d.serve.Shutdown(ctx))
	cancel()

	if !restartSystem {
		// tell systemd that we are stopping
		systemdSdNotify("STOPPING=1")

	}

	if restartSocket {
		// At this point we processed all open requests (and
		// stopped accepting new requests) - before going into
		// socket activated mode we need to check if any of
		// those open requests resulted in something that
		// prevents us from going into socket activation mode.
		//
		// If this is the case we do a "normal" snapd restart
		// to process the new changes.
		if !d.standbyOpinions.CanStandby() {
			d.restartSocket = false
		}
	}
	d.overlord.Stop()

	err := d.tomb.Wait()
	if err != nil {
		// do not stop the shutdown even if the tomb errors
		// because we already scheduled a slow shutdown and
		// exiting here will just restart snapd (via systemd)
		// which will lead to confusing results.
		if restartSystem {
			logger.Noticef("WARNING: cannot stop daemon: %v", err)
		} else {
			return err
		}
	}

	if restartSystem {
		return d.doReboot(sigCh, rebootWaitTimeout)
	}

	if d.restartSocket {
		return ErrRestartSocket
	}

	return nil
}

func (d *Daemon) rebootDelay() (time.Duration, error) {
	d.state.Lock()
	defer d.state.Unlock()
	now := time.Now()
	// see whether a reboot had already been scheduled
	var rebootAt time.Time
	err := d.state.Get("daemon-system-restart-at", &rebootAt)
	if err != nil && err != state.ErrNoState {
		return 0, err
	}
	rebootDelay := 1 * time.Minute
	if err == nil {
		rebootDelay = rebootAt.Sub(now)
	} else {
		ovr := os.Getenv("SNAPD_REBOOT_DELAY") // for tests
		if ovr != "" {
			d, err := time.ParseDuration(ovr)
			if err == nil {
				rebootDelay = d
			}
		}
		rebootAt = now.Add(rebootDelay)
		d.state.Set("daemon-system-restart-at", rebootAt)
	}
	return rebootDelay, nil
}

func (d *Daemon) doReboot(sigCh chan<- os.Signal, waitTimeout time.Duration) error {
	rebootDelay, err := d.rebootDelay()
	if err != nil {
		return err
	}
	// ask for shutdown and wait for it to happen.
	// if we exit snapd will be restared by systemd
	if err := reboot(rebootDelay); err != nil {
		return err
	}
	// wait for reboot to happen
	logger.Noticef("Waiting for system reboot")
	if sigCh != nil {
		signal.Stop(sigCh)
		if len(sigCh) > 0 {
			// a signal arrived in between
			return nil
		}
		close(sigCh)
	}
	time.Sleep(waitTimeout)
	return fmt.Errorf("expected reboot did not happen")
}

var shutdownMsg = i18n.G("reboot scheduled to update the system")

func rebootImpl(rebootDelay time.Duration) error {
	if rebootDelay < 0 {
		rebootDelay = 0
	}
	mins := int64(rebootDelay / time.Minute)
	cmd := exec.Command("shutdown", "-r", fmt.Sprintf("+%d", mins), shutdownMsg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return osutil.OutputErr(out, err)
	}
	return nil
}

var reboot = rebootImpl

// Dying is a tomb-ish thing
func (d *Daemon) Dying() <-chan struct{} {
	return d.tomb.Dying()
}

func clearReboot(st *state.State) {
	st.Set("daemon-system-restart-at", nil)
	st.Set("daemon-system-restart-tentative", nil)
}

// RebootAsExpected implements part of overlord.RestartBehavior.
func (d *Daemon) RebootAsExpected(st *state.State) error {
	clearReboot(st)
	return nil
}

// RebootDidNotHappen implements part of overlord.RestartBehavior.
func (d *Daemon) RebootDidNotHappen(st *state.State) error {
	var nTentative int
	err := st.Get("daemon-system-restart-tentative", &nTentative)
	if err != nil && err != state.ErrNoState {
		return err
	}
	nTentative++
	if nTentative > rebootMaxTentatives {
		// giving up, proceed normally, some in-progress refresh
		// might get rolled back!!
		st.ClearReboot()
		clearReboot(st)
		logger.Noticef("snapd was restarted while a system restart was expected, snapd retried to schedule and waited again for a system restart %d times and is giving up", rebootMaxTentatives)
		return nil
	}
	st.Set("daemon-system-restart-tentative", nTentative)
	d.state = st
	logger.Noticef("snapd was restarted while a system restart was expected, snapd will try to schedule and wait for a system restart again (tenative %d/%d)", nTentative, rebootMaxTentatives)
	return state.ErrExpectedReboot
}

// New Daemon
func New() (*Daemon, error) {
	d := &Daemon{}
	ovld, err := overlord.New(d)
	if err == state.ErrExpectedReboot {
		// we proceed without overlord until we reach Stop
		// where we will schedule and wait again for a system restart.
		// ATM we cannot do that in New because we need to satisfy
		// systemd notify mechanisms.
		d.expectedRebootDidNotHappen = true
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	d.overlord = ovld
	d.state = ovld.State()
	return d, nil
}
