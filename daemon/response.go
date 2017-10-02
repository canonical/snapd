// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/logger"
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

// Response knows how to serve itself, and how to find itself
type Response interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

type resp struct {
	Status int          `json:"status-code"`
	Type   ResponseType `json:"type"`
	Result interface{}  `json:"result,omitempty"`
	*Meta
	Maintenance *errorResult `json:"maintenance,omitempty"`
}

func (r *resp) transmitMaintenance(kind errorKind, message string) {
	r.Maintenance = &errorResult{
		Kind:    kind,
		Message: message,
	}
}

// TODO This is being done in a rush to get the proper external
//      JSON representation in the API in time for the release.
//      The right code style takes a bit more work and unifies
//      these fields inside resp.
type Meta struct {
	Sources           []string   `json:"sources,omitempty"`
	SuggestedCurrency string     `json:"suggested-currency,omitempty"`
	Change            string     `json:"change,omitempty"`
	WarningTimestamp  *time.Time `json:"warning-timestamp,omitempty"`
	WarningCount      int        `json:"warning-count,omitempty"`
}

type warningsSummariser interface {
	WarningsSummary() (int, time.Time)
}

func newMeta(w warningsSummariser) *Meta {
	count, stamp := w.WarningsSummary()
	meta := &Meta{WarningCount: count}
	if !stamp.IsZero() {
		meta.WarningTimestamp = &stamp
	}

	return meta
}

type respJSON struct {
	Type       ResponseType `json:"type"`
	Status     int          `json:"status-code"`
	StatusText string       `json:"status"`
	Result     interface{}  `json:"result"`
	*Meta
	Maintenance *errorResult `json:"maintenance,omitempty"`
}

func (r *resp) MarshalJSON() ([]byte, error) {
	return json.Marshal(respJSON{
		Type:        r.Type,
		Status:      r.Status,
		StatusText:  http.StatusText(r.Status),
		Result:      r.Result,
		Meta:        r.Meta,
		Maintenance: r.Maintenance,
	})
}

func (r *resp) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	status := r.Status
	bs, err := r.MarshalJSON()
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

type errorKind string

const (
	errorKindTwoFactorRequired = errorKind("two-factor-required")
	errorKindTwoFactorFailed   = errorKind("two-factor-failed")
	errorKindLoginRequired     = errorKind("login-required")
	errorKindInvalidAuthData   = errorKind("invalid-auth-data")
	errorKindTermsNotAccepted  = errorKind("terms-not-accepted")
	errorKindNoPaymentMethods  = errorKind("no-payment-methods")
	errorKindPaymentDeclined   = errorKind("payment-declined")
	errorKindPasswordPolicy    = errorKind("password-policy")

	errorKindSnapAlreadyInstalled  = errorKind("snap-already-installed")
	errorKindSnapNotInstalled      = errorKind("snap-not-installed")
	errorKindSnapNotFound          = errorKind("snap-not-found")
	errorKindAppNotFound           = errorKind("app-not-found")
	errorKindSnapLocal             = errorKind("snap-local")
	errorKindSnapNoUpdateAvailable = errorKind("snap-no-update-available")

	errorKindSnapRevisionNotAvailable     = errorKind("snap-revision-not-available")
	errorKindSnapChannelNotAvailable      = errorKind("snap-channel-not-available")
	errorKindSnapArchitectureNotAvailable = errorKind("snap-architecture-not-available")

	errorKindNotSnap = errorKind("snap-not-a-snap")

	errorKindSnapNeedsDevMode       = errorKind("snap-needs-devmode")
	errorKindSnapNeedsClassic       = errorKind("snap-needs-classic")
	errorKindSnapNeedsClassicSystem = errorKind("snap-needs-classic-system")

	errorKindBadQuery = errorKind("bad-query")

	errorKindNetworkTimeout      = errorKind("network-timeout")
	errorKindInterfacesUnchanged = errorKind("interfaces-unchanged")

	errorKindConfigNoSuchOption = errorKind("option-not-found")

	errorKindDaemonRestart = errorKind("daemon-restart")
	errorKindSystemRestart = errorKind("system-restart")
)

type errorValue interface{}

type errorResult struct {
	Message string     `json:"message"` // note no omitempty
	Kind    errorKind  `json:"kind,omitempty"`
	Value   errorValue `json:"value,omitempty"`
}

// SyncResponse builds a "sync" response from the given result.
func SyncResponse(result interface{}, meta *Meta) Response {
	if err, ok := result.(error); ok {
		return InternalError("internal error: %v", err)
	}

	if rsp, ok := result.(Response); ok {
		return rsp
	}

	return &resp{
		Type:   ResponseTypeSync,
		Status: 200,
		Result: result,
		Meta:   meta,
	}
}

// AsyncResponse builds an "async" response from the given *Task
func AsyncResponse(result map[string]interface{}, meta *Meta) Response {
	return &resp{
		Type:   ResponseTypeAsync,
		Status: 202,
		Result: result,
		Meta:   meta,
	}
}

// makeErrorResponder builds an errorResponder from the given error status.
func makeErrorResponder(status int) errorResponder {
	return func(format string, v ...interface{}) Response {
		res := &errorResult{
			Message: fmt.Sprintf(format, v...),
		}
		if status == 401 {
			res.Kind = errorKindLoginRequired
		}
		return &resp{
			Type:   ResponseTypeError,
			Result: res,
			Status: status,
		}
	}
}

// A FileResponse 's ServeHTTP method serves the file
type FileResponse string

// ServeHTTP from the Response interface
func (f FileResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

// InterfacesUnchanged is an error responder used when an operation
// that would normally change interfaces finds it has nothing to do
func InterfacesUnchanged(format string, v ...interface{}) Response {
	res := &errorResult{
		Message: fmt.Sprintf(format, v...),
		Kind:    errorKindInterfacesUnchanged,
	}
	return &resp{
		Type:   ResponseTypeError,
		Result: res,
		Status: 400,
	}
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
type errorResponder func(string, ...interface{}) Response

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

// SnapNotFound is an error responder used when an operation is
// requested on a snap that doesn't exist.
func SnapNotFound(snapName string, err error) Response {
	return &resp{
		Type: ResponseTypeError,
		Result: &errorResult{
			Message: err.Error(),
			Kind:    errorKindSnapNotFound,
			Value:   snapName,
		},
		Status: 404,
	}
}

// SnapRevisionNotAvailable is an error responder used when an
// operation is requested for which no revivision can be found
// in the given context (e.g. request an install from a stable
// channel when this channel is empty).
func SnapRevisionNotAvailable(snapName string, rnaErr *store.RevisionNotAvailableError) Response {
	var value interface{} = snapName
	kind := errorKindSnapRevisionNotAvailable
	msg := rnaErr.Error()
	if len(rnaErr.Releases) != 0 && rnaErr.Channel != "" {
		thisArch := arch.UbuntuArchitecture()
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
			kind = errorKindSnapChannelNotAvailable
			msg = "no snap revision on specified channel"
		} else {
			kind = errorKindSnapArchitectureNotAvailable
			msg = "no snap revision on specified architecture"
		}
		values["releases"] = releases
		value = values
	}
	return &resp{
		Type: ResponseTypeError,
		Result: &errorResult{
			Message: msg,
			Kind:    kind,
			Value:   value,
		},
		Status: 404,
	}
}

// AppNotFound is an error responder used when an operation is
// requested on a app that doesn't exist.
func AppNotFound(format string, v ...interface{}) Response {
	res := &errorResult{
		Message: fmt.Sprintf(format, v...),
		Kind:    errorKindAppNotFound,
	}
	return &resp{
		Type:   ResponseTypeError,
		Result: res,
		Status: 404,
	}
}
