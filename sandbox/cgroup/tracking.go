package cgroup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/systemd"
)

var osGetuid = os.Getuid
var osGetpid = os.Getpid
var cgroupProcessPathInTrackingCgroup = ProcessPathInTrackingCgroup

var ErrCannotTrackProcess = errors.New("cannot track application process")

// TrackingOptions control how tracking, based on systemd transient scope, operates.
type TrackingOptions struct {
	// AllowSessionBus controls if CreateTransientScopeForTracking will
	// consider using the session bus for making the request.
	AllowSessionBus bool
}

// CreateTransientScopeForTracking puts the current process in a transient scope.
//
// To quote systemd documentation about scope units:
//
// >> Scopes units manage a set of system processes. Unlike service units,
// >> scope units manage externally created processes, and do not fork off
// >> processes on its own.
//
// Scope names must be unique, a randomly generated UUID is appended to the
// security tag, further suffixed with the string ".scope".
func CreateTransientScopeForTracking(securityTag string, opts *TrackingOptions) error {
	if opts == nil {
		// Retain original semantics when not explicitly configured otherwise.
		opts = &TrackingOptions{AllowSessionBus: true}
	}
	logger.Debugf("creating transient scope %s", securityTag)

	// Session or system bus might be unavailable. To avoid being fragile
	// ignore all errors when establishing session bus connection to avoid
	// breaking user interactions. This is consistent with similar failure
	// modes below, where other parts of the stack fail.
	//
	// Ideally we would check for a distinct error type but this is just an
	// errors.New() in go-dbus code.
	uid := osGetuid()
	// Depending on options, we may use the session bus instead of the system
	// bus. In addition, when uid == 0 we may fall back from using the session
	// bus to the system bus.
	var isSessionBus bool
	var conn *dbus.Conn
	var err error
	if opts.AllowSessionBus {
		isSessionBus, conn, err = sessionOrMaybeSystemBus(uid)
		if err != nil {
			return ErrCannotTrackProcess
		}
	} else {
		isSessionBus = false
		conn, err = dbusutil.SystemBus()
		if err != nil {
			return ErrCannotTrackProcess
		}
	}

	// We ask the kernel for a random UUID. We need one because each transient
	// scope needs a unique name. The unique name is composed of said UUID and
	// the snap security tag.
	uuid, err := randomUUID()
	if err != nil {
		return err
	}

	// Enforcing uniqueness is preferred to reusing an existing scope for
	// simplicity since doing otherwise by joining an existing scope has
	// limitations:
	// - the originally started scope must be marked as a delegate, with all
	//   consequences.
	// - the method AttachProcessesToUnit is unavailable on Ubuntu 16.04
	unitName := fmt.Sprintf("%s-%s.scope", systemd.EscapeUnitName(securityTag), uuid)

	pid := osGetpid()
	start := time.Now()
tryAgain:
	// Create a transient scope by talking to systemd over DBus.
	if err := doCreateTransientScope(conn, unitName, pid); err != nil {
		switch err {
		case errDBusUnknownMethod:
			return ErrCannotTrackProcess
		case errDBusSpawnChildExited:
			fallthrough
		case errDBusNameHasNoOwner:
			if isSessionBus && uid == 0 {
				// We cannot activate systemd --user for root,
				// try the system bus as a fallback.
				logger.Debugf("cannot activate systemd --user on session bus, falling back to system bus: %s", err)
				isSessionBus = false
				conn, err = dbusutil.SystemBus()
				if err != nil {
					logger.Debugf("system bus is not available: %s", err)
					return ErrCannotTrackProcess
				}
				logger.Debugf("using system bus now, session bus could not activate systemd --user")
				goto tryAgain
			}
			return ErrCannotTrackProcess
		}
		return err
	}
	// We may have created a transient scope but due to the constraints the
	// kernel puts on process transitions on unprivileged users (and remember
	// that systemd --user is unprivileged) the actual re-association with the
	// scope cgroup may have silently failed - unfortunately some versions of
	// systemd do not report an error in that case. Systemd 238 and newer
	// detects the error correctly and uses privileged systemd running as pid 1
	// to assist in the transition.
	//
	// For more details about the transition constraints refer to
	// cgroup_procs_write_permission() as of linux 5.8 and
	// unit_attach_pids_to_cgroup() as of systemd 245.
	//
	// Verify the effective tracking cgroup and check that our scope name is
	// contained therein.
	hasTracking := false
	for tries := 0; tries < 100; tries++ {
		path, err := cgroupProcessPathInTrackingCgroup(pid)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, unitName) {
			hasTracking = true
			break
		}
		time.Sleep(1 * time.Millisecond)
	}
	waitForTracking := time.Since(start)
	logger.Debugf("waited %v for tracking", waitForTracking)
	if !hasTracking {
		logger.Debugf("systemd could not associate process %d with transient scope %s", pid, unitName)
		return ErrCannotTrackProcess
	}
	return nil
}

// ConfirmSystemdAppTracking checks if systemd tracks this process as a snap app.
//
// If the application process is not tracked then ErrCannotTrackProcess is returned.
func ConfirmSystemdAppTracking(securityTag string) error {
	pid := osGetpid()
	path, err := cgroupProcessPathInTrackingCgroup(pid)
	if err != nil {
		return err
	}

	escapedTag := systemd.EscapeUnitName(securityTag)

	// the transient scope of the application carries the security tag, eg:
	// snap.hello-world.sh-4706fe54-7802-4808-aa7e-ae8b567239e0.scope
	if strings.HasPrefix(filepath.Base(path), escapedTag+"-") && strings.HasSuffix(path, ".scope") {
		return nil
	}

	return ErrCannotTrackProcess
}

// ConfirmSystemdServiceTracking checks if systemd tracks this process as a snap service.
//
// Systemd is placing started services, both user and system, into appropriate
// tracking groups. Given a security tag we can confirm if the current process
// belongs to such tracking group and thus could be identified by snapd as
// belonging to a particular snap and application.
//
// If the application process is not tracked then ErrCannotTrackProcess is returned.
func ConfirmSystemdServiceTracking(securityTag string) error {
	pid := osGetpid()
	path, err := cgroupProcessPathInTrackingCgroup(pid)
	if err != nil {
		return err
	}
	unitName := fmt.Sprintf("%s.service", securityTag)
	if !strings.Contains(path, unitName) {
		return ErrCannotTrackProcess
	}
	return nil
}

func sessionOrMaybeSystemBus(uid int) (isSessionBus bool, conn *dbus.Conn, err error) {
	// The scope is created with a DBus call to systemd running either on
	// system or session bus. We have a preference for session bus, as this is
	// where applications normally go to. When a session bus is not available
	// and the invoking user is root, we use the system bus instead.
	//
	// It is worth noting that hooks will not normally have a session bus to
	// connect to, as they are invoked as descendants of snapd, and snapd is a
	// service running outside of any session.
	conn, err = dbusutil.SessionBus()
	if err == nil {
		logger.Debugf("using session bus")
		return true, conn, nil
	}
	logger.Debugf("session bus is not available: %s", err)
	if uid == 0 {
		logger.Debugf("falling back to system bus")
		conn, err = dbusutil.SystemBus()
		if err != nil {
			logger.Debugf("system bus is not available: %s", err)
		} else {
			logger.Debugf("using system bus now, session bus was not available")
		}
	}
	return false, conn, err
}

type handledDBusError struct {
	msg       string
	dbusError string
}

func (e *handledDBusError) Error() string {
	return fmt.Sprintf("%s [%s]", e.msg, e.dbusError)
}

var (
	errDBusUnknownMethod    = &handledDBusError{msg: "unknown dbus object method", dbusError: "org.freedesktop.DBus.Error.UnknownMethod"}
	errDBusNameHasNoOwner   = &handledDBusError{msg: "dbus name has no owner", dbusError: "org.freedesktop.DBus.Error.NameHasNoOwner"}
	errDBusSpawnChildExited = &handledDBusError{msg: "dbus spawned child process exited", dbusError: "org.freedesktop.DBus.Error.Spawn.ChildExited"}

	// pick a decent fit-all timeout
	createScopeJobTimeout = 10 * time.Second
)

// startTransientScope requests systemd to create a transient unit and returns
// the associated systemd job path.
//
// The scope is created by asking systemd via the specified DBus connection.
// The unit name and the PID to attach are provided as well. The DBus method
// call is performed outside confinement established by snap-confine.
func startTransientScope(conn *dbus.Conn, unitName string, pid int) (job dbus.ObjectPath, err error) {
	// Documentation of StartTransientUnit is available at
	// https://www.freedesktop.org/wiki/Software/systemd/dbus/
	//
	// The property and auxUnit types are not well documented but can be traced
	// from systemd source code. As of systemd 245 it can be found in src/core/dbus-manager.c,
	// in a declaration containing SD_BUS_METHOD_WITH_NAMES("SD_BUS_METHOD_WITH_NAMES",...
	// From there one can follow to method_start_transient_unit to understand
	// how argument parsing is performed.
	//
	// Systemd defines the signature of StartTransientUnit as
	// "ssa(sv)a(sa(sv))". The signature can be decomposed as follows:
	//
	// unitName string // name of the unit to start
	// jobMode string  // corresponds to --job-mode= (see systemctl(1) manual page)
	// properties []struct{
	//   Name string
	//   Value interface{}
	// } // properties describe properties of the started unit
	// auxUnits []struct {
	//   Name string
	//   Properties []struct{
	//   	Name string
	//   	Value interface{}
	//	 }
	// } // auxUnits describe any additional units to define.
	type property struct {
		Name  string
		Value interface{}
	}
	type auxUnit struct {
		Name  string
		Props []property
	}

	// The mode string decides how the job is interacting with other systemd
	// jobs on the system. The documentation of the systemd StartUnit() method
	// describes the possible values and their properties:
	//
	// >> StartUnit() enqeues a start job, and possibly depending jobs. Takes
	// >> the unit to activate, plus a mode string. The mode needs to be one of
	// >> replace, fail, isolate, ignore-dependencies, ignore-requirements. If
	// >> "replace" the call will start the unit and its dependencies, possibly
	// >> replacing already queued jobs that conflict with this. If "fail" the
	// >> call will start the unit and its dependencies, but will fail if this
	// >> would change an already queued job. If "isolate" the call will start
	// >> the unit in question and terminate all units that aren't dependencies
	// >> of it. If "ignore-dependencies" it will start a unit but ignore all
	// >> its dependencies. If "ignore-requirements" it will start a unit but
	// >> only ignore the requirement dependencies. It is not recommended to
	// >> make use of the latter two options. Returns the newly created job
	// >> object.
	//
	// Here we choose "fail" to match systemd-run.
	mode := "fail"
	properties := []property{{"PIDs", []uint{uint(pid)}}}
	aux := []auxUnit(nil)
	systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	call := systemd.Call(
		"org.freedesktop.systemd1.Manager.StartTransientUnit",
		0,
		unitName,
		mode,
		properties,
		aux,
	)
	if err := call.Store(&job); err != nil {
		if dbusErr, ok := err.(dbus.Error); ok {
			logger.Debugf("StartTransientUnit failed with %q: %v", dbusErr.Name, dbusErr.Body)
			// Some specific DBus errors have distinct handling.
			switch dbusErr.Name {
			case "org.freedesktop.DBus.Error.NameHasNoOwner":
				// Nothing is providing systemd bus name. This is, most likely,
				// an Ubuntu 14.04 system with the special deputy systemd.
				return "", errDBusNameHasNoOwner
			case "org.freedesktop.DBus.Error.UnknownMethod":
				// The DBus API is not supported on this system. This can happen on
				// very old versions of Systemd, for instance on Ubuntu 14.04.
				return "", errDBusUnknownMethod
			case "org.freedesktop.DBus.Error.Spawn.ChildExited":
				// We tried to socket-activate dbus-daemon or bus-activate
				// systemd --user but it failed.
				return "", errDBusSpawnChildExited
			case "org.freedesktop.systemd1.UnitExists":
				// Starting a scope with a name that already exists is an
				// error. Normally this should never happen.
				return "", fmt.Errorf("cannot create transient scope: scope %q clashed: %s", unitName, err)
			default:
				return "", fmt.Errorf("cannot create transient scope: DBus error %q: %v", dbusErr.Name, dbusErr.Body)
			}
		}
		return "", fmt.Errorf("cannot create transient scope: %s", err)
	}
	logger.Debugf("create transient scope job: %s", job)
	return job, nil
}

// doCreateTransientScopeOpportunisticSync creates a transient scope with a
// given unit name asking systemd to move the provided pid to that scope, does
// not wait for the systemd job to complete
func doCreateTransientScopeNoSync(conn *dbus.Conn, unitName string, pid int) error {
	_, err := startTransientScope(conn, unitName, pid)
	return err
}

// doCreateTransientScopeOpportunisticSync creates a transient scope with a
// given unit name asking systemd to move the provided pid to that scope, and
// waits for the systemd job to finish
func doCreateTransientScopeJobRemovedSync(conn *dbus.Conn, unitName string, pid int) error {
	// set up a watch for JobRemoved signals, so that we'll know when our
	// request has completed
	jobRemoveMatch := []dbus.MatchOption{
		dbus.WithMatchInterface("org.freedesktop.systemd1.Manager"),
		dbus.WithMatchMember("JobRemoved"),
	}
	if err := conn.AddMatchSignal(jobRemoveMatch...); err != nil {
		return fmt.Errorf("cannot subscribe to systemd signals: %v", err)
	}
	// signal channel with buffer for some messages
	signals := make(chan *dbus.Signal, 10)
	// for receiving job results
	jobResultChan := make(chan string, 1)
	// for passing the job we want to observe
	jobWaitFor := make(chan dbus.ObjectPath, 1)
	// and start watching for signals, we do this before even sending a
	// request, so that we won't miss any signals from systemd
	conn.Signal(signals)

	var wg sync.WaitGroup
	defer func() {
		close(jobWaitFor)
		// wait for the signal handling to finish before returning
		wg.Wait()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		jobResults := make(map[dbus.ObjectPath]string, 10)
		expectedJob := dbus.ObjectPath("")
		for {
			select {
			case job, ok := <-jobWaitFor:
				if !ok {
					// the channel got closed, meaning it's
					// time to clean up
					conn.RemoveSignal(signals)
					conn.RemoveMatchSignal(jobRemoveMatch...)
					close(jobResultChan)
					close(signals)
					return
				}
				if result, ok := jobResults[job]; ok {
					// maybe we already have result for this job
					jobResultChan <- result
				} else {
					expectedJob = job
				}
			case sig, ok := <-signals:
				if !ok {
					continue
				}
				// make sure the signal name is as expected, although the
				// match selectors should ensure we only receive
				// JobRemoved signals
				if sig.Name != "org.freedesktop.systemd1.Manager.JobRemoved" {
					continue
				}
				var id uint32
				var jobFromSignal dbus.ObjectPath
				var unit string
				var result string
				if err := dbus.Store(sig.Body, &id, &jobFromSignal, &unit, &result); err != nil {
					continue
				}
				if jobFromSignal == expectedJob {
					// we are already expecting results for this job
					jobResultChan <- result
				} else {
					// or not, just keep result for now, as
					// a request to track a job may come
					// later
					jobResults[jobFromSignal] = result
				}
			}
		}
	}()
	job, err := startTransientScope(conn, unitName, pid)
	if err != nil {
		return err
	}
	jobWaitFor <- job
	select {
	case result := <-jobResultChan:
		logger.Debugf("job result is %q", result)
		if result != "done" {
			return fmt.Errorf("transient scope could not be started, job %v finished with result %v", job, result)
		}
	case <-time.After(createScopeJobTimeout):
		return fmt.Errorf("transient scope not created in %v", createScopeJobTimeout)
	}
	logger.Debugf("transient scope %v created", unitName)
	return nil
}

// doCreateTransientScope creates a systemd transient scope with specified properties.
//
// The scope is created by asking systemd via the specified DBus connection.
// The unit name and the PID to attach are provided as well. The DBus method
// call is performed outside confinement established by snap-confine.
var doCreateTransientScope = func(conn *dbus.Conn, unitName string, pid int) error {
	// in theory we could use a single implementation that sync with job
	// removed signal and inspects the result, however some older
	// distributions sport an unpatched and broken version of systemd, which
	// prevents the job from being correctly moved to new scope when
	// creating one on the user systemd instance, and thus we always get an
	// error. Fortunately, it so happens that distributions that have
	// switched to a unified cgroup hierarchy, carry a systemd version that
	// has so far been able to successfully create user scopes in user
	// sessions
	if IsUnified() {
		// when using cgroup v2, we absolutely must be sure that the
		// tracking group has been created, otherwise we risk
		// establishing a device cgroup filtering in the wrong group
		return doCreateTransientScopeJobRemovedSync(conn, unitName, pid)
	}
	return doCreateTransientScopeNoSync(conn, unitName, pid)
}

// The source of the bytes generated here is the same as that of
// /dev/urandom which doesn't block and is sufficient for our purposes
// of avoiding clashing UUIDs that are needed for all of the non-service
// commands that are started with the help of this UUID.
var randomUUID = randutil.RandomKernelUUID
