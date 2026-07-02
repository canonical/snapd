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
	"github.com/snapcore/snapd/seclog"
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
	CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError
}

func isAuditedAccessLevel(level accessLevel) bool {
	return level == accessLevelAuthenticated || level == accessLevelRoot
}

func recordDeniedIfAudited(rec AuthzRecorder, level accessLevel, reason string) {
	if isAuditedAccessLevel(level) {
		rec.RecordDenied(reason)
	}
}

func recordGrantedIfAudited(rec AuthzRecorder, level accessLevel, reason, iface string, plug bool) {
	if isAuditedAccessLevel(level) {
		rec.RecordGranted(reason, iface, plug)
	}
}

func recordDeniedMissingInterfaceIfAudited(rec AuthzRecorder, level accessLevel, plug bool) {
	if !isAuditedAccessLevel(level) {
		return
	}
	if plug {
		rec.RecordDenied(seclog.ReasonDeniedMissingInterfacePlug)
	} else {
		rec.RecordDenied(seclog.ReasonDeniedMissingInterfaceSlot)
	}
}

// requireSockets ensures the request was received via one of the specified sockets.
func requireSockets(ucred *ucrednet, sockets []string, rec AuthzRecorder, level accessLevel) *apiError {
	if ucred == nil {
		recordDeniedIfAudited(rec, level, seclog.ReasonDeniedNoPeerCredentials)
		return Forbidden("access denied")
	}

	if !strutil.ListContains(sockets, ucred.Socket) {
		recordDeniedIfAudited(rec, level, seclog.ReasonDeniedSocketNotPermitted)
		return Forbidden("access denied")
	}

	return nil
}

type accessLevel string

const (
	accessLevelRoot          accessLevel = "root"
	accessLevelAuthenticated accessLevel = "authenticated"
	accessLevelOpen          accessLevel = "open"
)

type accessOptions struct {
	AccessLevel     accessLevel
	Sockets         []string
	InterfaceAccess *interfaceAccessReqs
	PolkitAction    string
}

func (o accessOptions) validate() error {
	switch o.AccessLevel {
	case accessLevelRoot, accessLevelAuthenticated, accessLevelOpen:
	default:
		return fmt.Errorf("unexpected access level %q", o.AccessLevel)
	}

	if len(o.Sockets) == 0 {
		return errors.New("no sockets specified")
	}
	for _, socket := range o.Sockets {
		switch socket {
		case dirs.SnapdSocket, dirs.SnapSocket:
		default:
			return fmt.Errorf("unexpected socket %q", socket)
		}
	}

	return nil
}

func checkAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, opts accessOptions, rec AuthzRecorder) *apiError {
	if err := opts.validate(); err != nil {
		return InternalError(err.Error())
	}

	if rspe := requireSockets(ucred, opts.Sockets, rec, opts.AccessLevel); rspe != nil {
		return rspe
	}

	var ifaceOutcome interfaceAccessOutcome
	if opts.InterfaceAccess != nil {
		// No interface checks are made if request is coming from snapd.socket
		// to account for the snapd-control interface.
		var rspe *apiError
		ifaceOutcome, rspe = requireInterfaceApiAccess(d, r, ucred, *opts.InterfaceAccess, rec, opts.AccessLevel)
		if rspe != nil {
			return rspe
		}
	}

	if opts.AccessLevel == accessLevelOpen {
		return nil
	}

	iface := ifaceOutcome.MatchedIface
	plug := ifaceOutcome.Plug

	if opts.AccessLevel == accessLevelAuthenticated && user != nil {
		// user != nil means we have an authenticated user
		recordGrantedIfAudited(rec, opts.AccessLevel, seclog.ReasonGrantedUserAuth, iface, plug)
		return nil
	}

	if ucred.Uid == 0 {
		recordGrantedIfAudited(rec, opts.AccessLevel, seclog.ReasonGrantedRootAuth, iface, plug)
		return nil
	}

	// We check polkit last because it may result in the user
	// being prompted for authorisation. This should be avoided if
	// access is otherwise granted.
	if opts.PolkitAction != "" {
		rspe := checkPolkitAction(r, ucred, opts.PolkitAction)
		if rspe == nil {
			recordGrantedIfAudited(rec, opts.AccessLevel, seclog.ReasonGrantedPolkitAuth, iface, plug)
		} else {
			recordDeniedIfAudited(rec, opts.AccessLevel, seclog.ReasonDeniedPolkitAuthDenied)
		}
		return rspe
	}

	// XXX: when to 403 vs 401?
	if opts.AccessLevel == accessLevelAuthenticated || opts.InterfaceAccess != nil {
		recordDeniedIfAudited(rec, opts.AccessLevel, seclog.ReasonDeniedUserAuthDenied)
		return Unauthorized("access denied")
	}
	recordDeniedIfAudited(rec, opts.AccessLevel, seclog.ReasonDeniedRootAuthDenied)
	return Forbidden("access denied")
}

// openAccess allows requests without authentication, provided they
// have peer credentials and were not received on snapd-snap.socket
type openAccess struct{}

func (ac openAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel: accessLevelOpen,
		Sockets:     []string{dirs.SnapdSocket},
	}
	return checkAccess(d, r, ucred, user, opts, rec)
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

func (ac authenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel:  accessLevelAuthenticated,
		Sockets:      []string{dirs.SnapdSocket},
		PolkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts, rec)
}

// rootAccess allows requests from the root uid, provided they
// were not received on snapd-snap.socket
type rootAccess struct{}

func (ac rootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel: accessLevelRoot,
		Sockets:     []string{dirs.SnapdSocket},
	}
	return checkAccess(d, r, ucred, user, opts, rec)
}

// snapAccess allows requests from the snapd-snap.socket only.
type snapAccess struct{}

func (ac snapAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel: accessLevelOpen,
		Sockets:     []string{dirs.SnapSocket},
	}
	return checkAccess(d, r, ucred, user, opts, rec)
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

// interfaceAccessOutcome carries a matched interface connection for grant postfix.
type interfaceAccessOutcome struct {
	MatchedIface string
	Plug         bool
}

func requireInterfaceApiAccessImpl(d *Daemon, r *http.Request,
	ucred *ucrednet, req interfaceAccessReqs, rec AuthzRecorder, level accessLevel,
) (interfaceAccessOutcome, *apiError) {
	if !req.Slot && !req.Plug {
		return interfaceAccessOutcome{}, InternalError("required connection side is unspecified")
	}
	if req.Slot && req.Plug {
		return interfaceAccessOutcome{}, InternalError("snap cannot be specified on both sides of the connection")
	}

	if len(req.Interfaces) == 0 {
		return interfaceAccessOutcome{}, InternalError("interfaces access check, but interfaces list is empty")
	}

	if ucred == nil {
		recordDeniedIfAudited(rec, level, seclog.ReasonDeniedNoPeerCredentials)
		return interfaceAccessOutcome{}, Forbidden("access denied")
	}

	switch ucred.Socket {
	case dirs.SnapdSocket:
		return interfaceAccessOutcome{}, nil

	case dirs.SnapSocket:
		// Handled below
	default:
		recordDeniedIfAudited(rec, level, seclog.ReasonDeniedSocketNotPermitted)
		return interfaceAccessOutcome{}, Forbidden("access denied")
	}

	// Access on snapd-snap.socket requires a connected plug.
	snapName, err := cgroupSnapNameFromPid(int(ucred.Pid))
	if err != nil {
		recordDeniedMissingInterfaceIfAudited(rec, level, req.Plug)
		return interfaceAccessOutcome{}, Forbidden("could not determine snap name for pid: %s", err)
	}

	st := d.state
	st.Lock()
	defer st.Unlock()
	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return interfaceAccessOutcome{}, Forbidden("internal error: cannot get connections: %s", err)
	}
	var outcome interfaceAccessOutcome
	for refStr, connState := range conns {
		if !connState.Active() || !strutil.ListContains(req.Interfaces, connState.Interface) {
			continue
		}
		connRef, err := interfaces.ParseConnRef(refStr)
		if err != nil {
			return interfaceAccessOutcome{}, Forbidden("internal error: %s", err)
		}
		matchOnSlot := req.Slot && connRef.SlotRef.Snap == snapName
		matchOnPlug := req.Plug && connRef.PlugRef.Snap == snapName
		if matchOnPlug || matchOnSlot {
			r.RemoteAddr = ucrednetAttachInterface(r.RemoteAddr, connState.Interface)
			// Do not return here, but keep processing connections for the side
			// effect of attaching all connected interfaces we asked for to the
			// remote address.
			if outcome.MatchedIface == "" {
				outcome.MatchedIface = connState.Interface
				outcome.Plug = req.Plug
			}
		}
	}
	if outcome.MatchedIface != "" {
		return outcome, nil
	}
	recordDeniedMissingInterfaceIfAudited(rec, level, req.Plug)
	return interfaceAccessOutcome{}, Forbidden("access denied")
}

// interfaceOpenAccess behaves like openAccess, but allows requests from
// snapd-snap.socket for snaps that plug one of the provided interfaces.
type interfaceOpenAccess struct {
	Interfaces []string
}

func (ac interfaceOpenAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel: accessLevelOpen,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
	}
	return checkAccess(d, r, ucred, user, opts, rec)
}

// interfaceAuthenticatedAccess behaves like authenticatedAccess, but also
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

func (ac interfaceAuthenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel: accessLevelAuthenticated,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
		PolkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts, rec)
}

// interfaceProviderRootAccess behaves like rootAccess, but also allows requests
// over snapd-snap.socket for snaps that have a connection of specific interface
// and are present on the slot side of that connection.
type interfaceProviderRootAccess struct {
	Interfaces []string
}

func (ac interfaceProviderRootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel: accessLevelRoot,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Slot:       true,
		},
	}
	return checkAccess(d, r, ucred, user, opts, rec)
}

// interfaceRootAccess behaves like rootAccess, but also allows requests
// over snapd-snap.socket for snaps that have a connection of specific interface
// and are present on the plug side of that connection.
//
// A user is considered authenticated if they are the root user according to
// peer credentials, or granted access by Polkit.
type interfaceRootAccess struct {
	Interfaces []string
	// Polkit is an optional polkit action to check as fallback
	// if the user is not root.
	// In most cases it is preferred to set Polkit since snaps
	// are not usually running as root.
	//
	// Note: The specified polkit action must require auth_admin
	// to avoid compromising security.
	Polkit string
}

func (ac interfaceRootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	opts := accessOptions{
		AccessLevel: accessLevelRoot,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
		PolkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts, rec)
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
	// Default is the fallback access checker if no action was matched.
	//
	// This can only be one of:
	//   - rootAccess
	//   - interfaceRootAccess
	//   - interfaceProviderRootAccess
	Default accessChecker
}

const maxBodySize = 4 * 1024 * 1024 // 4MB

func (ac byActionAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, rec AuthzRecorder) *apiError {
	switch ac.Default.(type) {
	// TODO: If less strict interfaces are needed as defaults then
	// we might need to introduce access checker sorting so that the
	// default access checker is at least as strict as the strictest
	// action access checker.
	case rootAccess, interfaceRootAccess, interfaceProviderRootAccess:
	default:
		return InternalError("internal error: default access checker must have root-level access: got %T", ac.Default)
	}

	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		return BadRequest("unexpected content type: %q", contentType)
	}

	req := actionRequest{}

	bufSize := r.ContentLength
	// The value -1 indicates that the length is unknown.
	if bufSize > maxBodySize || bufSize == -1 {
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
		return BadRequest(err.Error())
	}
	if decoder.More() {
		return BadRequest("unexpected data after request body")
	}

	r.Body.Close()
	r.Body = io.NopCloser(buf)

	checker := ac.ByAction[req.Action]
	if checker == nil {
		return ac.Default.CheckAccess(d, r, ucred, user, rec)
	}

	return checker.CheckAccess(d, r, ucred, user, rec)
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
	path := filepath.Join(dirs.SnapMountDir, "snapd/*/usr/bin/*")
	if matched, err := filepath.Match(path, exe); err != nil {
		return false, err
	} else if matched {
		base := filepath.Base(exe)
		// Depending on the build variant of the snapd snap, the command could
		// either be $SNAPD_MOUNT/usr/bin/snap or $SNAPD_MOUNT/usr/bin/snap-fips
		if base == "snap" || base == "snap-fips" {
			return true, nil
		}

		return false, nil
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
