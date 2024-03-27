// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2024 Canonical Ltd
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
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/polkit"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/strutil"
)

var polkitCheckAuthorization = polkit.CheckAuthorization

var checkPolkitAction = checkPolkitActionImpl

var osReadlink = os.Readlink

func checkPolkitActionImpl(r *http.Request, ucred *ucrednet, action string) *apiError {
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
	switch authorized, err := polkitCheckAuthorization(ucred.Pid, ucred.Uid, action, nil, flags); err {
	case nil:
		if authorized {
			// polkit says user is authorised
			return nil
		}
	case polkit.ErrDismissed:
		return AuthCancelled("cancelled")
	default:
		logger.Noticef("polkit error: %s", err)
	}
	return Unauthorized("access denied")
}

// accessChecker checks whether a particular request is allowed.
//
// An access checker will either allow a request, deny it, or return
// accessUnknown, which indicates the decision should be delegated to
// the next access checker.
type accessChecker interface {
	CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError
}

// requireSnapdSocket ensures the request was received via snapd.socket.
func requireSnapdSocket(ucred *ucrednet) *apiError {
	if ucred == nil {
		return Forbidden("access denied")
	}

	if ucred.Socket != dirs.SnapdSocket {
		return Forbidden("access denied")
	}

	return nil
}

// openAccess allows requests without authentication, provided they
// have peer credentials and were not received on snapd-snap.socket
type openAccess struct{}

func (ac openAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	return requireSnapdSocket(ucred)
}

// authenticatedAccess allows requests from authenticated users,
// provided they were not received on snapd-snap.socket
//
// A user is considered authenticated if they provide a macaroon, are
// the root user according to peer credentials, or granted access by
// Polkit.
type authenticatedAccess struct {
	Polkit string
}

func (ac authenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if rspe := requireSnapdSocket(ucred); rspe != nil {
		return rspe
	}

	if user != nil {
		return nil
	}

	if ucred.Uid == 0 {
		return nil
	}

	// We check polkit last because it may result in the user
	// being prompted for authorisation. This should be avoided if
	// access is otherwise granted.
	if ac.Polkit != "" {
		return checkPolkitAction(r, ucred, ac.Polkit)
	}

	return Unauthorized("access denied")
}

// rootAccess allows requests from the root uid, provided they
// were not received on snapd-snap.socket
type rootAccess struct{}

func (ac rootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if rspe := requireSnapdSocket(ucred); rspe != nil {
		return rspe
	}

	if ucred.Uid == 0 {
		return nil
	}
	return Forbidden("access denied")
}

// snapAccess allows requests from the snapd-snap.socket
type snapAccess struct{}

func (ac snapAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if ucred == nil {
		return Forbidden("access denied")
	}

	if ucred.Socket == dirs.SnapSocket {
		return nil
	}
	// FIXME: should snapctl access be allowed on the main socket?
	return Forbidden("access denied")
}

var (
	cgroupSnapNameFromPid     = cgroup.SnapNameFromPid
	requireInterfaceApiAccess = requireInterfaceApiAccessImpl
)

func requireInterfaceApiAccessImpl(d *Daemon, r *http.Request, ucred *ucrednet, interfaceNames []string) *apiError {
	if ucred == nil {
		return Forbidden("access denied")
	}

	switch ucred.Socket {
	case dirs.SnapdSocket:
		// Allow access on main snapd.socket
		return nil

	case dirs.SnapSocket:
		// Handled below
	default:
		return Forbidden("access denied")
	}

	// Access on snapd-snap.socket requires a connected plug.
	snapName, err := cgroupSnapNameFromPid(int(ucred.Pid))
	if err != nil {
		return Forbidden("could not determine snap name for pid: %s", err)
	}

	st := d.state
	st.Lock()
	defer st.Unlock()
	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return Forbidden("internal error: cannot get connections: %s", err)
	}
	foundMatchingInterface := false
	for refStr, connState := range conns {
		if !connState.Active() || !strutil.ListContains(interfaceNames, connState.Interface) {
			continue
		}
		connRef, err := interfaces.ParseConnRef(refStr)
		if err != nil {
			return Forbidden("internal error: %s", err)
		}
		if connRef.PlugRef.Snap == snapName {
			r.RemoteAddr = ucrednetAttachInterface(r.RemoteAddr, connState.Interface)
			// Do not return here, but keep processing connections for the side
			// effect of attaching all connected interfaces we asked for to the
			// remote address.
			foundMatchingInterface = true
		}
	}
	if foundMatchingInterface {
		return nil
	}
	return Forbidden("access denied")
}

// interfaceOpenAccess behaves like openAccess, but allows requests from
// snapd-snap.socket for snaps that plug one of the provided interfaces.
type interfaceOpenAccess struct {
	Interfaces []string
}

func (ac interfaceOpenAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	return requireInterfaceApiAccess(d, r, ucred, ac.Interfaces)
}

// interfaceAuthenticatedAccess behaves like authenticatedAccess, but
// allows requests from snapd-snap.socket that plug one of the provided
// interfaces.
type interfaceAuthenticatedAccess struct {
	Interfaces []string
	Polkit     string
}

func (ac interfaceAuthenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if rspe := requireInterfaceApiAccess(d, r, ucred, ac.Interfaces); rspe != nil {
		return rspe
	}

	// check as well that we have admin permission to proceed with
	// the operation
	if user != nil {
		return nil
	}

	if ucred.Uid == 0 {
		return nil
	}

	// We check polkit last because it may result in the user
	// being prompted for authorisation. This should be avoided if
	// access is otherwise granted.
	if ac.Polkit != "" {
		return checkPolkitAction(r, ucred, ac.Polkit)
	}

	return Unauthorized("access denied")
}

// isRequestFromSnapCmd checks that the request is coming from snap command.
//
// It checks that the request process "/proc/PID/exe" points to one of the
// known locations of the snap command. This not a security-oriented check.
func isRequestFromSnapCmd(r *http.Request) (bool, error) {
	ucred, err := ucrednetGet(r.RemoteAddr)
	if err != nil {
		return false, err
	}
	exe, err := osReadlink(fmt.Sprintf("/proc/%d/exe", ucred.Pid))
	if err != nil {
		return false, err
	}

	// SNAP_REEXEC=0
	if exe == filepath.Join(dirs.GlobalRootDir, "/usr/bin/snap") {
		return true, nil
	}

	// Check if re-exec in snapd
	path := filepath.Join(dirs.SnapMountDir, "snapd/*/usr/bin/snap")
	if matched, err := filepath.Match(path, exe); err != nil {
		return false, err
	} else if matched {
		return true, nil
	}

	// Check if re-exec in core
	path = filepath.Join(dirs.SnapMountDir, "core/*/usr/bin/snap")
	if matched, err := filepath.Match(path, exe); err != nil {
		return false, err
	} else if matched {
		return true, nil
	}

	return false, nil
}
