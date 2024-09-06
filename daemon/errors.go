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
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// apiError represents an error meant for returning to the client.
// It can serialize itself to our standard JSON response format.
type apiError struct {
	// Status is the error HTTP status code.
	Status  int
	Message string
	// Kind is the error kind. See client/errors.go
	Kind  client.ErrorKind
	Value errorValue
}

func (ae *apiError) Error() string {
	kindOrStatus := "api"
	if ae.Kind != "" {
		kindOrStatus = fmt.Sprintf("api: %s", ae.Kind)
	} else if ae.Status != 400 {
		kindOrStatus = fmt.Sprintf("api %d", ae.Status)
	}
	return fmt.Sprintf("%s (%s)", ae.Message, kindOrStatus)
}

func (ae *apiError) JSON() *respJSON {
	return &respJSON{
		Status: ae.Status,
		Type:   ResponseTypeError,
		Result: &errorResult{
			Message: ae.Message,
			Kind:    ae.Kind,
			Value:   ae.Value,
		},
	}
}

func (ae *apiError) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ae.JSON().ServeHTTP(w, r)
}

// check it implements StructuredResponse
var _ StructuredResponse = (*apiError)(nil)

type errorValue interface{}

type errorResult struct {
	Message string `json:"message"` // note no omitempty
	// Kind is the error kind. See client/errors.go
	Kind  client.ErrorKind `json:"kind,omitempty"`
	Value errorValue       `json:"value,omitempty"`
}

// errorResponder is a callable that produces an error Response.
// e.g., InternalError("something broke: %v", err), etc.
type errorResponder func(string, ...interface{}) *apiError

// makeErrorResponder builds an errorResponder from the given error status.
func makeErrorResponder(status int) errorResponder {
	return func(format string, v ...interface{}) *apiError {
		var msg string
		if len(v) == 0 {
			msg = format
		} else {
			msg = fmt.Sprintf(format, v...)
		}
		var kind client.ErrorKind
		if status == 401 || status == 403 {
			kind = client.ErrorKindLoginRequired
		}
		return &apiError{
			Status:  status,
			Message: msg,
			Kind:    kind,
		}
	}
}

// standard error responses
var (
	Unauthorized     = makeErrorResponder(401)
	NotFound         = makeErrorResponder(404)
	BadRequest       = makeErrorResponder(400)
	MethodNotAllowed = makeErrorResponder(405)
	InternalError    = makeErrorResponder(500)
	NotImplemented   = makeErrorResponder(501)
	Forbidden        = makeErrorResponder(403)
	Conflict         = makeErrorResponder(409)
)

// BadQuery is an error responder used when a bad query was
// provided to the store.
func BadQuery() *apiError {
	return &apiError{
		Status:  400,
		Message: "bad query",
		Kind:    client.ErrorKindBadQuery,
	}
}

// SnapNotFound is an error responder used when an operation is
// requested on a snap that doesn't exist.
func SnapNotFound(snapName string, err error) *apiError {
	return &apiError{
		Status:  404,
		Message: err.Error(),
		Kind:    client.ErrorKindSnapNotFound,
		Value:   snapName,
	}
}

// SnapNotInstalled is an error responder used when an operation is
// requested on a snap that is not in the system but expected to be.
func SnapNotInstalled(snapName string, err error) *apiError {
	return &apiError{
		Status:  400,
		Message: err.Error(),
		Kind:    client.ErrorKindSnapNotInstalled,
		Value:   snapName,
	}
}

// SnapRevisionNotAvailable is an error responder used when an
// operation is requested for which no revivision can be found
// in the given context (e.g. request an install from a stable
// channel when this channel is empty).
func SnapRevisionNotAvailable(snapName string, rnaErr *store.RevisionNotAvailableError) *apiError {
	var value interface{} = snapName
	kind := client.ErrorKindSnapRevisionNotAvailable
	msg := rnaErr.Error()
	if len(rnaErr.Releases) != 0 && rnaErr.Channel != "" {
		thisArch := arch.DpkgArchitecture()
		values := map[string]interface{}{
			"snap-name":    snapName,
			"action":       rnaErr.Action,
			"channel":      rnaErr.Channel,
			"architecture": thisArch,
		}
		archOK := false
		releases := make([]map[string]interface{}, 0, len(rnaErr.Releases))
		for _, c := range rnaErr.Releases {
			if c.Architecture == thisArch {
				archOK = true
			}
			releases = append(releases, map[string]interface{}{
				"architecture": c.Architecture,
				"channel":      c.Name,
			})
		}
		// we return all available releases (arch x channel)
		// as reported in the store error, but we hint with
		// the error kind whether there was anything at all
		// available for this architecture
		if archOK {
			kind = client.ErrorKindSnapChannelNotAvailable
			msg = "no snap revision on specified channel"
		} else {
			kind = client.ErrorKindSnapArchitectureNotAvailable
			msg = "no snap revision on specified architecture"
		}
		values["releases"] = releases
		value = values
	}
	return &apiError{
		Status:  404,
		Message: msg,
		Kind:    kind,
		Value:   value,
	}
}

// SnapChangeConflict is an error responder used when an operation would
// conflict with another ongoing change.
func SnapChangeConflict(cce *snapstate.ChangeConflictError) *apiError {
	value := map[string]interface{}{}
	if cce.Snap != "" {
		value["snap-name"] = cce.Snap
	}
	if cce.ChangeKind != "" {
		value["change-kind"] = cce.ChangeKind
	}

	return &apiError{
		Status:  409,
		Message: cce.Error(),
		Kind:    client.ErrorKindSnapChangeConflict,
		Value:   value,
	}
}

// QuotaChangeConflict is an error responder used when an operation would
// conflict with another ongoing change.
func QuotaChangeConflict(qce *servicestate.QuotaChangeConflictError) *apiError {
	value := map[string]interface{}{}
	if qce.Quota != "" {
		value["quota-name"] = qce.Quota
	}
	if qce.ChangeKind != "" {
		value["change-kind"] = qce.ChangeKind
	}

	return &apiError{
		Status:  409,
		Message: qce.Error(),
		Kind:    client.ErrorKindQuotaChangeConflict,
		Value:   value,
	}
}

// InsufficientSpace is an error responder used when an operation cannot
// be performed due to low disk space.
func InsufficientSpace(dserr *snapstate.InsufficientSpaceError) *apiError {
	value := map[string]interface{}{}
	if len(dserr.Snaps) > 0 {
		value["snap-names"] = dserr.Snaps
	}
	if dserr.ChangeKind != "" {
		value["change-kind"] = dserr.ChangeKind
	}
	return &apiError{
		Status:  507,
		Message: dserr.Error(),
		Kind:    client.ErrorKindInsufficientDiskSpace,
		Value:   value,
	}
}

// AppNotFound is an error responder used when an operation is
// requested on a app that doesn't exist.
func AppNotFound(format string, v ...interface{}) *apiError {
	return &apiError{
		Status:  404,
		Message: fmt.Sprintf(format, v...),
		Kind:    client.ErrorKindAppNotFound,
	}
}

// AuthCancelled is an error responder used when a user cancelled
// the auth process.
func AuthCancelled(format string, v ...interface{}) *apiError {
	return &apiError{
		Status:  403,
		Message: fmt.Sprintf(format, v...),
		Kind:    client.ErrorKindAuthCancelled,
	}
}

// InterfacesUnchanged is an error responder used when an operation
// that would normally change interfaces finds it has nothing to do
func InterfacesUnchanged(format string, v ...interface{}) *apiError {
	return &apiError{
		Status:  400,
		Message: fmt.Sprintf(format, v...),
		Kind:    client.ErrorKindInterfacesUnchanged,
	}
}

func errToResponse(err error, snaps []string, fallback errorResponder, format string, v ...interface{}) *apiError {
	var kind client.ErrorKind
	var snapName string

	switch err {
	case store.ErrSnapNotFound:
		switch len(snaps) {
		case 1:
			return SnapNotFound(snaps[0], err)
		// store.ErrSnapNotFound should only be returned for individual
		// snap queries; in all other cases something's wrong
		case 0:
			return InternalError("store.SnapNotFound with no snap given")
		default:
			return InternalError("store.SnapNotFound with %d snaps", len(snaps))
		}
	case store.ErrNoUpdateAvailable:
		kind = client.ErrorKindSnapNoUpdateAvailable
	case store.ErrLocalSnap:
		kind = client.ErrorKindSnapLocal
	default:
		handled := true
		switch err := err.(type) {
		case *store.RevisionNotAvailableError:
			// store.ErrRevisionNotAvailable should only be returned for
			// individual snap queries; in all other cases something's wrong
			switch len(snaps) {
			case 1:
				return SnapRevisionNotAvailable(snaps[0], err)
			case 0:
				return InternalError("store.RevisionNotAvailable with no snap given")
			default:
				return InternalError("store.RevisionNotAvailable with %d snaps", len(snaps))
			}
		case *snap.AlreadyInstalledError:
			kind = client.ErrorKindSnapAlreadyInstalled
			snapName = err.Snap
		case *snap.NotInstalledError:
			kind = client.ErrorKindSnapNotInstalled
			snapName = err.Snap
		case *servicestate.QuotaChangeConflictError:
			return QuotaChangeConflict(err)
		case *snapstate.SnapNeedsDevModeError:
			kind = client.ErrorKindSnapNeedsDevMode
			snapName = err.Snap
		case *snapstate.SnapNeedsClassicError:
			kind = client.ErrorKindSnapNeedsClassic
			snapName = err.Snap
		case *snapstate.SnapNeedsClassicSystemError:
			kind = client.ErrorKindSnapNeedsClassicSystem
			snapName = err.Snap
		case *snapstate.SnapNotClassicError:
			kind = client.ErrorKindSnapNotClassic
			snapName = err.Snap
		case *snapstate.InsufficientSpaceError:
			return InsufficientSpace(err)
		case *snapstate.BusySnapError:
			kind = client.ErrorKindBusySnap
		case net.Error:
			if err.Timeout() {
				kind = client.ErrorKindNetworkTimeout
			} else {
				handled = false
			}
		case *store.SnapActionError:
			// we only handle a few specific cases
			_, name, e := err.SingleOpError()
			if e != nil {
				// 👉😎👉
				return errToResponse(e, []string{name}, fallback, format)
			}
			handled = false
		default:
			// support wrapped errors
			switch {
			case errors.Is(err, &snapstate.ChangeConflictError{}):
				var conflErr *snapstate.ChangeConflictError
				if errors.As(err, &conflErr) {
					return SnapChangeConflict(conflErr)
				}
			}

			handled = false
		}

		if !handled {
			v = append(v, err)
			return fallback(format, v...)
		}
	}

	return &apiError{
		Status:  400,
		Message: err.Error(),
		Kind:    kind,
		Value:   snapName,
	}
}
