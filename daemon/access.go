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
// CheckAccess always produces an accessDecision carrying the verdict to apply,
// the per-stage authorization audit data, the access level at which the request
// was evaluated, and the audit name of the checker that actually evaluated
// the request.
type accessChecker interface {
	CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision
}

// accessCheckerName identifies a concrete accessChecker in audit events.
// Values must remain stable.
type accessCheckerName string

const (
	// accessCheckerOpen: any peer on snapd.socket; no authentication.
	accessCheckerOpen accessCheckerName = "open"
	// accessCheckerSnap: any peer on snapd-snap.socket only; no
	// authentication beyond the socket restriction.
	accessCheckerSnap accessCheckerName = "open-plus-snap-socket-only"
	// accessCheckerInterfaceOpen: any peer on snapd.socket, or a snap
	// on snapd-snap.socket that is on the plug side of an active
	// connection of one of the required interfaces.
	accessCheckerInterfaceOpen accessCheckerName = "open-plus-plug-of-required-iface"
	// accessCheckerAuthenticated: peer on snapd.socket authenticated
	// via snapd user macaroon, or root uid, or (optionally) polkit.
	accessCheckerAuthenticated accessCheckerName = "authenticated"
	// accessCheckerInterfaceAuthenticated: like authenticated, but
	// also allows a snap on snapd-snap.socket that is on the plug
	// side of an active connection of one of the required interfaces.
	accessCheckerInterfaceAuthenticated accessCheckerName = "authenticated-plus-plug-of-required-iface"
	// accessCheckerRoot: peer on snapd.socket with root uid.
	accessCheckerRoot accessCheckerName = "root"
	// accessCheckerInterfaceRoot: root uid or (optionally) polkit on
	// snapd.socket, or a snap on snapd-snap.socket that is on the
	// plug side of an active connection of one of the required
	// interfaces.
	accessCheckerInterfaceRoot accessCheckerName = "root-plus-plug-of-required-iface"
	// accessCheckerInterfaceProviderRoot: root uid on snapd.socket,
	// or a snap on snapd-snap.socket that is on the slot (provider)
	// side of an active connection of one of the required interfaces.
	accessCheckerInterfaceProviderRoot accessCheckerName = "root-plus-slot-of-required-iface"
)

// accessDecision is the structured result of an authorization check.
type accessDecision struct {
	// Verdict carries the HTTP response to serve when the request is
	// denied (nil when access is granted).
	Verdict *apiError
	// Checks records what each authorization stage evaluated to.
	Checks seclog.AuthzChecks
	// Level is the access level the request was evaluated at, or
	// accessLevelNotEvaluated when no authorization stage ran
	// (e.g. on dispatch errors in byActionAccess).
	Level accessLevel
	// CheckerName is the audit name of the checker that produced this
	// decision. For a multiplexer like byActionAccess it is the delegate's
	// name, not the multiplexer's; it is empty for dispatch-error decisions.
	CheckerName accessCheckerName
}

// Denied reports whether the request was rejected by the access checker.
func (d accessDecision) Denied() bool { return d.Verdict != nil }

// notEvaluated builds a decision for dispatch-only failures where no
// authorization stage ran. The returned decision has no per-stage check
// data, an empty checker name, and a level of accessLevelNotEvaluated;
// audit emission is suppressed for such decisions (see isAdministrativeAccess).
func notEvaluated(rspe *apiError) accessDecision {
	return accessDecision{
		Verdict: rspe,
		Checks:  seclog.NewAuthzChecks(),
		Level:   accessLevelNotEvaluated,
	}
}

type accessLevel string

const (
	accessLevelRoot          accessLevel = "root"
	accessLevelAuthenticated accessLevel = "authenticated"
	accessLevelOpen          accessLevel = "open"
	// accessLevelNotEvaluated is returned when byActionAccess fails before
	// delegation; no authorization checks ran so the level is unknown.
	accessLevelNotEvaluated accessLevel = "not-evaluated"
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

// checkPrerequisites runs prerequisite authorization checks that must all pass
// before access level checks are evaluated. These include peer credentials,
// socket restrictions, and interface requirements.
func checkPrerequisites(d *Daemon, r *http.Request, ucred *ucrednet, checks *seclog.AuthzChecks, opts accessOptions) *apiError {
	// Mark applicable checks as AuthzNotReached (will become Pass/Fail during evaluation).
	// AccessOptions is intentionally not reset here: it is set by the caller after
	// opts.validate() has run.
	checks.PeerCreds = seclog.AuthzNotReached
	if len(opts.Sockets) != 0 {
		checks.Socket = seclog.AuthzNotReached
	}
	if opts.InterfaceAccess != nil {
		checks.Interface = seclog.AuthzNotReached
	}

	// Peer credentials check
	if ucred == nil {
		checks.PeerCreds = seclog.AuthzFail
		return Forbidden("access denied")
	}
	checks.PeerCreds = seclog.AuthzPass

	// Socket check
	if len(opts.Sockets) != 0 {
		if !strutil.ListContains(opts.Sockets, ucred.Socket) {
			checks.Socket = seclog.AuthzFail
			return Forbidden("access denied")
		}
		checks.Socket = seclog.AuthzPass
	}

	// Interface check
	if opts.InterfaceAccess != nil {
		rspe := requireInterfaceApiAccess(d, r, ucred, *opts.InterfaceAccess)
		if rspe != nil {
			checks.Interface = seclog.AuthzFail
			return rspe
		}
		checks.Interface = seclog.AuthzPass
	}

	return nil
}

// checkAccessLevelAuthorization determines if the user is authorized at the
// required access level by evaluating multiple authorization methods. Any single
// method can grant access: open access, user authentication, root UID, or polkit.
func checkAccessLevelAuthorization(r *http.Request, ucred *ucrednet, user *auth.UserState, checks *seclog.AuthzChecks, opts accessOptions) *apiError {
	// Mark applicable checks as AuthzNotReached (will become Pass/Fail during evaluation).
	// Checks that do not apply to the current access level remain AuthzNotApplicable.
	switch opts.AccessLevel {
	case accessLevelOpen:
		checks.OpenAccess = seclog.AuthzNotReached
	case accessLevelAuthenticated:
		checks.UserAuth = seclog.AuthzNotReached
		checks.Root = seclog.AuthzNotReached
		if opts.PolkitAction != "" {
			checks.Polkit = seclog.AuthzNotReached
		}
	case accessLevelRoot:
		checks.Root = seclog.AuthzNotReached
		if opts.PolkitAction != "" {
			checks.Polkit = seclog.AuthzNotReached
		}
	}

	if opts.AccessLevel == accessLevelOpen {
		checks.OpenAccess = seclog.AuthzPass
		return nil
	}

	// Snapd local macaroon check - snapd user authentication
	if opts.AccessLevel == accessLevelAuthenticated && user != nil {
		checks.UserAuth = seclog.AuthzPass
		return nil
	}
	if opts.AccessLevel == accessLevelAuthenticated {
		checks.UserAuth = seclog.AuthzFail
		// Even though snapd user authentication failed,
		// we still accept root or polkit authorization as
		// permitted alternatives.
	}

	// System UID based root check for privileged actions.
	if ucred.Uid == 0 {
		checks.Root = seclog.AuthzPass
		return nil
	}
	checks.Root = seclog.AuthzFail

	// Polkit check - system user authentication/authorization (policy dependant) for privileged actions; last because it may prompt the user.
	if opts.PolkitAction != "" {
		rspe := checkPolkitAction(r, ucred, opts.PolkitAction)
		if rspe == nil {
			checks.Polkit = seclog.AuthzPass
			return nil
		}
		checks.Polkit = seclog.AuthzFail
		return rspe
	}

	// If we reach here, all access level checks failed
	if opts.AccessLevel == accessLevelAuthenticated || opts.InterfaceAccess != nil {
		// At this point, if authenticated access and/or interface access is required,
		// it means that accessLevelRoot (root or polkit) was either implicitly or
		// explicitly required and failed
		return Unauthorized("access denied")
	}
	// Explicitly required accessLevelRoot, via system UID or polkit checks was not authorized.
	return Forbidden("access denied")
}

func checkAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState, opts accessOptions, name accessCheckerName) accessDecision {
	// Level starts at accessLevelNotEvaluated and is upgraded to the requested
	// opts.AccessLevel only after opts.validate() succeeds, so dispatch-style
	// failures (invalid AccessLevel/Sockets) yield a decision that is
	// structurally identical to byActionAccess dispatch errors: no level, no
	// audit emission.
	dec := accessDecision{
		Checks:      seclog.NewAuthzChecks(),
		Level:       accessLevelNotEvaluated,
		CheckerName: name,
	}

	if err := opts.validate(); err != nil {
		dec.Checks.AccessOptions = seclog.AuthzFail
		dec.Verdict = InternalError(err.Error())
		return dec
	}
	dec.Checks.AccessOptions = seclog.AuthzPass
	dec.Level = opts.AccessLevel

	if err := checkPrerequisites(d, r, ucred, &dec.Checks, opts); err != nil {
		dec.Verdict = err
		return dec
	}

	if err := checkAccessLevelAuthorization(r, ucred, user, &dec.Checks, opts); err != nil {
		dec.Verdict = err
		return dec
	}

	return dec
}

// isAdministrativeAccess reports whether authz audit events should be emitted:
// the intended access level is authenticated or root. Dispatch-only failures
// return accessLevelNotEvaluated and are excluded.
func isAdministrativeAccess(level accessLevel) bool {
	return level == accessLevelAuthenticated || level == accessLevelRoot
}

// openAccess allows requests without authentication, provided they
// have peer credentials and were not received on snapd-snap.socket
type openAccess struct{}

func (ac openAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel: accessLevelOpen,
		Sockets:     []string{dirs.SnapdSocket},
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerOpen)
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

func (ac authenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel:  accessLevelAuthenticated,
		Sockets:      []string{dirs.SnapdSocket},
		PolkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerAuthenticated)
}

// rootAccess allows requests from the root uid, provided they
// were not received on snapd-snap.socket
type rootAccess struct{}

func (ac rootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel: accessLevelRoot,
		Sockets:     []string{dirs.SnapdSocket},
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerRoot)
}

// snapAccess allows requests from the snapd-snap.socket only.
type snapAccess struct{}

func (ac snapAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel: accessLevelOpen,
		Sockets:     []string{dirs.SnapSocket},
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerSnap)
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

func (ac interfaceOpenAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel: accessLevelOpen,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerInterfaceOpen)
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

func (ac interfaceAuthenticatedAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel: accessLevelAuthenticated,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
		PolkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerInterfaceAuthenticated)
}

// interfaceProviderRootAccess behaves like rootAccess, but also allows requests
// over snapd-snap.socket for snaps that have a connection of specific interface
// and are present on the slot side of that connection.
type interfaceProviderRootAccess struct {
	Interfaces []string
}

func (ac interfaceProviderRootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel: accessLevelRoot,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Slot:       true,
		},
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerInterfaceProviderRoot)
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

func (ac interfaceRootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	opts := accessOptions{
		AccessLevel: accessLevelRoot,
		Sockets:     []string{dirs.SnapdSocket, dirs.SnapSocket},
		InterfaceAccess: &interfaceAccessReqs{
			Interfaces: ac.Interfaces,
			Plug:       true,
		},
		PolkitAction: ac.Polkit,
	}
	return checkAccess(d, r, ucred, user, opts, accessCheckerInterfaceRoot)
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

// CheckAccess routes by JSON "action" to a delegated checker and returns
// the delegate's decision unchanged. The delegate sets CheckerName so audit
// records the actual evaluator, not the multiplexer. Failures before
// delegation are dispatch errors (BadRequest/InternalError), not
// authorization; the returned decision has empty AuthzChecks,
// accessLevelNotEvaluated, and no CheckerName, and is excluded from authz
// audit emission.
func (ac byActionAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *auth.UserState) accessDecision {
	switch ac.Default.(type) {
	// TODO: If less strict interfaces are needed as defaults then
	// we might need to introduce access checker sorting so that the
	// default access checker is at least as strict as the strictest
	// action access checker.
	case rootAccess, interfaceRootAccess, interfaceProviderRootAccess:
	default:
		return notEvaluated(InternalError("internal error: default access checker must have root-level access: got %T", ac.Default))
	}

	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		return notEvaluated(BadRequest("unexpected content type: %q", contentType))
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
			return notEvaluated(BadRequest("body size limit exceeded"))
		}
		// Content type is JSON, but it's invalid
		return notEvaluated(BadRequest(err.Error()))
	}
	if decoder.More() {
		return notEvaluated(BadRequest("unexpected data after request body"))
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
