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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// requireSocket ensures the request was received via specified socket.
func requireSocket(ucred *ucrednet, socket string) *apiError {
	if ucred == nil {
		return Forbidden("access denied")
	}

	if ucred.Socket != socket {
		return Forbidden("access denied")
	}

	return nil
}

type accessLevel string

const (
	accessLevelRoot          = "root"
	accessLevelAuthenticated = "authenticated"
	accessLevelOpen          = "open"
)

type accessOptions struct {
	accessLevel     accessLevel
	requireSocket   string
	interfaceAccess *interfaceAccessReqs
	polkitAction    string
}

func checkAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, opts accessOptions) *apiError {
	if opts.requireSocket != "" {
		if rspe := requireSocket(ucred, opts.requireSocket); rspe != nil {
			return rspe
		}
	}

	if opts.interfaceAccess != nil {
		rspe := requireInterfaceApiAccess(d, r, ucred, *opts.interfaceAccess)
		if rspe != nil {
			return rspe
		}
	}

	if opts.accessLevel == accessLevelOpen {
		return nil
	}

	if opts.accessLevel == accessLevelAuthenticated && user != nil {
		return nil
	}

	if ucred.Uid == 0 {
		return nil
	}

	// We check polkit last because it may result in the user
	// being prompted for authorisation. This should be avoided if
	// access is otherwise granted.
	if opts.polkitAction != "" {
		return checkPolkitAction(r, ucred, opts.polkitAction)
	}

	// XXX: when to 403 vs 401?
	if opts.accessLevel == accessLevelAuthenticated || opts.interfaceAccess != nil {
		return Unauthorized("access denied")
	}
	return Forbidden("access denied")
}

// openAccess allows requests without authentication, provided they
// have peer credentials and were not received on snapd-snap.socket
type openAccess struct{}

func (ac openAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	opts := accessOptions{
		accessLevel:   accessLevelOpen,
		requireSocket: dirs.SnapdSocket,
	}
	return checkAccess(d, r, ucred, user, opts)
}

// authenticatedAccess allows requests from authenticated users,
// provided they were not received on snapd-snap.socket
//
// A user is considered authenticated if they provide a macaroon, are
// the root user according to peer credentials, or granted access by
// Polkit.
type authenticatedAccess struct {
	// Polkit is an optional polkit action to check as fallback
	// if the user is not root or does not provide a macaroon.
	//
	// Note: The specified polkit action must require auth_admin
	// to avoid compromising security.
	Polkit string
}

func (ac authenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	opts := accessOptions{
		accessLevel:   accessLevelAuthenticated,
		requireSocket: dirs.SnapdSocket,
		polkitAction:  ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts)
}

// rootAccess allows requests from the root uid, provided they
// were not received on snapd-snap.socket
//
// A user is considered authenticated if they  are the root user
// according to peer credentials, or granted access by Polkit.
type rootAccess struct {
	// Polkit is an optional polkit action to check as fallback
	// if the user is not root.
	//
	// Note: The specified polkit action must require auth_admin
	// to avoid compromising security.
	Polkit string
}

func (ac rootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	opts := accessOptions{
		accessLevel:   accessLevelRoot,
		requireSocket: dirs.SnapdSocket,
		polkitAction:  ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts)
}

// snapAccess allows requests from the snapd-snap.socket only.
type snapAccess struct{}

func (ac snapAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	opts := accessOptions{
		accessLevel:   accessLevelOpen,
		requireSocket: dirs.SnapSocket,
	}
	return checkAccess(d, r, ucred, user, opts)
}

var (
	cgroupSnapNameFromPid     = cgroup.SnapNameFromPid
	requireInterfaceApiAccess = requireInterfaceApiAccessImpl
)

type interfaceAccessReqs struct {
	// Interfaces is a list of interfaces, at least one of which must be
	// connected
	Interfaces []string

	// Slot when true, the snap must appear on the slot side
	Slot bool
	// Plug when true, the snap must appear on the plug side
	Plug bool
}

func requireInterfaceApiAccessImpl(d *Daemon, r *http.Request,
	ucred *ucrednet, req interfaceAccessReqs,
) *apiError {
	if !req.Slot && !req.Plug {
		return InternalError("required connection side is unspecified")
	}
	if req.Slot && req.Plug {
		return InternalError("snap cannot be specified on both sides of the connection")
	}

	if len(req.Interfaces) == 0 {
		return InternalError("interfaces access check, but interfaces list is empty")
	}

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
		if !connState.Active() || !strutil.ListContains(req.Interfaces, connState.Interface) {
			continue
		}
		connRef, err := interfaces.ParseConnRef(refStr)
		if err != nil {
			return Forbidden("internal error: %s", err)
		}
		matchOnSlot := req.Slot && connRef.SlotRef.Snap == snapName
		matchOnPlug := req.Plug && connRef.PlugRef.Snap == snapName
		if matchOnPlug || matchOnSlot {
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
	opts := accessOptions{
		accessLevel: accessLevelOpen,
		interfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
	}
	return checkAccess(d, r, ucred, user, opts)
}

// interfaceAuthenticatedAccess behaves like authenticatedAccess, but
// allows requests from snapd-snap.socket that plug one of the provided
// interfaces.
type interfaceAuthenticatedAccess struct {
	Interfaces []string
	// Polkit is an optional polkit action to check as fallback
	// if the user is not root or does not provide a macaroon.
	//
	// Note: The specified polkit action must require auth_admin
	// to avoid compromising security.
	Polkit string
}

func (ac interfaceAuthenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	opts := accessOptions{
		accessLevel: accessLevelAuthenticated,
		interfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
		polkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts)
}

// interfaceProviderRootAccess behaves like rootAccess, but also allows requests
// over snapd-snap.socket for snaps that have a connection of specific interface
// and are present on the slot side of that connection.
type interfaceProviderRootAccess struct {
	Interfaces []string
	// Polkit is an optional polkit action to check as fallback
	// if the user is not root.
	//
	// Note: The specified polkit action must require auth_admin
	// to avoid compromising security.
	Polkit string
}

func (ac interfaceProviderRootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	opts := accessOptions{
		accessLevel: accessLevelRoot,
		interfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Slot:       true,
		},
		polkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts)
}

// interfaceRootAccess behaves like rootAccess, but also allows requests
// over snapd-snap.socket for snaps that have a connection of specific interface
// and are present on the plug side of that connection.
type interfaceRootAccess struct {
	Interfaces []string
	// Polkit is an optional polkit action to check as fallback
	// if the user is not root.
	//
	// Note: The specified polkit action must require auth_admin
	// to avoid compromising security.
	Polkit string
}

func (ac interfaceRootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	opts := accessOptions{
		accessLevel: accessLevelRoot,
		interfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
		polkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts)
}

type actionRequest struct {
	Action string `json:"action"`
}

// byActionAccess is an access checker multiplexer. The correct
// access checker is chosen based on the "action" field in the
// incoming request.
type byActionAccess struct {
	// ByAction maps from detected request action to access checker.
	ByAction map[string]accessChecker
	// Default is the fallback access checker if no action was matched
	// which could happen if:
	//   - Content type is not "application/json"
	//   - JSON body is malformed
	//   - No action is passed
	//   - Action is not found under ByAction
	//
	// This should be as strict as possible, e.g. rootAccess.
	Default accessChecker
}

const maxBodySize = 4 * 1024 * 1024 // 4MB

func (ac byActionAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if r.Header.Get("Content-Type") != "application/json" {
		return ac.Default.CheckAccess(d, r, ucred, user)
	}

	req := actionRequest{}

	bufSize := r.ContentLength
	if bufSize > maxBodySize {
		bufSize = maxBodySize
	}
	buf := bytes.NewBuffer(make([]byte, 0, bufSize))
	tr := io.TeeReader(r.Body, buf)
	lr := io.LimitedReader{R: tr, N: maxBodySize}
	decoder := json.NewDecoder(&lr)
	err := decoder.Decode(&req)
	if err != nil {
		if (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) && lr.N <= 0 {
			return BadRequest("body size limit exceeded")
		}
		// Content type is JSON, but it's invalid
		return BadRequest(err.Error())
	}
	if decoder.More() {
		return BadRequest("unexpected data after request body")
	}

	r.Body.Close()
	r.Body = io.NopCloser(buf)

	checker := ac.ByAction[req.Action]
	if checker == nil {
		return ac.Default.CheckAccess(d, r, ucred, user)
	}

	return checker.CheckAccess(d, r, ucred, user)
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
