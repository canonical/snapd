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
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
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

// respJSON represents our standard JSON response format.
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
	// Sources is used in find responses.
	Sources []string `json:"sources,omitempty"`
	// XXX SuggestedCurrency is part of unsupported paid snap code.
	SuggestedCurrency string `json:"suggested-currency,omitempty"`
	// Maintenance...  are filled as needed by the serving pipeline.
	WarningTimestamp *time.Time   `json:"warning-timestamp,omitempty"`
	WarningCount     int          `json:"warning-count,omitempty"`
	Maintenance      *errorResult `json:"maintenance,omitempty"`
}

func (r *respJSON) JSON() *respJSON {
	return r
}

func maintenanceForRestartType(rst restart.RestartType) *errorResult {
	e := &errorResult{}
	switch rst {
	case restart.RestartSystem, restart.RestartSystemNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemRestartMsg
		e.Value = map[string]interface{}{
			"op": "reboot",
		}
	case restart.RestartSystemHaltNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemHaltMsg
		e.Value = map[string]interface{}{
			"op": "halt",
		}
	case restart.RestartSystemPoweroffNow:
		e.Kind = client.ErrorKindSystemRestart
		e.Message = systemPoweroffMsg
		e.Value = map[string]interface{}{
			"op": "poweroff",
		}
	case restart.RestartDaemon:
		e.Kind = client.ErrorKindDaemonRestart
		e.Message = daemonRestartMsg
	case restart.RestartSocket:
		e.Kind = client.ErrorKindDaemonRestart
		e.Message = socketRestartMsg
	case restart.RestartUnset:
		// shouldn't happen, maintenance for unset type should just be nil
		panic("internal error: cannot marshal maintenance for RestartUnset")
	}
	return e
}

func (r *respJSON) addMaintenanceFromRestartType(rst restart.RestartType) {
	if rst == restart.RestartUnset {
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
	bs := mylog.Check2(json.Marshal(r))

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

// SyncResponse builds a "sync" response from the given result.
func SyncResponse(result interface{}) Response {
	if rsp, ok := result.(Response); ok {
		return rsp
	}

	if err, ok := result.(error); ok {
		return InternalError("internal error: %v", err)
	}

	return &respJSON{
		Type:   ResponseTypeSync,
		Status: 200,
		Result: result,
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
	bytesCopied := mylog.Check2(io.Copy(w, s.stream))

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
	mylog.Check(s.StreamTo(w))

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

	dec := json.NewDecoder(rr)
	writer := bufio.NewWriter(w)
	enc := json.NewEncoder(writer)
	for {
		var log systemd.Log
		mylog.Check(dec.Decode(&log))

		writer.WriteByte(0x1E) // RS -- see ascii(7), and RFC7464

		// ignore the error...
		t, _ := log.Time()
		mylog.Check(enc.Encode(client.Log{
			Timestamp: t,
			Message:   log.Message(),
			SID:       log.SID(),
			PID:       log.PID(),
		}))

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
	mylog.Check(writer.Flush())

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
		mylog.Check(enc.Encode(a))
	}
}
