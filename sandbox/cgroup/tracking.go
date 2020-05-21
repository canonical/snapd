package cgroup

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/godbus/dbus"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
)

var osGetuid = os.Getuid
var osGetpid = os.Getpid
var cgroupProcessPathInTrackingCgroup = ProcessPathInTrackingCgroup

var ErrCannotTrackProcess = errors.New("cannot track application process")

func CreateTransientScope(securityTag string) error {
	if !features.RefreshAppAwareness.IsEnabled() {
		return nil
	}
	logger.Debugf("creating transient scope %s", securityTag)

	// Session or system bus might be unavailable. To avoid being fragile
	// ignore all errors when establishing session bus connection to avoid
	// breaking user interactions. This is consistent with similar failure
	// modes below, where other parts of the stack fail.
	//
	// Ideally we would check for a distinct error type but this is just an
	// errors.New() in go-dbus code.
	isSessionBus, conn, err := sessionOrMaybeSystemBus(osGetuid())
	if err != nil {
		return ErrCannotTrackProcess
	}

	// We ask the kernel for a random UUID. We need one because each transient
	// scope needs a unique name. The unique name is comprosed of said UUID and
	// the snap security tag.
	uuid, err := randomUUID()
	if err != nil {
		return err
	}

	// Enforcing uniqueness is preferred to reusing an existing scope for
	// simplicity since doing otherwise and joining an existing scope has
	// limitations:
	// - the originally started scope must be marked as a delegate, with all
	//   consequences.
	// - the method AttachProcessesToUnit is unavailable on Ubuntu 16.04
	unitName := fmt.Sprintf("%s.%s.scope", securityTag, uuid)

	pid := osGetpid()
tryAgain:
	// Create a transient scope by talking to systemd over DBus.
	if err := doCreateTransientScope(conn, unitName, pid); err != nil {
		switch err {
		case errDBusUnknownMethod:
			return ErrCannotTrackProcess
		case errDBusSpawnChildExited:
			fallthrough
		case errDBusNameHasNoOwner:
			// We cannot activate systemd --user for root, try the system bus
			// as a fallback.
			if isSessionBus && osGetuid() == 0 {
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
	// We may have created a transient scope but due to a kernel design,
	// in specific situation when we are in a cgroup owned by one user,
	// and we want to run a process as a different user, *and* systemd is
	// older than 238, then this can silently fail.
	//
	// Verify the effective tracking cgroup and check that our scope name
	// is contained therein.
	path, err := cgroupProcessPathInTrackingCgroup(pid)
	if err != nil {
		return err
	}
	if !strings.Contains(path, unitName) {
		logger.Debugf("systemd could not associate process %d with transient scope %s", pid, unitName)
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
	return isSessionBus, conn, err
}

var errDBusUnknownMethod = errors.New("org.freedesktop.DBus.Error.UnknownMethod")
var errDBusNameHasNoOwner = errors.New("org.freedesktop.DBus.Error.NameHasNoOwner")
var errDBusSpawnChildExited = errors.New("org.freedesktop.DBus.Error.Spawn.ChildExited")

// doCreateTransientScope creates a systemd transient scope with specified properties.
//
// The scope is created by asking systemd via the specified DBus connection.
// The unit name and the PID to attach are provided as well. The DBus method
// call is performed outside confinement established by snap-confine.
var doCreateTransientScope = func(conn *dbus.Conn, unitName string, pid int) error {
	// The property and auxUnit types are not well documented but can be traced
	// from systemd source code. Systemd defines the signature of
	// StartTransientUnit as "ssa(sv)a(sa(sv))". The signature can be
	// decomposed as follows:
	//
	// Partial documentation, at the time of this writing, is available at
	// https://www.freedesktop.org/wiki/Software/systemd/dbus/
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
		Name string
		// XXX: This is getting marshaled as an invalid variant.
		Value interface{}
	}
	type auxUnit struct {
		Name  string
		Props []property
	}

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
	var job dbus.ObjectPath
	if err := call.Store(&job); err != nil {
		if dbusErr, ok := err.(dbus.Error); ok {
			logger.Debugf("StartTransientUnit failed with %q: %v", dbusErr.Name, dbusErr.Body)
			// Some specific DBus errors have distinct handling.
			switch dbusErr.Name {
			case "org.freedesktop.DBus.Error.NameHasNoOwner":
				// Nothing is providing systemd bus name. This is, most likely,
				// an Ubuntu 14.04 system with the special deputy systemd.
				return errDBusNameHasNoOwner
			case "org.freedesktop.DBus.Error.UnknownMethod":
				// The DBus API is not supported on this system. This can happen on
				// very old versions of Systemd, for instance on Ubuntu 14.04.
				return errDBusUnknownMethod
			case "org.freedesktop.DBus.Error.Spawn.ChildExited":
				// We tried to socket-activate dbus-daemon or bus-activate
				// systemd --user but it failed.
				return errDBusSpawnChildExited
			case "org.freedesktop.systemd1.UnitExists":
				// Starting a scope with a name that already exists is an
				// error. Normally this should never happen.
				return fmt.Errorf("cannot create transient scope: scope %q clashed: %s", unitName, err)
			default:
				return fmt.Errorf("cannot create transient scope: DBus error %q: %v", dbusErr.Name, dbusErr.Body)
			}
		}
		if err != nil {
			return fmt.Errorf("cannot create transient scope: %s", err)
		}
	}
	logger.Debugf("created transient scope as object: %s", job)
	return nil
}

var randomUUID = func() (string, error) {
	// The source of the bytes generated here is the same as that of
	// /dev/urandom which doesn't block and is sufficient for our purposes
	// of avoiding clashing UUIDs that are needed for all of the non-service
	// commands that are started with the help of this UUID.
	uuidBytes, err := ioutil.ReadFile("/proc/sys/kernel/random/uuid")
	return strings.TrimSpace(string(uuidBytes)), err
}
