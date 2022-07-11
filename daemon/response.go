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
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

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
	readers []io.ReadCloser
	follow  bool
}

var errCannotWriteToClient = errors.New("cannot write data, client may have hung up unexpectedly")

// logReader is a helper function which should be spawned as a go-routine.
// The objective for the logReader is to have one of these per log-source
// as reading from a log-source is a blocking operation. They will read from
// the io.ReadCloser until an error occurs or the stop channel is closed.
func (rr *journalLineReaderSeqResponse) logReader(r io.ReadCloser, c chan systemd.Log, e chan error, stopCh chan struct{}) {
	defer r.Close()
	decoder := json.NewDecoder(r)

	safeSendLog := func(log systemd.Log) bool {
		select {
		case <-stopCh:
			e <- io.EOF
			return true
		case c <- log:
		}
		return false
	}

	for {
		var log systemd.Log

		// This will always cause an error sooner or later because of an
		// io.EOF. This means we can rely on this being our termination
		// condition for the read loop, and then do the error handling in
		// the main go routine.
		if err := decoder.Decode(&log); err != nil {
			e <- err
			break
		}

		if closed := safeSendLog(log); closed {
			break
		}
	}
}

// readAllLogs is a helper function to read all available logs.
// It will read logs until all log sources are exhausted (all readers report io.EOF or
// report an error).
// This function is designed for the non-following case, where we can
// expect all log readers to report io.EOF immediately after writing their backlogs.
func (rr *journalLineReaderSeqResponse) readAllLogs(logCh chan systemd.Log, errCh chan error) []systemd.Log {
	var logReadersDone int
	var logs []systemd.Log
	var terminate bool
	for !terminate {
		select {
		case log := <-logCh:
			logs = append(logs, log)
		case err := <-errCh:
			if err != io.EOF {
				logger.Noticef("cannot decode systemd log: %v", err)
			}
			logReadersDone++
		}

		// Make sure we don't terminate early even if all readers done
		if logReadersDone == len(rr.readers) && len(logCh) == 0 {
			terminate = true
		}
	}
	return logs
}

// readFollowBacklog is a best-effort function to read the backlogs of the available
// log sources. It will continuously batch read from the log channels for as long as
// we read logs that predates the start of the follow action. Only when reading a batch
// of logs (or none at all) in which all are newer than the start timestamp, we assume the
// end of the backlog, and return the combined read logs.
func (rr *journalLineReaderSeqResponse) readFollowBacklog(logCh chan systemd.Log, errCh chan error) []systemd.Log {
	var backLog []systemd.Log
	currentTime := time.Now()

	filterLogs := func(logs []systemd.Log) bool {
		var readAgain bool
		// If the logs contain a log with a timestamp before the
		// current timestamp, then we are still reading backlog
		// logs, and we should read again to ensure there are no
		// more backlogs
		for _, l := range logs {
			t, _ := l.Time()
			if t.Before(currentTime) {
				readAgain = true
			}
		}
		backLog = append(backLog, logs...)
		return readAgain
	}

	for {
		var logs []systemd.Log

		// The batch interval will be set at 50ms (arbitrary) from which a new
		// backlog entry can arrive. If we go 50ms without a new backlog entry,
		// then we assume we have reached the end of the backlog.
		timeout := time.After(time.Millisecond * 50)
		var terminate bool
		for !terminate {
			select {
			case log := <-logCh:
				logs = append(logs, log)
			case <-timeout:
				terminate = true
			}
		}

		if !filterLogs(logs) {
			break
		}
	}
	return backLog
}

// readFollowMode contains the logic for reading the log-channel in follow-mode.
// After reading the backlog, we go into a simpler read loop which consists of just
// reading at a fixed interval. The idea is to allow for all log sources to get their
// logs into the log channel, and then be able to read them all in a single go and correctly
// sort them by their timestamp.
func (rr *journalLineReaderSeqResponse) readFollowMode(writeLogs func(logs []systemd.Log) error, logCh chan systemd.Log, errCh chan error) (int, error) {
	var logReadersDone int
	for {
		var logs []systemd.Log
		timeout := time.After(time.Millisecond * 25)
		var terminate bool
		for !terminate {
			select {
			case log := <-logCh:
				logs = append(logs, log)
			case err := <-errCh:
				if err != io.EOF {
					logger.Noticef("cannot decode systemd log: %v", err)
				}
				logReadersDone++
			case <-timeout:
				terminate = true
			}
		}

		if err := writeLogs(logs); err != nil {
			return logReadersDone, err
		}

		if logReadersDone == len(rr.readers) {
			break
		}
	}
	return logReadersDone, nil
}

func (rr *journalLineReaderSeqResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json-seq")

	flusher, hasFlusher := w.(http.Flusher)
	writer := bufio.NewWriter(w)
	enc := json.NewEncoder(writer)

	// Buffer 128 (arbitrary, seems appropriate) messages, and
	// the number of readers in errors as we know exactly how
	// many errors to expect on the channel.
	logCh := make(chan systemd.Log, 128)
	errCh := make(chan error, len(rr.readers))
	stopCh := make(chan struct{})
	for _, r := range rr.readers {
		go rr.logReader(r, logCh, errCh, stopCh)
	}

	writeError := func(err error) {
		if err == errCannotWriteToClient {
			return
		}
		// RS -- see ascii(7), and RFC7464
		fmt.Fprintf(writer, `\x1E{"error": %q}\n`, err)
		logger.Noticef("cannot stream response; problem reading: %v", err)
	}

	writeLogs := func(logs []systemd.Log) error {
		// sort by timestamp ascending
		sort.Slice(logs, func(i, j int) bool {
			ti, _ := logs[i].Time()
			tj, _ := logs[j].Time()
			return ti.Before(tj)
		})

		for _, l := range logs {
			writer.WriteByte(0x1E) // RS -- see ascii(7), and RFC7464

			// ignore the error...
			t, _ := l.Time()
			if err := enc.Encode(client.Log{
				Timestamp: t,
				Message:   l.Message(),
				SID:       l.SID(),
				PID:       l.PID(),
			}); err != nil {
				return err
			}
		}

		if rr.follow {
			if err := writer.Flush(); err != nil {
				return errCannotWriteToClient
			}
			if hasFlusher {
				flusher.Flush()
			}
		}
		return nil
	}

	// When we are not following, we can just read the entire logs in one go
	// and print that to the user.
	if !rr.follow {
		logs := rr.readAllLogs(logCh, errCh)
		if err := writeLogs(logs); err != nil {
			writeError(err)
		}

		// Just close the stop channel here, we aren't using it.
		close(stopCh)
	} else {
		// In follow-mode the case is different. It gets a bit more complex
		// with the possibility of multiple log sources, that can arrive in
		// any given order. To handle this we do the following, try to read
		// the combined backlog from all the sources, and keep track of logs
		// read that are from a later time than the current time.
		backlog := rr.readFollowBacklog(logCh, errCh)
		if err := writeLogs(backlog); err != nil {
			writeError(err)
		}

		// After reading what we believe to be the complete backlog, we then
		// enter a follow-mode read loop. This will run until the client disconnects
		// (i.e we get the errCannotWriteToClient error).
		logReadersDone, err := rr.readFollowMode(writeLogs, logCh, errCh)
		if err != nil {
			writeError(err)
		}

		// At last, we close the stop channel to signal to the readers that we are done.
		close(stopCh)
		for logReadersDone != len(rr.readers) {
			<-errCh
			logReadersDone++
		}
	}

	// Cleanup the other channels after all readers exit
	close(logCh)
	close(errCh)

	if err := writer.Flush(); err != nil {
		logger.Noticef("cannot stream response; problem writing: %v", err)
	}
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
