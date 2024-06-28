// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2021 Canonical Ltd
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
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/netutil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/standby"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
)

var ErrRestartSocket = fmt.Errorf("daemon stop requested to wait for socket activation")
var ErrNoFailureRecoveryNeeded = fmt.Errorf("no failure recovery needed")

var systemdSdNotify = systemd.SdNotify

const (
	daemonRestartMsg  = "daemon is restarting"
	systemRestartMsg  = "system is restarting"
	systemHaltMsg     = "system is halting"
	systemPoweroffMsg = "system is powering off"
	socketRestartMsg  = "daemon is stopping to wait for socket activation"
)

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

	// set to what kind of restart was requested (if any)
	requestedRestart restart.RestartType
	// reboot info needed to handle reboots
	rebootInfo *boot.RebootInfo
	// set to remember that we need to exit the daemon in a way that
	// prevents systemd from restarting it
	restartSocket bool
	// degradedErr is set when the daemon is in degraded mode
	degradedErr error

	expectedRebootDidNotHappen bool

	mu     sync.Mutex
	cancel func()
}

// A ResponseFunc handles one of the individual verbs for a method
type ResponseFunc func(*Command, *http.Request, *auth.UserState) Response

// A Command routes a request to an individual per-verb ResponseFunc
type Command struct {
	Path       string
	PathPrefix string
	//
	GET  ResponseFunc
	PUT  ResponseFunc
	POST ResponseFunc

	// Access control.
	ReadAccess  accessChecker
	WriteAccess accessChecker

	d *Daemon
}

func (c *Command) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	st := c.d.state
	st.Lock()
	// TODO Look at the error and fail if there's an attempt to authenticate with invalid data.
	user, _ := userFromRequest(st, r)
	st.Unlock()

	// check if we are in degradedMode
	if c.d.degradedErr != nil && r.Method != "GET" {
		InternalError(c.d.degradedErr.Error()).ServeHTTP(w, r)
		return
	}

	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil && err != errNoID {
		logger.Noticef("unexpected error when attempting to get UID: %s", err)
		InternalError(err.Error()).ServeHTTP(w, r)
		return
	}

	ctx := store.WithClientUserAgent(r.Context(), r)
	r = r.WithContext(ctx)

	var rspf ResponseFunc
	var access accessChecker

	switch r.Method {
	case "GET":
		rspf = c.GET
		access = c.ReadAccess
	case "PUT":
		rspf = c.PUT
		access = c.WriteAccess
	case "POST":
		rspf = c.POST
		access = c.WriteAccess
	}

	if rspf == nil {
		MethodNotAllowed("method %q not allowed", r.Method).ServeHTTP(w, r)
		return
	}

	if rspe := access.CheckAccess(c.d, r, ucred, user); rspe != nil {
		rspe.ServeHTTP(w, r)
		return
	}

	rsp := rspf(c, r, user)

	if srsp, ok := rsp.(StructuredResponse); ok {
		rjson := srsp.JSON()

		st.Lock()
		_, rst := restart.Pending(st)
		st.Unlock()
		rjson.addMaintenanceFromRestartType(rst)

		if rjson.Type != ResponseTypeError {
			st.Lock()
			count, stamp := st.WarningsSummary()
			st.Unlock()
			rjson.addWarningCount(count, stamp)
		}

		// serve the updated serialisation
		rsp = rjson
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
		t := time.Since(t0)
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

	// The SnapdSocket is required -- without it, die.
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

	logger.Noticef("started %v.", snapdenv.UserAgent())

	return nil
}

// SetDegradedMode puts the daemon into a degraded mode. In this mode
// it will return the error given in the "err" argument for commands
// that are not pure HTTP GETs.
//
// This is useful to report errors to the client when the daemon
// cannot work because e.g. a snapd squashfs precondition check failed
// or the system is out of diskspace.
//
// When the system is fine again, calling "SetDegradedMode(nil)" is enough
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

// Start the Daemon. Takes a context which will be used as the base request
// context in the embedded http.Server.
func (d *Daemon) Start(ctx context.Context) (err error) {
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

	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer func() {
		// cancel the context on any errors
		if err != nil {
			cancel()
		}
	}()

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
	// now perform expensive overlord/manages initialization
	if err := d.overlord.StartUp(); err != nil {
		if errors.Is(err, snapstate.ErrUnexpectedRuntimeRestart) {
			logger.Noticef("detected failure recovery context, but no recovery needed")
			return ErrNoFailureRecoveryNeeded
		}
		return err
	}

	d.connTracker = &connTracker{conns: make(map[net.Conn]struct{})}
	d.serve = &http.Server{
		Handler:   logit(d.router),
		ConnState: d.connTracker.trackConn,
		BaseContext: func(net.Listener) context.Context {
			// requests will use the context provided to Start, as
			// the caller will likely cancel it when appropriate
			// thus canceling any outstanding requests to the snapd
			// API
			return ctx
		},
	}

	// enable standby handling
	d.initStandbyHandling()

	// before serving actual connections remove the maintenance.json file as we
	// are no longer down for maintenance, this state most closely corresponds
	// to restart.RestartUnset
	if err := d.updateMaintenanceFile(restart.RestartUnset); err != nil {
		return err
	}

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
func (d *Daemon) HandleRestart(t restart.RestartType, rebootInfo *boot.RebootInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	scheduleFallback := func(a boot.RebootAction) {
		if err := reboot(a, rebootWaitTimeout, rebootInfo); err != nil {
			logger.Noticef("%s", err)
		}
	}
	d.rebootInfo = rebootInfo

	// die when asked to restart (systemd should get us back up!) etc
	switch t {
	case restart.RestartDaemon:
		// save the restart kind to write out a maintenance.json in a bit
		d.requestedRestart = t
	case restart.RestartSystem, restart.RestartSystemNow:
		// try to schedule a fallback slow reboot already here
		// in case we get stuck shutting down

		// save the restart kind to write out a maintenance.json in a bit
		scheduleFallback(boot.RebootReboot)
		d.requestedRestart = t
	case restart.RestartSystemHaltNow:
		scheduleFallback(boot.RebootHalt)
		d.requestedRestart = t
	case restart.RestartSystemPoweroffNow:
		scheduleFallback(boot.RebootPoweroff)
		d.requestedRestart = t
	case restart.RestartSocket:
		// save the restart kind to write out a maintenance.json in a bit
		d.requestedRestart = t
		d.restartSocket = true
	case restart.StopDaemon:
		logger.Noticef("stopping snapd as requested")
	default:
		logger.Noticef("internal error: restart handler called with unknown restart type: %v", t)
	}

	d.tomb.Kill(nil)
}

var (
	rebootNoticeWait       = 3 * time.Second
	rebootWaitTimeout      = 10 * time.Minute
	rebootRetryWaitTimeout = 5 * time.Minute
	rebootMaxAttempts      = 3
)

func (d *Daemon) updateMaintenanceFile(rst restart.RestartType) error {
	// for unset restart, just remove the maintenance.json file
	if rst == restart.RestartUnset {
		err := os.Remove(dirs.SnapdMaintenanceFile)
		// only return err if the error was something other than the file not
		// existing
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	// otherwise marshal and write it out appropriately
	b, err := json.Marshal(maintenanceForRestartType(rst))
	if err != nil {
		return err
	}

	return osutil.AtomicWrite(dirs.SnapdMaintenanceFile, bytes.NewBuffer(b), 0644, 0)
}

// Stop shuts down the Daemon
func (d *Daemon) Stop(sigCh chan<- os.Signal) error {
	// we need to schedule/wait for a system restart again
	if d.expectedRebootDidNotHappen {
		// make the reboot retry immediate
		immediateReboot := true
		// TODO: we do not know the RebootInfo from the previous snapd
		// instance. Passing nil for the moment, but maybe we should
		// cache to disk and recover at this point. In any case, it is
		// expected that the reboot will not be harmful even if
		// RebootInfo is unknown, and that things will end up in a
		// kernel refresh failure, that can be retried later.
		return d.doReboot(sigCh, restart.RestartSystem, nil, immediateReboot, rebootRetryWaitTimeout)
	}
	if d.overlord == nil {
		return fmt.Errorf("internal error: no Overlord")
	}

	if d.cancel != nil {
		d.cancel()
	}

	d.tomb.Kill(nil)

	// check the state associated with a potential restart with the lock to
	// prevent races
	d.mu.Lock()
	// needsFullShutdown is whether the entire system will
	// shutdown or not as a consequence of this request
	needsFullShutdown := false
	switch d.requestedRestart {
	case restart.RestartSystem, restart.RestartSystemNow, restart.RestartSystemHaltNow, restart.RestartSystemPoweroffNow:
		needsFullShutdown = true
	}
	immediateShutdown := false
	switch d.requestedRestart {
	case restart.RestartSystemNow, restart.RestartSystemHaltNow, restart.RestartSystemPoweroffNow:
		immediateShutdown = true
	}
	restartSocket := d.restartSocket
	rebootInfo := d.rebootInfo
	d.mu.Unlock()

	// before not accepting any new client connections we need to write the
	// maintenance.json file for potential clients to see after the daemon stops
	// responding so they can read it correctly and handle the maintenance
	if err := d.updateMaintenanceFile(d.requestedRestart); err != nil {
		logger.Noticef("error writing maintenance file: %v", err)
	}

	// take a timestamp before shutting down the snap listener, and
	// use the time we may spend on waiting for hooks against the shutdown
	// delay.
	ts := time.Now()
	if d.snapListener != nil {
		// stop running hooks first
		// and do it more gracefully if we are restarting
		hookMgr := d.overlord.HookManager()
		d.state.Lock()
		ok, _ := restart.Pending(d.state)
		d.state.Unlock()
		if ok {
			logger.Noticef("gracefully waiting for running hooks")
			hookMgr.GracefullyWaitRunningHooks()
			logger.Noticef("done waiting for running hooks")
		}
		hookMgr.StopHooks()
		d.snapListener.Close()
	}
	timeSpent := time.Since(ts)

	// When shutting down the snapd listener wait until the rebootNoticeWait
	// period has passed before snapdListener is closed to allow polling
	// clients to access the daemon. For testing we disable this unless SNAPD_SHUTDOWN_DELAY
	// has been set, to avoid incurring this wait for every daemon restart which happens
	// quite often in testing.
	if !snapdenv.Testing() || osutil.GetenvBool("SNAPD_SHUTDOWN_DELAY") {
		time.Sleep(rebootNoticeWait - timeSpent)
	}
	d.snapdListener.Close()
	d.standbyOpinions.Stop()

	// We're using the background context here because the tomb's
	// context will likely already have been cancelled when we are
	// called.
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	d.tomb.Kill(d.serve.Shutdown(ctx))
	cancel()

	if !needsFullShutdown {
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

	if err := d.tomb.Wait(); err != nil {
		if err == context.DeadlineExceeded {
			logger.Noticef("WARNING: cannot gracefully shut down in-flight snapd API activity within: %v", shutdownTimeout)
			// the process is shutting down anyway, so we may just
			// as well close the active connections right now
			d.serve.Close()
		} else {
			// do not stop the shutdown even if the tomb errors
			// because we already scheduled a slow shutdown and
			// exiting here will just restart snapd (via systemd)
			// which will lead to confusing results.
			if needsFullShutdown {
				logger.Noticef("WARNING: cannot stop daemon: %v", err)
			} else {
				return err
			}
		}
	}

	if needsFullShutdown {
		return d.doReboot(sigCh, d.requestedRestart, rebootInfo, immediateShutdown, rebootWaitTimeout)
	}

	if d.restartSocket {
		return ErrRestartSocket
	}

	return nil
}

func (d *Daemon) rebootDelay(immediate bool) (time.Duration, error) {
	d.state.Lock()
	defer d.state.Unlock()
	now := time.Now()
	// see whether a reboot had already been scheduled
	var rebootAt time.Time
	err := d.state.Get("daemon-system-restart-at", &rebootAt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return 0, err
	}
	rebootDelay := 1 * time.Minute
	if immediate {
		rebootDelay = 0
	}
	if err == nil {
		rebootDelay = rebootAt.Sub(now)
	} else {
		ovr := os.Getenv("SNAPD_REBOOT_DELAY") // for tests
		if ovr != "" && !immediate {
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

func (d *Daemon) doReboot(sigCh chan<- os.Signal, rst restart.RestartType, rbi *boot.RebootInfo, immediate bool, waitTimeout time.Duration) error {
	rebootDelay, err := d.rebootDelay(immediate)
	if err != nil {
		return err
	}
	action := boot.RebootReboot
	switch rst {
	case restart.RestartSystemHaltNow:
		action = boot.RebootHalt
	case restart.RestartSystemPoweroffNow:
		action = boot.RebootPoweroff
	}
	// ask for shutdown and wait for it to happen.
	// if we exit snapd will be restarted by systemd
	if err := reboot(action, rebootDelay, rbi); err != nil {
		return err
	}
	// wait for reboot to happen
	logger.Noticef("Waiting for %s", action)
	if sigCh != nil {
		signal.Stop(sigCh)
		if len(sigCh) > 0 {
			// a signal arrived in between
			return nil
		}
		close(sigCh)
	}
	time.Sleep(waitTimeout)
	return fmt.Errorf("expected %s did not happen", action)
}

var reboot = boot.Reboot

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

var errExpectedReboot = errors.New("expected reboot did not happen")

// RebootDidNotHappen implements part of overlord.RestartBehavior.
func (d *Daemon) RebootDidNotHappen(st *state.State) error {
	var attempt int
	err := st.Get("daemon-system-restart-tentative", &attempt)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	attempt++
	if attempt > rebootMaxAttempts {
		// giving up, proceed normally, some in-progress refresh
		// might get rolled back!!
		restart.ClearReboot(st)
		clearReboot(st)
		logger.Noticef("snapd was restarted while a system restart was expected, snapd retried to schedule and waited again for a system restart %d times and is giving up", rebootMaxAttempts)
		return nil
	}
	st.Set("daemon-system-restart-tentative", attempt)
	d.state = st
	logger.Noticef("snapd was restarted while a system restart was expected, snapd will try to schedule and wait for a system restart again (attempt %d/%d)", attempt, rebootMaxAttempts)
	return errExpectedReboot
}

// New Daemon
func New() (*Daemon, error) {
	d := &Daemon{}
	ovld, err := overlord.New(d)
	if err == errExpectedReboot {
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
