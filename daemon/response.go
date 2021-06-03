// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/systemd"
)

// ResponseType is the response type
type ResponseType string

// "there are three standard return types: Standard return value,
// Background operation, Error", each returning a JSON object with the
// following "type" field:
const (
	ResponseTypeSync  ResponseType = "sync"
	ResponseTypeAsync ResponseType = "async"
	ResponseTypeError ResponseType = "error"
)

// Response knows how to serve itself.
type Response interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// A StructuredResponse serializes itself to our standard JSON response format.
type StructuredResponse interface {
	Response

	JSON() *respJSON
}

// XXX drop resp
type resp = respJSON

// TODO This is being done in a rush to get the proper external
//      JSON representation in the API in time for the release.
//      The right code style takes a bit more work and unifies
//      these fields inside resp.
// Increment the counter if you read this: 43
type Meta struct {
	Sources           []string `json:"sources,omitempty"`
	SuggestedCurrency string   `json:"suggested-currency,omitempty"`
}

type respJSON struct {
	Type ResponseType `json:"type"`
	// Status is the HTTP status code.
	Status int `json:"status-code"`
	// StatusText is filled by the serving pipeline.
	StatusText string `json:"status"`
	// Result is a free-form optional result object.
	Result interface{} `json:"result"`
	// Change is the change ID for an async response.
	Change string `json:"change,omitempty"`
	*Meta
	// Maintenance...  are filled as needed by the serving pipeline.
	WarningTimestamp *time.Time   `json:"warning-timestamp,omitempty"`
	WarningCount     int          `json:"warning-count,omitempty"`
	Maintenance      *errorResult `json:"maintenance,omitempty"`
}

func (r *respJSON) JSON() *respJSON {
	return r
}

func maintenanceForRestartType(rst state.RestartType) *errorResult {
	e := &errorResult{}
	switch rst {
	case state.RestartSystem, state.RestartSystemNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemRestartMsg
		e.Value = map[string]interface{}{
			"op": "reboot",
		}
	case state.RestartSystemHaltNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemHaltMsg
		e.Value = map[string]interface{}{
			"op": "halt",
		}
	case state.RestartSystemPoweroffNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemPoweroffMsg
		e.Value = map[string]interface{}{
			"op": "poweroff",
		}
	case state.RestartDaemon:
		e.Kind = client.ErrorKindDaemonRestart
		e.Message = daemonRestartMsg
	case state.RestartSocket:
		e.Kind = client.ErrorKindDaemonRestart
		e.Message = socketRestartMsg
	case state.RestartUnset:
		// shouldn't happen, maintenance for unset type should just be nil
		panic("internal error: cannot marshal maintenance for RestartUnset")
	}
	return e
}

func (r *respJSON) addMaintenanceFromRestartType(rst state.RestartType) {
	if rst == state.RestartUnset {
		// nothing to do
		return
	}
	r.Maintenance = maintenanceForRestartType(rst)
}

func (r *respJSON) addWarningCount(count int, stamp time.Time) {
	if count == 0 {
		return
	}
	r.WarningCount = count
	r.WarningTimestamp = &stamp
}

func (r *respJSON) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	status := r.Status
	r.StatusText = http.StatusText(r.Status)
	bs, err := json.Marshal(r)
	if err != nil {
		logger.Noticef("cannot marshal %#v to JSON: %v", *r, err)
		bs = nil
		status = 500
	}

	hdr := w.Header()
	if r.Status == 202 || r.Status == 201 {
		if m, ok := r.Result.(map[string]interface{}); ok {
			if location, ok := m["resource"]; ok {
				if location, ok := location.(string); ok && location != "" {
					hdr.Set("Location", location)
				}
			}
		}
	}

	hdr.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(bs)
}

type errorValue interface{}

type errorResult struct {
	Message string `json:"message"` // note no omitempty
	// Kind is the error kind. See client/errors.go
	Kind  client.ErrorKind `json:"kind,omitempty"`
	Value errorValue       `json:"value,omitempty"`
}

// SyncResponse builds a "sync" response from the given result.
func SyncResponse(result interface{}, meta *Meta) Response {
	if rsp, ok := result.(Response); ok {
		return rsp
	}

	if err, ok := result.(error); ok {
		return InternalError("internal error: %v", err)
	}

	return &resp{
		Type:   ResponseTypeSync,
		Status: 200,
		Result: result,
		Meta:   meta,
	}
}

// AsyncResponse builds an "async" response for a created change
func AsyncResponse(result map[string]interface{}, change string) Response {
	return &respJSON{
		Type:   ResponseTypeAsync,
		Status: 202,
		Result: result,
		Change: change,
	}
}

// A snapStream ServeHTTP method streams a snap
type snapStream struct {
	SnapName string
	Filename string
	Info     *snap.DownloadInfo
	Token    string
	stream   io.ReadCloser
	resume   int64
}

// ServeHTTP from the Response interface
func (s *snapStream) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/octet-stream")
	snapname := fmt.Sprintf("attachment; filename=%s", s.Filename)
	hdr.Set("Content-Disposition", snapname)

	hdr.Set("Snap-Sha3-384", s.Info.Sha3_384)
	// can't set Content-Length when stream is nil as it breaks http clients
	// setting it also when there is a stream, for consistency
	hdr.Set("Snap-Length", strconv.FormatInt(s.Info.Size, 10))
	if s.Token != "" {
		hdr.Set("Snap-Download-Token", s.Token)
	}

	if s.stream == nil {
		// nothing to actually stream
		return
	}
	hdr.Set("Content-Length", strconv.FormatInt(s.Info.Size-s.resume, 10))

	if s.resume > 0 {
		hdr.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", s.resume, s.Info.Size-1, s.Info.Size))
		w.WriteHeader(206)
	}

	defer s.stream.Close()
	bytesCopied, err := io.Copy(w, s.stream)
	if err != nil {
		logger.Noticef("cannot copy snap %s (%#v) to the stream: %v", s.SnapName, s.Info, err)
		http.Error(w, err.Error(), 500)
	}
	if bytesCopied != s.Info.Size-s.resume {
		logger.Noticef("cannot copy snap %s (%#v) to the stream: bytes copied=%d, expected=%d", s.SnapName, s.Info, bytesCopied, s.Info.Size)
		http.Error(w, io.EOF.Error(), 502)
	}
}

// A snapshotExportResponse 's ServeHTTP method serves a specific snapshot ID
type snapshotExportResponse struct {
	*snapshotstate.SnapshotExport
	setID uint64
	st    *state.State
}

// ServeHTTP from the Response interface
func (s snapshotExportResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Length", strconv.FormatInt(s.Size(), 10))
	w.Header().Add("Content-Type", client.SnapshotExportMediaType)
	if err := s.StreamTo(w); err != nil {
		logger.Debugf("cannot export snapshot: %v", err)
	}
	s.Close()
	s.st.Lock()
	defer s.st.Unlock()
	snapshotstate.UnsetSnapshotOpInProgress(s.st, s.setID)
}

// A fileResponse 's ServeHTTP method serves the file
type fileResponse string

// ServeHTTP from the Response interface
func (f fileResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filename := fmt.Sprintf("attachment; filename=%s", filepath.Base(string(f)))
	w.Header().Add("Content-Disposition", filename)
	http.ServeFile(w, r, string(f))
}

// A journalLineReaderSeqResponse's ServeHTTP method reads lines (presumed to
// be, each one on its own, a JSON dump of a systemd.Log, as output by
// journalctl -o json) from an io.ReadCloser, loads that into a client.Log, and
// outputs the json dump of that, padded with RS and LF to make it a valid
// json-seq response.
//
// The reader is always closed when done (this is important for
// osutil.WatingStdoutPipe).
//
// Tip: “jq” knows how to read this; “jq --seq” both reads and writes this.
type journalLineReaderSeqResponse struct {
	io.ReadCloser
	follow bool
}

func (rr *journalLineReaderSeqResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json-seq")

	flusher, hasFlusher := w.(http.Flusher)

	var err error
	dec := json.NewDecoder(rr)
	writer := bufio.NewWriter(w)
	enc := json.NewEncoder(writer)
	for {
		var log systemd.Log
		if err = dec.Decode(&log); err != nil {
			break
		}

		writer.WriteByte(0x1E) // RS -- see ascii(7), and RFC7464

		// ignore the error...
		t, _ := log.Time()
		if err = enc.Encode(client.Log{
			Timestamp: t,
			Message:   log.Message(),
			SID:       log.SID(),
			PID:       log.PID(),
		}); err != nil {
			break
		}

		if rr.follow {
			if e := writer.Flush(); e != nil {
				break
			}
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
	if err != nil && err != io.EOF {
		fmt.Fprintf(writer, `\x1E{"error": %q}\n`, err)
		logger.Noticef("cannot stream response; problem reading: %v", err)
	}
	if err := writer.Flush(); err != nil {
		logger.Noticef("cannot stream response; problem writing: %v", err)
	}
	rr.Close()
}

type assertResponse struct {
	assertions []asserts.Assertion
	bundle     bool
}

// AssertResponse builds a response whose ServerHTTP method serves one or a bundle of assertions.
func AssertResponse(asserts []asserts.Assertion, bundle bool) Response {
	if len(asserts) > 1 {
		bundle = true
	}
	return &assertResponse{assertions: asserts, bundle: bundle}
}

func (ar assertResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := asserts.MediaType
	if ar.bundle {
		t = mime.FormatMediaType(t, map[string]string{"bundle": "y"})
	}
	w.Header().Set("Content-Type", t)
	w.Header().Set("X-Ubuntu-Assertions-Count", strconv.Itoa(len(ar.assertions)))
	w.WriteHeader(200)
	enc := asserts.NewEncoder(w)
	for _, a := range ar.assertions {
		err := enc.Encode(a)
		if err != nil {
			logger.Noticef("cannot write encoded assertion into response: %v", err)
			break

		}
	}
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

// SnapChangeConflict is an error responder used when an operation is
// conflicts with another change.
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
		case *snapstate.ChangeConflictError:
			return SnapChangeConflict(err)
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
		case net.Error:
			if err.Timeout() {
				kind = client.ErrorKindNetworkTimeout
			} else {
				handled = false
			}
		case *store.SnapActionError:
			// we only handle a few specific cases
			_, _, e := err.SingleOpError()
			if e != nil {
				// 👉😎👉
				return errToResponse(e, snaps, fallback, format)
			}
			handled = false
		default:
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
